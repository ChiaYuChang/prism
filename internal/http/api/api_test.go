package api_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/http/api"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/ChiaYuChang/prism/internal/repo/mocks"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type testServerMocks struct {
	scout       *mocks.MockScout
	tasks       *mocks.MockTasks
	pipeline    *mocks.MockPipeline
	userFetches *mocks.MockUserFetches
}

func newTestServer(t *testing.T) (*api.Server, *testServerMocks) {
	t.Helper()
	m := &testServerMocks{
		scout:       mocks.NewMockScout(t),
		tasks:       mocks.NewMockTasks(t),
		pipeline:    mocks.NewMockPipeline(t),
		userFetches: mocks.NewMockUserFetches(t),
	}
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	srv, err := api.NewServer(logger, m.scout, m.tasks, m.pipeline, m.userFetches)
	require.NoError(t, err)
	return srv, m
}

func TestListCandidates_HappyPath(t *testing.T) {
	srv, m := newTestServer(t)

	id := uuid.Must(uuid.NewV7())
	batch := uuid.Must(uuid.NewV7())
	published := time.Now().Add(-time.Hour).UTC()
	m.scout.EXPECT().ListCandidates(mock.Anything, mock.MatchedBy(func(p repo.ListCandidatesParams) bool {
		return p.Query != nil && *p.Query == "election" &&
			p.SourceAbbr != nil && *p.SourceAbbr == "dpp" &&
			p.Limit == 25 && p.Offset == 10
	})).Return([]repo.Candidate{{
		ID:              id,
		BatchID:         batch,
		SourceAbbr:      "dpp",
		Title:           "T",
		URL:             "https://example.com/a",
		PublishedAt:     &published,
		DiscoveredAt:    published,
		IngestionMethod: repo.IngestionMethodDirectory,
		TraceID:         "trace-1",
	}}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/candidates?q=election&source_abbr=dpp&limit=25&offset=10", nil)
	rec := httptest.NewRecorder()
	srv.ListCandidates(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var body api.ListCandidatesResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Len(t, body.Items, 1)
	require.Equal(t, id, body.Items[0].ID)
	require.EqualValues(t, 25, body.Limit)
	require.EqualValues(t, 10, body.Offset)
}

func TestListCandidates_InvalidSince(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/candidates?since=not-a-time", nil)
	rec := httptest.NewRecorder()
	srv.ListCandidates(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

// expectCreateFetch stubs UserFetches.Create with a fresh fetch_id.
func expectCreateFetch(t *testing.T, m *testServerMocks) uuid.UUID {
	t.Helper()
	fetchID := uuid.Must(uuid.NewV7())
	m.userFetches.EXPECT().Create(mock.Anything, repo.CreateUserFetchParams{UserID: nil}).
		Return(repo.UserFetch{ID: fetchID}, nil).Once()
	return fetchID
}

func TestPageFetch_CreatesTaskForFoundCandidate(t *testing.T) {
	srv, m := newTestServer(t)

	candID := uuid.Must(uuid.NewV7())
	missingID := uuid.Must(uuid.NewV7())
	batch := uuid.Must(uuid.NewV7())
	taskID := uuid.Must(uuid.NewV7())

	m.scout.EXPECT().GetCandidatesByIDs(mock.Anything, []uuid.UUID{candID, missingID}).
		Return([]repo.Candidate{{
			ID:         candID,
			BatchID:    batch,
			SourceAbbr: "yahoo",
			URL:        "https://news.example/a",
			TraceID:    "trace-2",
		}}, nil).Once()

	fetchID := expectCreateFetch(t, m)

	m.tasks.EXPECT().CreateTask(mock.Anything, mock.MatchedBy(func(p repo.CreateTaskParams) bool {
		return p.Kind == repo.TaskKindPageFetch &&
			p.SourceType == repo.SourceTypeMedia &&
			p.SourceAbbr == "yahoo" &&
			p.URL == "https://news.example/a" &&
			p.BatchID == batch &&
			p.TraceID == "trace-2"
	})).Return(repo.Task{ID: taskID}, nil).Once()

	m.userFetches.EXPECT().CreateItem(mock.Anything, mock.MatchedBy(func(p repo.CreateUserFetchItemParams) bool {
		return p.FetchID == fetchID && p.CandidateID == candID &&
			p.TaskID != nil && *p.TaskID == taskID && p.SnapshotStatus == nil
	})).Return(repo.UserFetchItem{}, nil).Once()

	body, _ := json.Marshal(api.PageFetchRequest{CandidateIDs: []uuid.UUID{candID, missingID}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/page_fetch", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.PageFetch(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)
	var resp api.PageFetchResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Equal(t, fetchID, resp.FetchID)
	require.Len(t, resp.Items, 2)
	// Order preserved.
	require.Equal(t, candID, resp.Items[0].CandidateID)
	require.Equal(t, api.PageFetchStatusCreated, resp.Items[0].Status)
	require.Equal(t, missingID, resp.Items[1].CandidateID)
	require.Equal(t, api.PageFetchStatusNotFound, resp.Items[1].Status)

	// task_id must never appear in the response payload.
	raw, _ := json.Marshal(resp)
	require.NotContains(t, string(raw), "task_id")
}

func TestPageFetch_AlreadyActiveCollapsesToCreated(t *testing.T) {
	srv, m := newTestServer(t)

	candID := uuid.Must(uuid.NewV7())
	existingTaskID := uuid.Must(uuid.NewV7())
	url := "https://news.example/x"

	m.scout.EXPECT().GetCandidatesByIDs(mock.Anything, mock.Anything).
		Return([]repo.Candidate{{
			ID: candID, BatchID: uuid.Must(uuid.NewV7()),
			SourceAbbr: "yahoo", URL: url, TraceID: "t",
		}}, nil).Once()

	fetchID := expectCreateFetch(t, m)

	m.tasks.EXPECT().CreateTask(mock.Anything, mock.Anything).
		Return(repo.Task{ID: existingTaskID}, repo.ErrTaskAlreadyActive).Once()
	m.userFetches.EXPECT().CreateItem(mock.Anything, mock.MatchedBy(func(p repo.CreateUserFetchItemParams) bool {
		return p.FetchID == fetchID && p.CandidateID == candID &&
			p.TaskID != nil && *p.TaskID == existingTaskID && p.SnapshotStatus == nil
	})).Return(repo.UserFetchItem{}, nil).Once()

	body, _ := json.Marshal(api.PageFetchRequest{CandidateIDs: []uuid.UUID{candID}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/page_fetch", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.PageFetch(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)
	var resp api.PageFetchResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Equal(t, api.PageFetchStatusCreated, resp.Items[0].Status)

	// Privacy: shared task_id must never leak.
	raw, _ := json.Marshal(resp)
	require.NotContains(t, string(raw), existingTaskID.String())
	require.NotContains(t, string(raw), "already_active")
}

func TestPageFetch_AlreadyCompleteSnapshot(t *testing.T) {
	srv, m := newTestServer(t)

	candID := uuid.Must(uuid.NewV7())
	url := "https://news.example/done"

	m.scout.EXPECT().GetCandidatesByIDs(mock.Anything, mock.Anything).
		Return([]repo.Candidate{{
			ID: candID, BatchID: uuid.Must(uuid.NewV7()),
			SourceAbbr: "yahoo", URL: url, TraceID: "t",
		}}, nil).Once()

	fetchID := expectCreateFetch(t, m)

	m.tasks.EXPECT().CreateTask(mock.Anything, mock.Anything).
		Return(repo.Task{}, repo.ErrTaskAlreadyActive).Once()
	m.pipeline.EXPECT().GetContentByURL(mock.Anything, url).
		Return(repo.Content{ID: uuid.Must(uuid.NewV7())}, nil).Once()
	m.userFetches.EXPECT().CreateItem(mock.Anything, mock.MatchedBy(func(p repo.CreateUserFetchItemParams) bool {
		return p.FetchID == fetchID && p.CandidateID == candID &&
			p.TaskID == nil &&
			p.SnapshotStatus != nil && *p.SnapshotStatus == repo.UserFetchItemSnapshotAlreadyComplete
	})).Return(repo.UserFetchItem{}, nil).Once()

	body, _ := json.Marshal(api.PageFetchRequest{CandidateIDs: []uuid.UUID{candID}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/page_fetch", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.PageFetch(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)
	var resp api.PageFetchResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Equal(t, api.PageFetchStatusAlreadyComplete, resp.Items[0].Status)
}

func TestPageFetch_RaceMissReturns500(t *testing.T) {
	srv, m := newTestServer(t)

	candID := uuid.Must(uuid.NewV7())
	url := "https://news.example/race"

	m.scout.EXPECT().GetCandidatesByIDs(mock.Anything, mock.Anything).
		Return([]repo.Candidate{{
			ID: candID, BatchID: uuid.Must(uuid.NewV7()),
			SourceAbbr: "yahoo", URL: url, TraceID: "t",
		}}, nil).Once()

	expectCreateFetch(t, m)

	m.tasks.EXPECT().CreateTask(mock.Anything, mock.Anything).
		Return(repo.Task{}, repo.ErrTaskAlreadyActive).Once()
	m.pipeline.EXPECT().GetContentByURL(mock.Anything, url).
		Return(repo.Content{}, pgx.ErrNoRows).Once()

	body, _ := json.Marshal(api.PageFetchRequest{CandidateIDs: []uuid.UUID{candID}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/page_fetch", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.PageFetch(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestPageFetch_EmptyBody(t *testing.T) {
	srv, _ := newTestServer(t)
	body, _ := json.Marshal(api.PageFetchRequest{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/page_fetch", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.PageFetch(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestGetFetch_HappyPath(t *testing.T) {
	srv, m := newTestServer(t)

	fetchID := uuid.Must(uuid.NewV7())
	m.userFetches.EXPECT().Get(mock.Anything, fetchID).
		Return(repo.UserFetch{ID: fetchID}, nil).Once()
	m.userFetches.EXPECT().GetProgress(mock.Anything, fetchID).
		Return(repo.UserFetchProgress{
			Total: 3, Completed: 1, Running: 1, AlreadyComplete: 1, Terminal: false,
		}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/fetches/"+fetchID.String(), nil)
	req.SetPathValue("id", fetchID.String())
	rec := httptest.NewRecorder()
	srv.GetFetch(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp api.FetchProgressResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Equal(t, fetchID, resp.FetchID)
	require.EqualValues(t, 3, resp.Total)
	require.EqualValues(t, 1, resp.Completed)
	require.EqualValues(t, 1, resp.Running)
	require.EqualValues(t, 1, resp.AlreadyComplete)
	require.False(t, resp.Terminal)
}

func TestGetFetch_NotFound(t *testing.T) {
	srv, m := newTestServer(t)

	fetchID := uuid.Must(uuid.NewV7())
	m.userFetches.EXPECT().Get(mock.Anything, fetchID).
		Return(repo.UserFetch{}, pgx.ErrNoRows).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/fetches/"+fetchID.String(), nil)
	req.SetPathValue("id", fetchID.String())
	rec := httptest.NewRecorder()
	srv.GetFetch(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestGetFetch_InvalidID(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/fetches/not-a-uuid", nil)
	req.SetPathValue("id", "not-a-uuid")
	rec := httptest.NewRecorder()
	srv.GetFetch(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestGetContent_NotFoundReturns404(t *testing.T) {
	srv, m := newTestServer(t)

	id := uuid.Must(uuid.NewV7())
	m.pipeline.EXPECT().GetContentByCandidateID(mock.Anything, id).
		Return(repo.Content{}, pgx.ErrNoRows).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/contents/"+id.String(), nil)
	req.SetPathValue("candidate_id", id.String())
	rec := httptest.NewRecorder()
	srv.GetContent(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestGetContent_HappyPath(t *testing.T) {
	srv, m := newTestServer(t)

	id := uuid.Must(uuid.NewV7())
	contentID := uuid.Must(uuid.NewV7())
	m.pipeline.EXPECT().GetContentByCandidateID(mock.Anything, id).
		Return(repo.Content{
			ID: contentID, CandidateID: id,
			URL: "https://news.example/x", Title: "t", Content: "body",
			Type: repo.ContentTypeArticle, SourceAbbr: "yahoo", TraceID: "tr",
			PublishedAt: time.Now().UTC(), FetchedAt: time.Now().UTC(),
		}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/contents/"+id.String(), nil)
	req.SetPathValue("candidate_id", id.String())
	rec := httptest.NewRecorder()
	srv.GetContent(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var c api.Content
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&c))
	require.Equal(t, contentID, c.ID)
}

func TestGetContent_InvalidID(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/contents/not-a-uuid", nil)
	req.SetPathValue("candidate_id", "not-a-uuid")
	rec := httptest.NewRecorder()
	srv.GetContent(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}
