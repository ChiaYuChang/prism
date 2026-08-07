import os

with open('internal/http/api/analysis_runs_test.go', 'a') as f:
    f.write("""
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
""")
