import os

with open('internal/repo/pg/analysis_runs_integration_test.go', 'a') as f:
    f.write("""
func TestAnalysisRuns_RetryFailedItems(t *testing.T) {
	url := os.Getenv("PRISM_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set PRISM_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	require.NoError(t, err)
	defer pool.Close()
	
	repository := NewPostgresRepository(pool)
	
	// Create a shared Source
	sourceAbbr := "test_source_" + uuid.NewString()[:8]
	_, err = pool.Exec(ctx, `INSERT INTO sources (abbr, name, type, base_url) VALUES ($1, $2, 'MEDIA', 'https://example.test')`, sourceAbbr, sourceAbbr)
	require.NoError(t, err)
	
	// Pre-insert batch and candidates
	batchID := uuid.Must(uuid.NewV7())
	_, err = pool.Exec(ctx, `INSERT INTO batches (id, source_type, source_abbr, n_subtasks, max_subtasks) VALUES ($1, 'MEDIA', $2, 0, 10)`, batchID, sourceAbbr)
	require.NoError(t, err)
	
	cID1 := uuid.Must(uuid.NewV7())
	_, err = pool.Exec(ctx, `INSERT INTO candidates (id, batch_id, source_abbr, fingerprint, url) VALUES ($1, $2, $3, $4, $5)`, cID1, batchID, sourceAbbr, "test1", "https://example.com/1")
	require.NoError(t, err)
	
	cID2 := uuid.Must(uuid.NewV7())
	_, err = pool.Exec(ctx, `INSERT INTO candidates (id, batch_id, source_abbr, fingerprint, url) VALUES ($1, $2, $3, $4, $5)`, cID2, batchID, sourceAbbr, "test2", "https://example.com/2")
	require.NoError(t, err)
	
	// Create analysis session
	runID := uuid.Must(uuid.NewV7())
	_, err = repository.AnalysisRuns().CreateSession(ctx, repo.CreateAnalysisSessionParams{
		AnalysisID: runID, Topic: "test", Brief: "brief", 
		FetchFailurePolicy: "STOP",
		SelectedCandidates: []repo.Candidate{{ID: cID1, SourceAbbr: sourceAbbr}, {ID: cID2, SourceAbbr: sourceAbbr}},
	})
	require.NoError(t, err)
	
	// Complete one task, fail another
	items, err := repository.AnalysisRuns().ListItems(ctx, runID)
	require.NoError(t, err)
	require.Len(t, items, 2)
	
	var taskID1, taskID2 uuid.UUID
	if items[0].CandidateID == cID1 { taskID1 = *items[0].TaskID; taskID2 = *items[1].TaskID } else { taskID1 = *items[1].TaskID; taskID2 = *items[0].TaskID }
	
	_, err = pool.Exec(ctx, `UPDATE tasks SET status = 'COMPLETED' WHERE id = $1`, taskID1)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE tasks SET status = 'FAILED' WHERE id = $1`, taskID2)
	require.NoError(t, err)
	
	// Execute RetryFailedItems
	err = repository.AnalysisRuns().RetryFailedItems(ctx, runID)
	require.NoError(t, err)
	
	// Verify that the failed task has been retried (a new task is created and linked)
	newItems, err := repository.AnalysisRuns().ListItems(ctx, runID)
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
	url := os.Getenv("PRISM_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set PRISM_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	require.NoError(t, err)
	defer pool.Close()
	
	repository := NewPostgresRepository(pool)
	
	// Create a shared Source
	sourceAbbr := "test_source_" + uuid.NewString()[:8]
	_, err = pool.Exec(ctx, `INSERT INTO sources (abbr, name, type, base_url) VALUES ($1, $2, 'MEDIA', 'https://example.test')`, sourceAbbr, sourceAbbr)
	require.NoError(t, err)
	
	batchID := uuid.Must(uuid.NewV7())
	_, err = pool.Exec(ctx, `INSERT INTO batches (id, source_type, source_abbr, n_subtasks, max_subtasks) VALUES ($1, 'MEDIA', $2, 0, 10)`, batchID, sourceAbbr)
	require.NoError(t, err)
	
	cID := uuid.Must(uuid.NewV7())
	_, err = pool.Exec(ctx, `INSERT INTO candidates (id, batch_id, source_abbr, fingerprint, url) VALUES ($1, $2, $3, $4, $5)`, cID, batchID, sourceAbbr, "test-soft-delete", "https://example.com/soft-delete")
	require.NoError(t, err)
	
	// Create task and content
	task, err := repository.Tasks().CreateTask(ctx, repo.CreateTaskParams{
		BatchID: batchID, Kind: repo.TaskKindPageFetch, SourceType: repo.SourceTypeMedia, SourceAbbr: sourceAbbr, URL: "https://example.com/soft-delete", TraceID: "trace",
	})
	require.NoError(t, err)
	
	_, err = pool.Exec(ctx, `UPDATE tasks SET status = 'COMPLETED' WHERE id = $1`, task.ID)
	require.NoError(t, err)
	
	_, err = pool.Exec(ctx, `INSERT INTO contents (candidate_id, task_id, url, raw_content, content, word_count, trace_id, deleted_at) VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())`, 
		cID, task.ID, "https://example.com/soft-delete", []byte("raw"), "content", 1, "trace")
	require.NoError(t, err)
	
	// Now call CreateSession -> race fallback should NOT mark it ALREADY_COMPLETE because content is deleted
	runID := uuid.Must(uuid.NewV7())
	_, err = repository.AnalysisRuns().CreateSession(ctx, repo.CreateAnalysisSessionParams{
		AnalysisID: runID, Topic: "test", Brief: "brief", 
		FetchFailurePolicy: "STOP",
		SelectedCandidates: []repo.Candidate{{ID: cID, SourceAbbr: sourceAbbr, URL: "https://example.com/soft-delete"}},
	})
	require.NoError(t, err)
	
	items, err := repository.AnalysisRuns().ListItems(ctx, runID)
	require.NoError(t, err)
	require.Len(t, items, 1)
	
	// Because content is deleted, the race fallback should NOT have set SnapshotStatus to ALREADY_COMPLETE.
	// It should create a new task and leave SnapshotStatus as NULL.
	require.Nil(t, items[0].SnapshotStatus)
	require.NotEqual(t, uuid.Nil, *items[0].TaskID)
	require.NotEqual(t, task.ID, *items[0].TaskID) // should create a fresh task
}
""")
