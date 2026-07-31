package pg

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestInitializePipelineConcurrentIsAtomicAndIdempotent(t *testing.T) {
	url := os.Getenv("PRISM_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set PRISM_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	require.NoError(t, err)
	defer pool.Close()
	require.NoError(t, pool.Ping(ctx))

	abbr := "it" + uuid.NewString()[:12]
	rootID := uuid.New()
	traceID := "integration-" + uuid.NewString()
	_, err = pool.Exec(ctx, `INSERT INTO sources (abbr, name, type, base_url) VALUES ($1, $2, 'PARTY', 'https://example.test')`, abbr, abbr)
	require.NoError(t, err)
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM tasks WHERE batch_id IN (SELECT id FROM batches WHERE parent_id = $1)`, rootID)
		_, _ = pool.Exec(ctx, `DELETE FROM batches WHERE parent_id = $1`, rootID)
		_, _ = pool.Exec(ctx, `DELETE FROM tasks WHERE batch_id = $1`, rootID)
		_, _ = pool.Exec(ctx, `DELETE FROM batches WHERE id = $1`, rootID)
		_, _ = pool.Exec(ctx, `DELETE FROM sources WHERE abbr = $1`, abbr)
	}()

	r := NewPostgresRepository(pool)
	tasks := r.Tasks()
	require.NoError(t, tasks.EnsureBatch(ctx, repo.EnsureBatchParams{BatchID: rootID, SourceType: repo.SourceTypeParty, TraceID: traceID}))
	initTask, err := tasks.CreateTask(ctx, repo.CreateTaskParams{
		BatchID: rootID, Kind: repo.TaskKindPipelineInit, SourceType: repo.SourceTypeParty, SourceAbbr: abbr,
		URL: "https://pipeline.test/init/" + rootID.String(), TraceID: traceID,
	})
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE tasks SET status = 'RUNNING' WHERE id = $1`, initTask.ID)
	require.NoError(t, err)

	stageTasks := make([]repo.CreateTaskParams, 2)
	for i, name := range []string{"first", "second"} {
		logical := "pipeline:stage:" + name
		stageTasks[i] = repo.CreateTaskParams{
			BatchID: rootID, LogicalKey: &logical, Kind: repo.TaskKindPipelineStage,
			SourceType: repo.SourceTypeParty, SourceAbbr: abbr, URL: "https://pipeline.test/stage/" + name,
			TraceID: traceID,
		}
	}
	arg := repo.InitializePipelineParams{BatchID: rootID, InitTaskID: initTask.ID, NSubtasks: 3, Tasks: stageTasks}
	const attempts = 8
	errs := make(chan error, attempts)
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- r.PipelineRuntime().InitializePipeline(ctx, arg)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	rows, err := tasks.ListTasksByBatchID(ctx, rootID)
	require.NoError(t, err)
	require.Len(t, rows, 3)
	finishedInit, err := tasks.GetTaskByID(ctx, initTask.ID)
	require.NoError(t, err)
	require.Equal(t, repo.TaskStatusCompleted, finishedInit.Status)
	var nSubtasks int32
	require.NoError(t, pool.QueryRow(ctx, `SELECT n_subtasks FROM batches WHERE id = $1`, rootID).Scan(&nSubtasks))
	require.Equal(t, int32(3), nSubtasks)
	stageOwner := rows[1]
	stageWork := []repo.CreateTaskParams{
		{LogicalKey: stringPtr("work:first"), Kind: repo.TaskKindEmbedCandidate, SourceType: repo.SourceTypeParty, SourceAbbr: abbr, URL: "https://pipeline.test/work/first", TraceID: traceID},
		{LogicalKey: stringPtr("work:second"), Kind: repo.TaskKindEmbedCandidate, SourceType: repo.SourceTypeParty, SourceAbbr: abbr, URL: "https://pipeline.test/work/second", TraceID: traceID},
	}
	stageArg := repo.InitializePipelineStageParams{
		ChildBatchID: uuid.New(), ParentBatchID: rootID, ParentTaskID: stageOwner.ID, SourceType: repo.SourceTypeParty,
		TraceID: traceID, NSubtasks: 2, Tasks: stageWork,
	}
	childIDs := make(chan uuid.UUID, attempts)
	stageErrs := make(chan error, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			attempt := stageArg
			attempt.ChildBatchID = uuid.New()
			childID, stageErr := r.PipelineRuntime().InitializePipelineStage(ctx, attempt)
			if stageErr != nil {
				stageErrs <- stageErr
				return
			}
			childIDs <- childID
		}()
	}
	wg.Wait()
	close(childIDs)
	close(stageErrs)
	for err := range stageErrs {
		require.NoError(t, err)
	}
	var childID uuid.UUID
	for id := range childIDs {
		if childID == uuid.Nil {
			childID = id
		}
		require.Equal(t, childID, id)
	}
	require.NotEqual(t, uuid.Nil, childID)
	var childCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM batches WHERE parent_task_id = $1`, stageOwner.ID).Scan(&childCount))
	require.Equal(t, 1, childCount)
	var workCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM tasks WHERE batch_id = $1`, childID).Scan(&workCount))
	require.Equal(t, 2, workCount)
	var predecessorCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM tasks WHERE batch_id = $1 AND previous_task_id IS NOT NULL`, childID).Scan(&predecessorCount))
	require.Zero(t, predecessorCount)
	_, err = pool.Exec(ctx, `UPDATE batches SET completed_at = NOW(), succeeded = TRUE WHERE id = $1`, childID)
	require.NoError(t, err)
	recoveredID, err := r.PipelineRuntime().InitializePipelineStage(ctx, stageArg)
	require.NoError(t, err)
	require.Equal(t, childID, recoveredID)
}

func TestCreatePipelineRootRequiresCompletedInputAndStoresParent(t *testing.T) {
	url := os.Getenv("PRISM_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set PRISM_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	require.NoError(t, err)
	defer pool.Close()
	require.NoError(t, pool.Ping(ctx))

	abbr := "it" + uuid.NewString()[:12]
	inputID, rootID := uuid.New(), uuid.New()
	_, err = pool.Exec(ctx, `INSERT INTO sources (abbr, name, type, base_url) VALUES ($1, $2, 'PARTY', 'https://example.test')`, abbr, abbr)
	require.NoError(t, err)
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM tasks WHERE batch_id = $1`, rootID)
		_, _ = pool.Exec(ctx, `DELETE FROM batches WHERE id IN ($1, $2)`, rootID, inputID)
		_, _ = pool.Exec(ctx, `DELETE FROM sources WHERE abbr = $1`, abbr)
	}()
	_, err = pool.Exec(ctx, `INSERT INTO batches (id, source_type, trace_id) VALUES ($1, 'PARTY', 'input')`, inputID)
	require.NoError(t, err)
	runtime := NewPostgresRepository(pool).PipelineRuntime()
	_, err = runtime.CreatePipelineRoot(ctx, repo.CreateTaskParams{
		BatchID: rootID, ParentBatchID: &inputID, Kind: repo.TaskKindPipelineInit,
		SourceType: repo.SourceTypeParty, SourceAbbr: abbr, URL: "https://pipeline.test/init/" + rootID.String(),
		TraceID: "root",
	})
	require.ErrorIs(t, err, repo.ErrPipelineInputNotTerminal)

	_, err = pool.Exec(ctx, `UPDATE batches SET completed_at = NOW() WHERE id = $1`, inputID)
	require.NoError(t, err)
	created, err := runtime.CreatePipelineRoot(ctx, repo.CreateTaskParams{
		BatchID: rootID, ParentBatchID: &inputID, Kind: repo.TaskKindPipelineInit,
		SourceType: repo.SourceTypeParty, SourceAbbr: abbr, URL: "https://pipeline.test/init/" + rootID.String(),
		TraceID: "root",
	})
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, created.ID)
	var parentID uuid.UUID
	require.NoError(t, pool.QueryRow(ctx, `SELECT parent_id FROM batches WHERE id = $1`, rootID).Scan(&parentID))
	require.Equal(t, inputID, parentID)
}

func stringPtr(value string) *string { return &value }

func TestMarkPipelineBatchFinishedHasSingleWinner(t *testing.T) {
	url := os.Getenv("PRISM_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set PRISM_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	require.NoError(t, err)
	defer pool.Close()
	require.NoError(t, pool.Ping(ctx))

	abbr := "it" + uuid.NewString()[:12]
	batchID := uuid.New()
	_, err = pool.Exec(ctx, `INSERT INTO sources (abbr, name, type, base_url) VALUES ($1, $2, 'PARTY', 'https://example.test')`, abbr, abbr)
	require.NoError(t, err)
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM tasks WHERE batch_id = $1`, batchID)
		_, _ = pool.Exec(ctx, `DELETE FROM batches WHERE id = $1`, batchID)
		_, _ = pool.Exec(ctx, `DELETE FROM sources WHERE abbr = $1`, abbr)
	}()
	r := NewPostgresRepository(pool)
	tasks := r.Tasks()
	require.NoError(t, tasks.EnsureBatch(ctx, repo.EnsureBatchParams{BatchID: batchID, SourceType: repo.SourceTypeParty, TraceID: "detector-test"}))
	_, err = tasks.CreateTask(ctx, repo.CreateTaskParams{
		BatchID: batchID, Kind: repo.TaskKindPipelineStage, SourceType: repo.SourceTypeParty, SourceAbbr: abbr,
		URL: "https://pipeline.test/finished/" + batchID.String(), TraceID: "detector-test",
	})
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE tasks SET status = 'COMPLETED' WHERE batch_id = $1`, batchID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE batches SET n_subtasks = 1 WHERE id = $1`, batchID)
	require.NoError(t, err)

	const attempts = 8
	winners := make(chan int64, attempts)
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			winner, markErr := r.PipelineRuntime().MarkBatchFinished(ctx, batchID, true, "detector-test")
			if markErr != nil {
				winners <- -1
				return
			}
			winners <- winner
		}()
	}
	wg.Wait()
	close(winners)
	winningCalls := 0
	for winner := range winners {
		require.NotEqual(t, int64(-1), winner)
		if winner == 1 {
			winningCalls++
		}
	}
	require.Equal(t, 1, winningCalls)
	finishedRoots, err := r.PipelineRuntime().FindFinishedRootBatches(ctx, 100)
	require.NoError(t, err)
	for _, finished := range finishedRoots {
		require.NotEqual(t, batchID, finished.ID)
	}
}

func TestPipelinePublishFailureRemainsRetryable(t *testing.T) {
	url := os.Getenv("PRISM_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set PRISM_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	require.NoError(t, err)
	defer pool.Close()
	require.NoError(t, pool.Ping(ctx))
	batchID := uuid.New()
	parentID, ownerID := uuid.New(), uuid.New()
	abbr := "it" + uuid.NewString()[:12]
	_, err = pool.Exec(ctx, `INSERT INTO sources (abbr, name, type, base_url) VALUES ($1, $2, 'PARTY', 'https://example.test')`, abbr, abbr)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO batches (id, source_type, trace_id) VALUES ($1, 'PARTY', 'publish-root')`, parentID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO tasks (id, batch_id, kind, source_type, source_abbr, url, trace_id) VALUES ($1, $2, 'PIPELINE_STAGE', 'PARTY', $3, 'https://pipeline.test/owner', 'publish-root')`, ownerID, parentID, abbr)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO batches (id, source_type, trace_id, parent_id, parent_task_id, n_subtasks, completed_at) VALUES ($1, 'PARTY', 'publish-test', $2, $3, 0, NOW())`, batchID, parentID, ownerID)
	require.NoError(t, err)
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM tasks WHERE id = $1`, ownerID)
		_, _ = pool.Exec(ctx, `DELETE FROM batches WHERE id = $1`, batchID)
		_, _ = pool.Exec(ctx, `DELETE FROM batches WHERE id = $1`, parentID)
		_, _ = pool.Exec(ctx, `DELETE FROM sources WHERE abbr = $1`, abbr)
	}()

	runtime := NewPostgresRepository(pool).PipelineRuntime()
	require.NoError(t, runtime.RecordPipelinePublishFailure(ctx, batchID, "publisher unavailable"))
	ready, err := runtime.ListReadyPipelineBatches(ctx, 1000)
	require.NoError(t, err)
	found := false
	for _, batch := range ready {
		if batch.ID == batchID {
			found = true
		}
	}
	require.True(t, found)
	require.NoError(t, runtime.MarkPipelinePublished(ctx, batchID))
	ready, err = runtime.ListReadyPipelineBatches(ctx, 1000)
	require.NoError(t, err)
	for _, batch := range ready {
		require.NotEqual(t, batchID, batch.ID)
	}
}

func TestPipelineRootFailureConvergesAndFinalizes(t *testing.T) {
	url := os.Getenv("PRISM_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set PRISM_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	require.NoError(t, err)
	defer pool.Close()
	require.NoError(t, pool.Ping(ctx))
	abbr := "it" + uuid.NewString()[:12]
	rootID := uuid.New()
	_, err = pool.Exec(ctx, `INSERT INTO sources (abbr, name, type, base_url) VALUES ($1, $2, 'PARTY', 'https://example.test')`, abbr, abbr)
	require.NoError(t, err)
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM tasks WHERE batch_id = $1`, rootID)
		_, _ = pool.Exec(ctx, `DELETE FROM batches WHERE id = $1`, rootID)
		_, _ = pool.Exec(ctx, `DELETE FROM sources WHERE abbr = $1`, abbr)
	}()
	r := NewPostgresRepository(pool)
	tasks := r.Tasks()
	require.NoError(t, tasks.EnsureBatch(ctx, repo.EnsureBatchParams{BatchID: rootID, SourceType: repo.SourceTypeParty, TraceID: "failure-test"}))
	initTask, err := tasks.CreateTask(ctx, repo.CreateTaskParams{
		BatchID: rootID, Kind: repo.TaskKindPipelineInit, SourceType: repo.SourceTypeParty, SourceAbbr: abbr,
		URL: "https://pipeline.test/failure/" + rootID.String(), TraceID: "failure-test",
	})
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE tasks SET status = 'FAILED', failure_message = 'invalid pipeline' WHERE id = $1`, initTask.ID)
	require.NoError(t, err)
	runtime := r.PipelineRuntime()
	require.NoError(t, runtime.ConvergePipelineFailure(ctx, initTask.ID, rootID, "invalid pipeline"))
	var nSubtasks int32
	require.NoError(t, pool.QueryRow(ctx, `SELECT n_subtasks FROM batches WHERE id = $1`, rootID).Scan(&nSubtasks))
	require.Equal(t, int32(1), nSubtasks)
	roots, err := runtime.FindFinishedRootBatches(ctx, 10)
	require.NoError(t, err)
	require.Len(t, roots, 1)
	require.Equal(t, int64(1), mustMarkRoot(t, runtime, rootID))
}

func mustMarkRoot(t *testing.T, runtime repo.PipelineRuntime, batchID uuid.UUID) int64 {
	t.Helper()
	rows, err := runtime.MarkRootBatchFinished(context.Background(), batchID, false, "failure-test")
	require.NoError(t, err)
	return rows
}
