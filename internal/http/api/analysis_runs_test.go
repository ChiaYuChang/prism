package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ChiaYuChang/prism/internal/http/api"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/ChiaYuChang/prism/internal/repo/mocks"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestAnalysisPreflightDoesNotCreateFetch(t *testing.T) {
	srv, m := newTestServer(t)
	availableID := uuid.Must(uuid.NewV7())
	missingID := uuid.Must(uuid.NewV7())
	m.scout.EXPECT().GetCandidatesByIDs(mock.Anything, []uuid.UUID{availableID, missingID}).
		Return([]repo.Candidate{{ID: availableID, URL: "https://example.com/article"}}, nil).Once()

	body, _ := json.Marshal(map[string]any{"selected_candidate_ids": []uuid.UUID{availableID, missingID}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/analysis/preflight", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.AnalysisPreflight(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var response api.AnalysisPreflightResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&response))
	require.Equal(t, 2, response.Total)
	require.Equal(t, []uuid.UUID{availableID}, response.AvailableCandidateIDs)
	require.Equal(t, []uuid.UUID{missingID}, response.UnavailableCandidateIDs)
}

func TestCreateAnalysisRunReusesExistingContent(t *testing.T) {
	srv, m := newTestServer(t)
	analysisRuns := mocks.NewMockAnalysisRuns(t)
	srv.AnalysisRuns = analysisRuns
	candidateID := uuid.Must(uuid.NewV7())
	fetchID := uuid.Must(uuid.NewV7())
	runID := uuid.Must(uuid.NewV7())
	userID := uuid.Must(uuid.NewV7())
	candidate := repo.Candidate{ID: candidateID, BatchID: uuid.Must(uuid.NewV7()), SourceAbbr: "yahoo", URL: "https://example.com/article", TraceID: "trace"}

	m.scout.EXPECT().GetCandidatesByIDs(mock.Anything, []uuid.UUID{candidateID}).Return([]repo.Candidate{candidate}, nil).Twice()
	analysisRuns.EXPECT().GetByID(mock.Anything, runID).Return(repo.AnalysisRun{}, pgx.ErrNoRows).Once()
	
	analysisRuns.EXPECT().CreateSession(mock.Anything, mock.MatchedBy(func(arg repo.CreateAnalysisSessionParams) bool {
		return arg.AnalysisID == runID && 
			arg.FetchFailurePolicy == repo.AnalysisFailurePolicyIgnoreFailed && 
			arg.SelectedCandidates[0].ID == candidateID &&
			arg.Topic == "topic" && arg.Brief == "brief"
	})).Return(repo.AnalysisRun{ID: runID, FetchID: fetchID, Status: repo.AnalysisRunStatusFetching}, nil).Once()

	body, _ := json.Marshal(map[string]any{
		"analysis_id":            runID,
		"selected_candidate_ids": []uuid.UUID{candidateID},
		"topic":                  "topic",
		"brief":                  "brief",
		"fetch_failure_policy":   repo.AnalysisFailurePolicyIgnoreFailed,
	})
	req := withUserPrincipal(httptest.NewRequest(http.MethodPost, "/api/v1/analysis-runs", bytes.NewReader(body)), userID)
	rec := httptest.NewRecorder()
	srv.CreateAnalysisRun(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)
	var response api.AnalysisRunResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&response))
	require.Equal(t, fetchID, response.FetchID)
	require.NotEqual(t, uuid.Nil, response.AnalysisRunID)
	require.Equal(t, repo.AnalysisRunStatusFetching, response.Status)
}

func TestResolveFetchFailures_API(t *testing.T) {
	srv, _ := newTestServer(t)
	mockRepo := mocks.NewMockAnalysisRuns(t)
	srv.AnalysisRuns = mockRepo

	analysisID := uuid.New()
	userID := uuid.New()
	run := repo.AnalysisRun{
		ID:     analysisID,
		UserID: &userID,
		Status: repo.AnalysisRunStatusAwaitingResolution,
	}

	testCases := []struct {
		name       string
		action     string
		wantStatus int
	}{
		{
			name:       "Error - CANCEL is rejected",
			action:     "CANCEL",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "Error - Invalid action",
			action:     "INVALID_ACTION",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockRepo.EXPECT().GetByID(mock.Anything, analysisID).Return(run, nil).Once()

			body := `{"action":"` + tc.action + `"}`
			req := httptest.NewRequest(http.MethodPost, "/analysis-runs/"+analysisID.String()+"/resolve-fetch-failures", bytes.NewBufferString(body))
			req = withUserPrincipal(req, userID)
			req.SetPathValue("id", analysisID.String())
			w := httptest.NewRecorder()

			srv.ResolveFetchFailures(w, req)

			require.Equal(t, tc.wantStatus, w.Code)
		})
	}
}

func TestAnalysisPreflight_DuplicateIDs(t *testing.T) {
	srv, m := newTestServer(t)
	dupID := uuid.Must(uuid.NewV7())
	
	m.scout.EXPECT().GetCandidatesByIDs(mock.Anything, []uuid.UUID{dupID}).
		Return([]repo.Candidate{{ID: dupID, URL: "https://example.com/article"}}, nil).Once()

	body, _ := json.Marshal(map[string]any{"selected_candidate_ids": []uuid.UUID{dupID, dupID}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/analysis/preflight", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.AnalysisPreflight(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var response api.AnalysisPreflightResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&response))
	require.Equal(t, 1, response.Total)
	require.Equal(t, []uuid.UUID{dupID}, response.AvailableCandidateIDs)
}

func TestCreateAnalysisRun_DuplicateIDs(t *testing.T) {
	srv, m := newTestServer(t)
	userID := uuid.Must(uuid.NewV7())
	
	dupID := uuid.Must(uuid.NewV7())
	
	m.scout.EXPECT().GetCandidatesByIDs(mock.Anything, []uuid.UUID{dupID}).
		Return([]repo.Candidate{{ID: dupID, URL: "https://example.com/article"}}, nil).Twice()

	mockRepo := mocks.NewMockAnalysisRuns(t)
	srv.AnalysisRuns = mockRepo
	mockRepo.EXPECT().GetByID(mock.Anything, mock.Anything).Return(repo.AnalysisRun{}, pgx.ErrNoRows).Once()
	mockRepo.EXPECT().CreateSession(mock.Anything, mock.MatchedBy(func(arg repo.CreateAnalysisSessionParams) bool {
		return len(arg.SelectedCandidates) == 1 && arg.SelectedCandidates[0].ID == dupID
	})).Return(repo.AnalysisRun{ID: uuid.Must(uuid.NewV7()), Status: repo.AnalysisRunStatusFetching}, nil).Once()

	body, _ := json.Marshal(map[string]any{
		"analysis_id": uuid.Must(uuid.NewV7()),
		"topic": "Test", 
		"brief": "Brief", 
		"fetch_failure_policy": "STOP",
		"selected_candidate_ids": []uuid.UUID{dupID, dupID},
	})
	
	req := httptest.NewRequest(http.MethodPost, "/api/v1/analysis-runs", bytes.NewReader(body))
	req = withUserPrincipal(req, userID)
	rec := httptest.NewRecorder()
	srv.CreateAnalysisRun(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
}

func TestGetAnalysisRun_OwnershipEnforcement(t *testing.T) {
	srv, _ := newTestServer(t)
	runID := uuid.Must(uuid.NewV7())
	ownerID := uuid.Must(uuid.NewV7())
	otherID := uuid.Must(uuid.NewV7())
	
	mockRepo := mocks.NewMockAnalysisRuns(t)
	srv.AnalysisRuns = mockRepo
	mockRepo.EXPECT().GetByID(mock.Anything, runID).
		Return(repo.AnalysisRun{ID: runID, UserID: &ownerID}, nil).Twice()
	mockRepo.EXPECT().ListItems(mock.Anything, runID).
		Return([]repo.AnalysisRunItem{}, nil).Maybe()

	// Owner can access
	req1 := httptest.NewRequest(http.MethodGet, "/api/v1/analyses/"+runID.String(), nil)
	req1 = withUserPrincipal(req1, ownerID)
	req1.SetPathValue("id", runID.String())
	rec1 := httptest.NewRecorder()
	srv.GetAnalysis(rec1, req1)
	require.Equal(t, http.StatusOK, rec1.Code)

	// Other user gets 404
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/analyses/"+runID.String(), nil)
	req2 = withUserPrincipal(req2, otherID)
	req2.SetPathValue("id", runID.String())
	rec2 := httptest.NewRecorder()
	srv.GetAnalysis(rec2, req2)
	require.Equal(t, http.StatusNotFound, rec2.Code)
}

func TestResolveFetchFailures_OwnershipEnforcement(t *testing.T) {
	srv, _ := newTestServer(t)
	runID := uuid.Must(uuid.NewV7())
	ownerID := uuid.Must(uuid.NewV7())
	otherID := uuid.Must(uuid.NewV7())
	
	mockRepo := mocks.NewMockAnalysisRuns(t)
	srv.AnalysisRuns = mockRepo
	mockRepo.EXPECT().GetByID(mock.Anything, runID).
		Return(repo.AnalysisRun{ID: runID, UserID: &ownerID, Status: repo.AnalysisRunStatusAwaitingResolution}, nil).Once()

	body, _ := json.Marshal(map[string]any{"action": "RETRY_FAILED"})
	
	// Other user gets 404
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/analysis-runs/"+runID.String()+"/resolve-fetch-failures", bytes.NewReader(body))
	req2 = withUserPrincipal(req2, otherID)
	req2.SetPathValue("id", runID.String())
	rec2 := httptest.NewRecorder()
	srv.ResolveFetchFailures(rec2, req2)
	require.Equal(t, http.StatusNotFound, rec2.Code)
}
