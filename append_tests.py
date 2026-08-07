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
		Return([]repo.Candidate{{ID: dupID, URL: "https://example.com/article"}}, nil).Once()

	m.analysisRuns.EXPECT().CreateSession(mock.Anything, mock.MatchedBy(func(arg repo.CreateAnalysisSessionParams) bool {
		return len(arg.SelectedCandidates) == 1 && arg.SelectedCandidates[0].ID == dupID
	})).Return(repo.AnalysisRun{ID: uuid.Must(uuid.NewV7()), Status: repo.AnalysisRunStatusFetching}, nil).Once()

	body, _ := json.Marshal(map[string]any{
		"topic": "Test", 
		"brief": "Brief", 
		"fetch_failure_policy": "STOP",
		"selected_candidate_ids": []uuid.UUID{dupID, dupID},
	})
	
	req := httptest.NewRequest(http.MethodPost, "/api/v1/analysis-runs", bytes.NewReader(body))
	req = withUserPrincipal(req, userID)
	rec := httptest.NewRecorder()
	srv.CreateAnalysisRun(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestGetAnalysisRun_OwnershipEnforcement(t *testing.T) {
	srv, m := newTestServer(t)
	runID := uuid.Must(uuid.NewV7())
	ownerID := uuid.Must(uuid.NewV7())
	otherID := uuid.Must(uuid.NewV7())
	
	m.analysisRuns.EXPECT().GetByID(mock.Anything, runID).
		Return(repo.AnalysisRun{ID: runID, UserID: &ownerID}, nil).Twice()

	// Owner can access
	req1 := httptest.NewRequest(http.MethodGet, "/api/v1/analyses/"+runID.String(), nil)
	req1 = withUserPrincipal(req1, ownerID)
	req1.SetPathValue("id", runID.String())
	rec1 := httptest.NewRecorder()
	srv.GetAnalysisRun(rec1, req1)
	require.Equal(t, http.StatusOK, rec1.Code)

	// Other user gets 404
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/analyses/"+runID.String(), nil)
	req2 = withUserPrincipal(req2, otherID)
	req2.SetPathValue("id", runID.String())
	rec2 := httptest.NewRecorder()
	srv.GetAnalysisRun(rec2, req2)
	require.Equal(t, http.StatusNotFound, rec2.Code)
}

func TestResolveFetchFailures_OwnershipEnforcement(t *testing.T) {
	srv, m := newTestServer(t)
	runID := uuid.Must(uuid.NewV7())
	ownerID := uuid.Must(uuid.NewV7())
	otherID := uuid.Must(uuid.NewV7())
	
	m.analysisRuns.EXPECT().GetByID(mock.Anything, runID).
		Return(repo.AnalysisRun{ID: runID, UserID: &ownerID, Status: repo.AnalysisRunStatusAwaitingResolution}, nil).Twice()

	body, _ := json.Marshal(map[string]any{"action": "RETRY_FAILED"})
	
	// Other user gets 404
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/analyses/"+runID.String()+"/resolve-fetch-failures", bytes.NewReader(body))
	req2 = withUserPrincipal(req2, otherID)
	req2.SetPathValue("id", runID.String())
	rec2 := httptest.NewRecorder()
	srv.ResolveFetchFailures(rec2, req2)
	require.Equal(t, http.StatusNotFound, rec2.Code)
}
""")

with open('internal/repo/pg/analysis_runs_integration_test.go', 'a') as f:
    f.write("""
func TestAnalysisRuns_RetryFailedItems(t *testing.T) {
	ctx := context.Background()
	pgContainer, db := testutils.SetupPostgres(t)
	defer pgContainer.Terminate(ctx)
	
	queries := New(db)
	repo := NewFactory(db).NewRepository()
	
	// Pre-insert candidate and batch
	batchID := uuid.Must(uuid.NewV7())
	require.NoError(t, queries.EnsureBatchExists(ctx, EnsureBatchExistsParams{
		ID: batchID, SourceType: "MEDIA", SourceAbbr: "test", MaxSubtasks: 10,
	}))
	
	cID1 := uuid.Must(uuid.NewV7())
	_, err := queries.UpsertCandidate(ctx, UpsertCandidateParams{
		ID: cID1, BatchID: pgtype.UUID{Bytes: batchID, Valid: true}, SourceAbbr: "test", 
		Fingerprint: "test", Url: "https://example.com/1",
	})
	require.NoError(t, err)
	
	cID2 := uuid.Must(uuid.NewV7())
	_, err = queries.UpsertCandidate(ctx, UpsertCandidateParams{
		ID: cID2, BatchID: pgtype.UUID{Bytes: batchID, Valid: true}, SourceAbbr: "test", 
		Fingerprint: "test2", Url: "https://example.com/2",
	})
	require.NoError(t, err)
	
	// Create analysis session
	runID := uuid.Must(uuid.NewV7())
	run, err := repo.AnalysisRuns().CreateSession(ctx, rrepo.CreateAnalysisSessionParams{
		AnalysisID: runID, UserID: nil, Topic: "test", Brief: "brief", 
		FetchFailurePolicy: rrepo.AnalysisFetchFailurePolicyStop,
		SelectedCandidates: []rrepo.Candidate{{ID: cID1, SourceAbbr: "test", TraceID: "trace"}, {ID: cID2, SourceAbbr: "test", TraceID: "trace"}},
	})
	require.NoError(t, err)
	
	// Complete one task, fail another
	items, err := repo.AnalysisRuns().ListItems(ctx, runID)
	require.NoError(t, err)
	require.Len(t, items, 2)
	
	var taskID1, taskID2 uuid.UUID
	if items[0].CandidateID == cID1 { taskID1 = *items[0].TaskID; taskID2 = *items[1].TaskID } else { taskID1 = *items[1].TaskID; taskID2 = *items[0].TaskID }
	
	err = queries.CompleteTask(ctx, CompleteTaskParams{
		ID: taskID1, Status: TaskStatusCOMPLETED,
	})
	require.NoError(t, err)
	
	err = queries.CompleteTask(ctx, CompleteTaskParams{
		ID: taskID2, Status: TaskStatusFAILED,
	})
	require.NoError(t, err)
	
	// Execute RetryFailedItems
	err = repo.AnalysisRuns().RetryFailedItems(ctx, runID)
	require.NoError(t, err)
	
	// Verify that the failed task has been retried (a new task is created and linked)
	newItems, err := repo.AnalysisRuns().ListItems(ctx, runID)
	require.NoError(t, err)
	require.Len(t, newItems, 2)
	
	for _, item := range newItems {
		if item.CandidateID == cID1 {
			require.Equal(t, taskID1, *item.TaskID) // The COMPLETED task should NOT change
		} else {
			require.NotEqual(t, taskID2, *item.TaskID) // The FAILED task should get a NEW task ID
		}
	}
}

func TestAnalysisRuns_CreateSession_RaceFallback_SoftDelete(t *testing.T) {
	ctx := context.Background()
	pgContainer, db := testutils.SetupPostgres(t)
	defer pgContainer.Terminate(ctx)
	
	queries := New(db)
	repo := NewFactory(db).NewRepository()
	
	// Setup
	batchID := uuid.Must(uuid.NewV7())
	require.NoError(t, queries.EnsureBatchExists(ctx, EnsureBatchExistsParams{
		ID: batchID, SourceType: "MEDIA", SourceAbbr: "test", MaxSubtasks: 10,
	}))
	
	cID := uuid.Must(uuid.NewV7())
	_, err := queries.UpsertCandidate(ctx, UpsertCandidateParams{
		ID: cID, BatchID: pgtype.UUID{Bytes: batchID, Valid: true}, SourceAbbr: "test", 
		Fingerprint: "test", Url: "https://example.com/1",
	})
	require.NoError(t, err)
	
	// Create task and content
	task, err := repo.Tasks().Create(ctx, rrepo.CreateTaskParams{
		BatchID: batchID, Kind: rrepo.TaskKindPageFetch, SourceType: rrepo.SourceTypeMedia, SourceAbbr: "test", URL: "https://example.com/1", TraceID: "trace",
	})
	require.NoError(t, err)
	
	err = queries.CompleteTask(ctx, CompleteTaskParams{ID: task.ID, Status: TaskStatusCOMPLETED})
	require.NoError(t, err)
	
	_, err = queries.UpsertContent(ctx, UpsertContentParams{
		CandidateID: cID, TaskID: pgtype.UUID{Bytes: task.ID, Valid: true}, Url: "https://example.com/1", RawContent: []byte("raw"), Content: "content", WordCount: 1, TraceID: "trace",
	})
	require.NoError(t, err)
	
	// Soft delete the content
	err = queries.DeleteContent(ctx, cID)
	require.NoError(t, err)
	
	// Now call CreateSession -> race fallback should NOT mark it ALREADY_COMPLETE because content is deleted
	runID := uuid.Must(uuid.NewV7())
	_, err = repo.AnalysisRuns().CreateSession(ctx, rrepo.CreateAnalysisSessionParams{
		AnalysisID: runID, UserID: nil, Topic: "test", Brief: "brief", 
		FetchFailurePolicy: rrepo.AnalysisFetchFailurePolicyStop,
		SelectedCandidates: []rrepo.Candidate{{ID: cID, SourceAbbr: "test", URL: "https://example.com/1", TraceID: "trace"}},
	})
	require.NoError(t, err)
	
	items, err := repo.AnalysisRuns().ListItems(ctx, runID)
	require.NoError(t, err)
	require.Len(t, items, 1)
	
	// Because content is deleted, the race fallback should NOT have set SnapshotStatus to ALREADY_COMPLETE.
	// It should create a new task and leave SnapshotStatus as NULL.
	require.Nil(t, items[0].SnapshotStatus)
	require.NotEqual(t, uuid.Nil, *items[0].TaskID)
	require.NotEqual(t, task.ID, *items[0].TaskID) // should create a fresh task
}
""")
