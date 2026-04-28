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
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/mock"
)

func newTestServer(t *testing.T) (*api.Server, *mocks.MockScout, *mocks.MockTasks, *mocks.MockPipeline) {
	t.Helper()
	scout := mocks.NewMockScout(t)
	tasks := mocks.NewMockTasks(t)
	pipeline := mocks.NewMockPipeline(t)
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	srv, err := api.NewServer(logger, scout, tasks, pipeline)
	require.NoError(t, err)
	return srv, scout, tasks, pipeline
}

func TestListCandidates_HappyPath(t *testing.T) {
	srv, scout, _, _ := newTestServer(t)

	id := uuid.Must(uuid.NewV7())
	batch := uuid.Must(uuid.NewV7())
	published := time.Now().Add(-time.Hour).UTC()
	scout.EXPECT().ListCandidates(mock.Anything, mock.MatchedBy(func(p repo.ListCandidatesParams) bool {
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
	srv, _, _, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/candidates?since=not-a-time", nil)
	rec := httptest.NewRecorder()
	srv.ListCandidates(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPageFetch_CreatesTaskForFoundCandidate(t *testing.T) {
	srv, scout, tasks, _ := newTestServer(t)

	candID := uuid.Must(uuid.NewV7())
	missingID := uuid.Must(uuid.NewV7())
	batch := uuid.Must(uuid.NewV7())
	taskID := uuid.Must(uuid.NewV7())

	scout.EXPECT().GetCandidatesByIDs(mock.Anything, []uuid.UUID{candID, missingID}).
		Return([]repo.Candidate{{
			ID:         candID,
			BatchID:    batch,
			SourceAbbr: "yahoo",
			URL:        "https://news.example/a",
			TraceID:    "trace-2",
		}}, nil).Once()

	tasks.EXPECT().CreateTask(mock.Anything, mock.MatchedBy(func(p repo.CreateTaskParams) bool {
		return p.Kind == repo.TaskKindPageFetch &&
			p.SourceType == repo.SourceTypeMedia &&
			p.SourceAbbr == "yahoo" &&
			p.URL == "https://news.example/a" &&
			p.BatchID == batch &&
			p.TraceID == "trace-2"
	})).Return(repo.Task{ID: taskID}, nil).Once()

	body, _ := json.Marshal(api.PageFetchRequest{CandidateIDs: []uuid.UUID{candID, missingID}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/page_fetch", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.PageFetch(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)
	var resp api.PageFetchResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Len(t, resp.Results, 2)
	require.Equal(t, "created", resp.Results[0].Status)
	require.Equal(t, taskID, resp.Results[0].TaskID)
	require.Equal(t, "not_found", resp.Results[1].Status)
}

func TestPageFetch_AlreadyActiveIsIdempotent(t *testing.T) {
	srv, scout, tasks, _ := newTestServer(t)

	candID := uuid.Must(uuid.NewV7())
	scout.EXPECT().GetCandidatesByIDs(mock.Anything, mock.Anything).
		Return([]repo.Candidate{{
			ID: candID, BatchID: uuid.Must(uuid.NewV7()),
			SourceAbbr: "yahoo", URL: "https://news.example/x", TraceID: "t",
		}}, nil).Once()
	tasks.EXPECT().CreateTask(mock.Anything, mock.Anything).
		Return(repo.Task{}, repo.ErrTaskAlreadyActive).Once()

	body, _ := json.Marshal(api.PageFetchRequest{CandidateIDs: []uuid.UUID{candID}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/page_fetch", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.PageFetch(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)
	var resp api.PageFetchResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Equal(t, "already_active", resp.Results[0].Status)
}

func TestPageFetch_EmptyBody(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	body, _ := json.Marshal(api.PageFetchRequest{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/page_fetch", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.PageFetch(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestGetContent_NotFoundReturns404(t *testing.T) {
	srv, _, _, pipeline := newTestServer(t)

	id := uuid.Must(uuid.NewV7())
	pipeline.EXPECT().GetContentByCandidateID(mock.Anything, id).
		Return(repo.Content{}, pgx.ErrNoRows).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/contents/"+id.String(), nil)
	req.SetPathValue("candidate_id", id.String())
	rec := httptest.NewRecorder()
	srv.GetContent(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestGetContent_HappyPath(t *testing.T) {
	srv, _, _, pipeline := newTestServer(t)

	id := uuid.Must(uuid.NewV7())
	contentID := uuid.Must(uuid.NewV7())
	pipeline.EXPECT().GetContentByCandidateID(mock.Anything, id).
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
	srv, _, _, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/contents/not-a-uuid", nil)
	req.SetPathValue("candidate_id", "not-a-uuid")
	rec := httptest.NewRecorder()
	srv.GetContent(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}
