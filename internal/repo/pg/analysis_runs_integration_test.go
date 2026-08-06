package pg

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestAnalysisRuns_CreateSession_Integration(t *testing.T) {
	url := os.Getenv("PRISM_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set PRISM_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	require.NoError(t, err)
	defer pool.Close()
	require.NoError(t, pool.Ping(ctx))

	// Initialize Repository
	repository := NewPostgresRepository(pool)
	q := New(pool) // Use SQLC queries directly to seed test data

	// Create a shared Source (since candidate requires a source)
	sourceAbbr := "test_source_" + uuid.NewString()[:8]
	_, err = pool.Exec(ctx, `INSERT INTO sources (abbr, name, type, base_url) VALUES ($1, $2, 'MEDIA', 'https://example.test')`, sourceAbbr, sourceAbbr)
	require.NoError(t, err)

	testCases := []struct {
		name                 string
		givenExistingContent bool
		givenActiveTask      bool
		expectNewTaskCreated bool
		expectFetchItemTask  bool
	}{
		{
			name:                 "OK_HappyPath",
			givenExistingContent: false,
			givenActiveTask:      false,
			expectNewTaskCreated: true,
			expectFetchItemTask:  true,
		},
		{
			name:                 "SkipsTask_When_ContentAlreadyExists",
			givenExistingContent: true,
			givenActiveTask:      false,
			expectNewTaskCreated: false,
			expectFetchItemTask:  false, // SnapshotStatus will be ALREADY_COMPLETE
		},
		{
			name:                 "SharesTask_When_ActiveTaskExists",
			givenExistingContent: false,
			givenActiveTask:      true,
			expectNewTaskCreated: false,
			expectFetchItemTask:  true, // Will share the existing task
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			userID := uuid.New()
			analysisID := uuid.New()
			candidateID := uuid.New()
			candidateURL := "https://example.com/article/" + uuid.NewString()

			// 1. Setup Candidate test data
			_, err = pool.Exec(ctx, `
				INSERT INTO candidates (id, batch_id, source_abbr, fingerprint, title, url, ingestion_method) 
				VALUES ($1, $2, $3, $4, $5, $6, 'DIRECTORY')`,
				candidateID, uuid.New(), sourceAbbr, uuid.NewString(), "Test Article", candidateURL)
			require.NoError(t, err)

			// [GIVEN] Pre-existing Content
			if tc.givenExistingContent {
				_, err = pool.Exec(ctx, `
					INSERT INTO contents (url, candidate_id, title, content, fetched_at)
					VALUES ($1, $2, 'Test Title', '<html>body</html>', $3)`,
					candidateURL, candidateID, time.Now())
				require.NoError(t, err)
			}

			// [GIVEN] Pre-existing active PAGE_FETCH task (simulating another batch)
			if tc.givenActiveTask {
				otherBatchID := uuid.New()
				err = q.EnsureBatchExists(ctx, EnsureBatchExistsParams{
					ID:         otherBatchID,
					SourceType: SourceType("PARTY"),
				})
				require.NoError(t, err)

				_, err = pool.Exec(ctx, `
					INSERT INTO tasks (batch_id, kind, source_type, source_abbr, url, status)
					VALUES ($1, 'PAGE_FETCH', 'MEDIA', $2, $3, 'PENDING')`,
					otherBatchID, sourceAbbr, candidateURL)
				require.NoError(t, err)
			}

			// Record initial task count
			var initialTaskCount int
			err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM tasks").Scan(&initialTaskCount)
			require.NoError(t, err)

			// [WHEN] Invoke CreateSession
			params := repo.CreateAnalysisSessionParams{
				AnalysisID:         analysisID,
				UserID:             &userID,
				Topic:              "Test Topic",
				Brief:              "Test Brief",
				FetchFailurePolicy: "IGNORE",
				SelectedCandidates: []repo.Candidate{
					{
						ID:         candidateID,
						URL:        candidateURL,
						SourceAbbr: sourceAbbr,
						BatchID:    uuid.New(),
					},
				},
			}

			run, err := repository.AnalysisRuns().CreateSession(ctx, params)
			require.NoError(t, err)
			require.Equal(t, repo.AnalysisRunStatusFetching, run.Status)

			// [THEN] Verify task count mutation
			var finalTaskCount int
			err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM tasks").Scan(&finalTaskCount)
			require.NoError(t, err)

			if tc.expectNewTaskCreated {
				require.Equal(t, initialTaskCount+1, finalTaskCount, "Expected 1 new task to be created")
			} else {
				require.Equal(t, initialTaskCount, finalTaskCount, "Expected no new tasks to be created")
			}

			// [THEN] Verify UserFetchItem
			var items []struct {
				TaskID         *uuid.UUID
				SnapshotStatus *string
			}
			rows, err := pool.Query(ctx, "SELECT task_id, snapshot_status FROM user_fetch_items WHERE fetch_id = $1", run.FetchID)
			require.NoError(t, err)
			for rows.Next() {
				var item struct {
					TaskID         *uuid.UUID
					SnapshotStatus *string
				}
				err := rows.Scan(&item.TaskID, &item.SnapshotStatus)
				require.NoError(t, err)
				items = append(items, item)
			}
			rows.Close()
			require.Len(t, items, 1)

			fetchItem := items[0]
			if tc.expectFetchItemTask {
				require.NotNil(t, fetchItem.TaskID, "Expected UserFetchItem to be bound to a task")
				require.Nil(t, fetchItem.SnapshotStatus)
			} else {
				require.Nil(t, fetchItem.TaskID, "Expected UserFetchItem NOT to have a task")
				require.NotNil(t, fetchItem.SnapshotStatus)
				require.Equal(t, "ALREADY_COMPLETE", *fetchItem.SnapshotStatus)
			}
		})
	}
}

func TestAnalysisRuns_CreateSession_RollbackOnConflict(t *testing.T) {
	if os.Getenv("PRISM_TEST_DATABASE_URL") == "" {
		t.Skip("Skipping integration test: PRISM_TEST_DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("PRISM_TEST_DATABASE_URL"))
	require.NoError(t, err)
	defer pool.Close()

	repository := NewPostgresRepository(pool)

	userID := uuid.New()
	analysisID := uuid.New()
	fetchID := uuid.New()

	// Pre-create the fetch and analysis run to cause a primary key conflict
	_, err = pool.Exec(ctx, "INSERT INTO user_fetches (id, user_id) VALUES ($1, $2)", fetchID, userID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO analysis_runs (id, user_id, fetch_id, status, fetch_failure_policy) 
		VALUES ($1, $2, $3, 'FETCHING', 'IGNORE')`,
		analysisID, userID, fetchID)
	require.NoError(t, err)

	// WHEN CreateSession is called with the SAME analysisID
	params := repo.CreateAnalysisSessionParams{
		AnalysisID:         analysisID, // CONFLICT!
		UserID:             &userID,
		Topic:              "Test Topic",
		Brief:              "Test Brief",
		FetchFailurePolicy: "IGNORE",
		SelectedCandidates: []repo.Candidate{},
	}

	// Record initial fetches count
	var initialFetchCount int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM user_fetches").Scan(&initialFetchCount)
	require.NoError(t, err)

	_, err = repository.AnalysisRuns().CreateSession(ctx, params)
	require.Error(t, err) // Should fail due to PK conflict

	// THEN: It should have rolled back, so NO new user_fetches were created
	var finalFetchCount int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM user_fetches").Scan(&finalFetchCount)
	require.NoError(t, err)
	require.Equal(t, initialFetchCount, finalFetchCount, "Expected Rollback to prevent user_fetches creation")
}

func TestAnalysisRuns_CreateSession_RaceFallback(t *testing.T) {
	if os.Getenv("PRISM_TEST_DATABASE_URL") == "" {
		t.Skip("Skipping integration test: PRISM_TEST_DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("PRISM_TEST_DATABASE_URL"))
	require.NoError(t, err)
	defer pool.Close()

	repository := NewPostgresRepository(pool)

	userID := uuid.New()
	analysisID := uuid.New()
	candidateID := uuid.New()
	candidateURL := "https://example.com/race/" + uuid.NewString()

	// 1. Setup Candidate test data
	sourceAbbr := "test_source_" + uuid.NewString()[:8]
	_, err = pool.Exec(ctx, `INSERT INTO sources (abbr, name, type, base_url) VALUES ($1, $2, 'MEDIA', 'https://example.test')`, sourceAbbr, sourceAbbr)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO candidates (id, batch_id, source_abbr, fingerprint, title, url, ingestion_method) 
		VALUES ($1, $2, $3, $4, $5, $6, 'DIRECTORY')`,
		candidateID, uuid.New(), sourceAbbr, uuid.NewString(), "Test Article", candidateURL)
	require.NoError(t, err)

	// 2. Setup a COMPLETED task and its contents
	otherBatchID := uuid.New()
	_, err = pool.Exec(ctx, `INSERT INTO batches (id, source_type) VALUES ($1, 'PARTY')`, otherBatchID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO tasks (batch_id, kind, source_type, source_abbr, url, status)
		VALUES ($1, 'PAGE_FETCH', 'MEDIA', $2, $3, 'COMPLETED')`,
		otherBatchID, sourceAbbr, candidateURL)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO contents (url, candidate_id, title, content, fetched_at)
		VALUES ($1, $2, 'Test Title', '<html>body</html>', $3)`,
		candidateURL, candidateID, time.Now())
	require.NoError(t, err)

	// WHEN CreateSession is called
	params := repo.CreateAnalysisSessionParams{
		AnalysisID:         analysisID,
		UserID:             &userID,
		Topic:              "Test Topic",
		Brief:              "Test Brief",
		FetchFailurePolicy: "IGNORE",
		SelectedCandidates: []repo.Candidate{
			{
				ID:         candidateID,
				URL:        candidateURL,
				SourceAbbr: sourceAbbr,
				BatchID:    uuid.New(),
			},
		},
	}

	run, err := repository.AnalysisRuns().CreateSession(ctx, params)
	require.NoError(t, err)

	// THEN: The race fallback should have been triggered
	// (Task insertion hit conflict on URL, but no PENDING/RUNNING task was found.
	// So it fell back to GetContentByURL and succeeded).
	var snapshotStatus *string
	var taskID *uuid.UUID
	err = pool.QueryRow(ctx, "SELECT task_id, snapshot_status FROM user_fetch_items WHERE fetch_id = $1", run.FetchID).Scan(&taskID, &snapshotStatus)
	require.NoError(t, err)
	
	require.Nil(t, taskID, "Expected UserFetchItem NOT to have a task (it should use ALREADY_COMPLETE snapshot)")
	require.NotNil(t, snapshotStatus)
	require.Equal(t, "ALREADY_COMPLETE", *snapshotStatus)
}

