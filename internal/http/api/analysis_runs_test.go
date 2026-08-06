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
