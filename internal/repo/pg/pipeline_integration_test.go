package pg

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"

	pipeline "github.com/ChiaYuChang/prism/internal/analyzer/pipeline"
	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

type integrationSnapshotBuilder struct {
	inputs     []pipeline.WorkSetInput
	sourceAbbr string
}

func (*integrationSnapshotBuilder) TaskType() string { return "CAPTURE_INTEGRATION" }

func (b *integrationSnapshotBuilder) Build(_ string, _ map[string]any, input pipeline.WorkSetInput) ([]pipeline.WorkTaskSpec, error) {
	b.inputs = append(b.inputs, input)
	return []pipeline.WorkTaskSpec{{
		LogicalKey: "capture:integration", Kind: repo.TaskKindEmbedCandidate, SourceType: repo.SourceTypeParty,
		SourceAbbr: b.sourceAbbr, URL: "https://pipeline.test/integration-work", Payload: []byte(`{}`),
	}}, nil
}

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
	_, err = pool.Exec(ctx, `UPDATE batches SET purpose = 'ANALYZER_PIPELINE_ROOT' WHERE id = $1`, rootID)
	require.NoError(t, err)
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
	stages := make(map[string]repo.Task, 2)
	for _, task := range rows {
		if task.LogicalKey != nil {
			stages[*task.LogicalKey] = task
		}
	}
	first, ok := stages["pipeline:stage:first"]
	require.True(t, ok)
	second, ok := stages["pipeline:stage:second"]
	require.True(t, ok)
	require.Equal(t, int64(1), queryCount(t, pool, ctx, `SELECT COUNT(*) FROM tasks WHERE id = $1 AND next_task_id = $2`, initTask.ID, first.ID))
	require.Equal(t, int64(1), queryCount(t, pool, ctx, `SELECT COUNT(*) FROM tasks WHERE id = $1 AND previous_task_id = $2 AND next_task_id = $3`, first.ID, initTask.ID, second.ID))
	require.Equal(t, int64(1), queryCount(t, pool, ctx, `SELECT COUNT(*) FROM tasks WHERE id = $1 AND previous_task_id = $2 AND next_task_id IS NULL`, second.ID, first.ID))
	var nSubtasks int32
	require.NoError(t, pool.QueryRow(ctx, `SELECT n_subtasks FROM batches WHERE id = $1`, rootID).Scan(&nSubtasks))
	require.Equal(t, int32(3), nSubtasks)
	stageOwner := first
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

func queryCount(t *testing.T, pool *pgxpool.Pool, ctx context.Context, query string, args ...any) int64 {
	t.Helper()
	var count int64
	require.NoError(t, pool.QueryRow(ctx, query, args...).Scan(&count))
	return count
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

func TestCollectionCompletionPredicate(t *testing.T) {
	url := os.Getenv("PRISM_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set PRISM_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	require.NoError(t, err)
	defer pool.Close()
	require.NoError(t, pool.Ping(ctx))

	testCases := []struct {
		name           string
		statuses       []string
		candidateCount int
		contentCount   int
		wantFinished   bool
		wantSucceeded  bool
	}{
		{
			name:          "successful zero output",
			statuses:      []string{"COMPLETED"},
			wantFinished:  true,
			wantSucceeded: true,
		},
		{
			name:         "failed zero output",
			statuses:     []string{"FAILED"},
			wantFinished: true,
		},
		{
			name:           "successful promotion remains gated",
			statuses:       []string{"COMPLETED"},
			candidateCount: 2,
			contentCount:   1,
		},
		{
			name:           "partial failure bypasses promotion gate",
			statuses:       []string{"COMPLETED", "CANCELLED"},
			candidateCount: 2,
			contentCount:   1,
			wantFinished:   true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			abbr := "it" + uuid.NewString()[:12]
			batchID := uuid.New()
			require.NoError(t, insertTestSource(ctx, pool, abbr))
			t.Cleanup(func() { cleanupCollectionBatch(ctx, pool, batchID, abbr) })
			require.NoError(t, insertCollectionBatch(ctx, pool, batchID, abbr, tc.statuses, tc.candidateCount, tc.contentCount))

			trigger := NewPostgresRepository(pool).BatchTrigger()
			ready, queryErr := trigger.FindNewlyCompletedBatches(ctx, 100, repo.SourceTypeParty)
			require.NoError(t, queryErr)
			found := containsBatchID(ready, batchID)
			require.Equal(t, tc.wantFinished, found)
			if !tc.wantFinished {
				return
			}

			marked, markErr := trigger.MarkBatchCompleted(ctx, batchID, "collection-test")
			require.NoError(t, markErr)
			require.Equal(t, int64(1), marked)
			var succeeded bool
			require.NoError(t, pool.QueryRow(ctx, `SELECT succeeded FROM batches WHERE id = $1`, batchID).Scan(&succeeded))
			require.Equal(t, tc.wantSucceeded, succeeded)
		})
	}
}

func insertTestSource(ctx context.Context, pool *pgxpool.Pool, abbr string) error {
	_, err := pool.Exec(ctx, `INSERT INTO sources (abbr, name, type, base_url) VALUES ($1, $2, 'PARTY', 'https://example.test')`, abbr, abbr)
	return err
}

func cleanupCollectionBatch(ctx context.Context, pool *pgxpool.Pool, batchID uuid.UUID, abbr string) {
	_, _ = pool.Exec(ctx, `DELETE FROM contents WHERE batch_id = $1`, batchID)
	_, _ = pool.Exec(ctx, `DELETE FROM candidates WHERE batch_id = $1`, batchID)
	_, _ = pool.Exec(ctx, `DELETE FROM tasks WHERE batch_id = $1`, batchID)
	_, _ = pool.Exec(ctx, `DELETE FROM batches WHERE id = $1`, batchID)
	_, _ = pool.Exec(ctx, `DELETE FROM sources WHERE abbr = $1`, abbr)
}

func insertCollectionBatch(ctx context.Context, pool *pgxpool.Pool, batchID uuid.UUID, abbr string, statuses []string, candidateCount, contentCount int) error {
	if _, err := pool.Exec(ctx, `INSERT INTO batches (id, source_type, trace_id) VALUES ($1, 'PARTY', 'collection-test')`, batchID); err != nil {
		return err
	}
	for _, status := range statuses {
		if _, err := pool.Exec(ctx, `INSERT INTO tasks (id, batch_id, kind, source_type, source_abbr, url, trace_id, status) VALUES ($1, $2, 'DIRECTORY_FETCH', 'PARTY', $3, $4, 'collection-test', $5)`, uuid.New(), batchID, abbr, "https://example.test/task/"+uuid.NewString(), status); err != nil {
			return err
		}
	}
	candidateIDs := make([]uuid.UUID, candidateCount)
	for i := range candidateIDs {
		candidateIDs[i] = uuid.New()
		if _, err := pool.Exec(ctx, `INSERT INTO candidates (id, batch_id, source_abbr, trace_id, fingerprint, url, title, ingestion_method) VALUES ($1, $2, $3, 'collection-test', $4, $5, 'Candidate', 'DIRECTORY')`, candidateIDs[i], batchID, abbr, uuid.NewString()[:32], "https://example.test/candidate/"+candidateIDs[i].String()); err != nil {
			return err
		}
	}
	for i := 0; i < contentCount; i++ {
		if _, err := pool.Exec(ctx, `INSERT INTO contents (id, batch_id, type, source_abbr, candidate_id, url, title, content, trace_id, published_at, fetched_at) VALUES ($1, $2, 'ARTICLE', $3, $4, $5, 'Content', 'body', 'collection-test', NOW(), NOW())`, uuid.New(), batchID, abbr, candidateIDs[i], "https://example.test/content/"+uuid.NewString()); err != nil {
			return err
		}
	}
	return nil
}

func containsBatchID(batches []repo.Batch, batchID uuid.UUID) bool {
	for _, batch := range batches {
		if batch.ID == batchID {
			return true
		}
	}
	return false
}

func TestPipelineInputSnapshotRemainsStableAfterSourceMutation(t *testing.T) {
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
	candidate1, candidate2 := uuid.New(), uuid.New()
	content1, content2 := uuid.New(), uuid.New()
	_, err = pool.Exec(ctx, `INSERT INTO sources (abbr, name, type, base_url) VALUES ($1, $2, 'PARTY', 'https://example.test')`, abbr, abbr)
	require.NoError(t, err)
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM pipeline_input_candidates WHERE root_batch_id = $1`, rootID)
		_, _ = pool.Exec(ctx, `DELETE FROM pipeline_input_contents WHERE root_batch_id = $1`, rootID)
		_, _ = pool.Exec(ctx, `DELETE FROM contents WHERE id IN ($1, $2)`, content1, content2)
		_, _ = pool.Exec(ctx, `DELETE FROM candidates WHERE id IN ($1, $2)`, candidate1, candidate2)
		_, _ = pool.Exec(ctx, `DELETE FROM tasks WHERE batch_id = $1`, rootID)
		_, _ = pool.Exec(ctx, `DELETE FROM batches WHERE id IN ($1, $2)`, rootID, inputID)
		_, _ = pool.Exec(ctx, `DELETE FROM sources WHERE abbr = $1`, abbr)
	}()
	_, err = pool.Exec(ctx, `INSERT INTO batches (id, source_type, trace_id, completed_at, succeeded) VALUES ($1, 'PARTY', 'input', NOW(), TRUE)`, inputID)
	require.NoError(t, err)
	for i, candidateID := range []uuid.UUID{candidate1, candidate2} {
		_, err = pool.Exec(ctx, `INSERT INTO candidates (id, batch_id, source_abbr, trace_id, fingerprint, url, title, ingestion_method) VALUES ($1, $2, $3, 'trace', $4, $5, $6, 'DIRECTORY')`, candidateID, inputID, abbr, uuid.NewString()[:32], "https://example.test/candidate/"+candidateID.String(), "Candidate "+string(rune('A'+i)))
		require.NoError(t, err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO contents (id, batch_id, type, source_abbr, candidate_id, url, title, content, trace_id, published_at, fetched_at) VALUES ($1, $2, 'ARTICLE', $3, $4, $5, 'Content A', 'body', 'trace', NOW(), NOW())`, content1, inputID, abbr, candidate1, "https://example.test/content/1")
	require.NoError(t, err)

	runtime := NewPostgresRepository(pool).PipelineRuntime()
	_, err = runtime.CreatePipelineRoot(ctx, repo.CreateTaskParams{
		BatchID: rootID, ParentBatchID: &inputID, Kind: repo.TaskKindPipelineInit,
		SourceType: repo.SourceTypeParty, SourceAbbr: abbr, URL: "https://pipeline.test/snapshot/" + rootID.String(), TraceID: "root",
		PipelineDefinitionHash:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		PipelineRequestFingerprint: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	})
	require.NoError(t, err)

	candidates, err := runtime.ListPipelineInputCandidates(ctx, rootID)
	require.NoError(t, err)
	contents, err := runtime.ListPipelineInputContents(ctx, rootID)
	require.NoError(t, err)
	require.ElementsMatch(t, []uuid.UUID{candidate1, candidate2}, []uuid.UUID{candidates[0].ID, candidates[1].ID})
	require.Equal(t, abbr, candidates[0].SourceAbbr)
	require.Equal(t, content1, contents[0].ID)
	require.Equal(t, "body", contents[0].Content.Content)

	_, err = pool.Exec(ctx, `UPDATE candidates SET title = 'Mutated candidate', metadata = '{"mutated":true}' WHERE id = $1`, candidate1)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE contents SET content = 'mutated body', deleted_at = NOW(), metadata = '{"mutated":true}' WHERE id = $1`, content1)
	require.NoError(t, err)
	candidates, err = runtime.ListPipelineInputCandidates(ctx, rootID)
	require.NoError(t, err)
	contents, err = runtime.ListPipelineInputContents(ctx, rootID)
	require.NoError(t, err)
	for _, candidate := range candidates {
		if candidate.ID == candidate1 {
			require.Equal(t, "Candidate A", candidate.Title)
			require.Empty(t, candidate.Metadata)
		}
	}
	require.Equal(t, "body", contents[0].Content.Content)
	require.Nil(t, contents[0].DeletedAt)
	require.Empty(t, contents[0].Metadata)

	_, err = pool.Exec(ctx, `INSERT INTO contents (id, batch_id, type, source_abbr, candidate_id, url, title, content, trace_id, published_at, fetched_at) VALUES ($1, $2, 'ARTICLE', $3, $4, $5, 'Content B', 'body', 'trace', NOW(), NOW())`, content2, inputID, abbr, candidate2, "https://example.test/content/2")
	require.NoError(t, err)
	candidates, err = runtime.ListPipelineInputCandidates(ctx, rootID)
	require.NoError(t, err)
	contents, err = runtime.ListPipelineInputContents(ctx, rootID)
	require.NoError(t, err)
	require.Len(t, candidates, 2)
	require.Len(t, contents, 1)
}

func TestPipelineHandlersReuseSnapshotAcrossStagesMutationsAndReplay(t *testing.T) {
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
	candidate1, candidate2 := uuid.New(), uuid.New()
	content1, content2 := uuid.New(), uuid.New()
	stage1ID, stage2ID := uuid.New(), uuid.New()
	_, err = pool.Exec(ctx, `INSERT INTO sources (abbr, name, type, base_url) VALUES ($1, $2, 'PARTY', 'https://example.test')`, abbr, abbr)
	require.NoError(t, err)
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM pipeline_input_candidates WHERE root_batch_id = $1`, rootID)
		_, _ = pool.Exec(ctx, `DELETE FROM pipeline_input_contents WHERE root_batch_id = $1`, rootID)
		_, _ = pool.Exec(ctx, `DELETE FROM tasks WHERE batch_id IN (SELECT id FROM batches WHERE parent_id = $1)`, rootID)
		_, _ = pool.Exec(ctx, `DELETE FROM tasks WHERE batch_id = $1`, rootID)
		_, _ = pool.Exec(ctx, `DELETE FROM batches WHERE parent_id = $1`, rootID)
		_, _ = pool.Exec(ctx, `DELETE FROM contents WHERE id IN ($1, $2)`, content1, content2)
		_, _ = pool.Exec(ctx, `DELETE FROM candidates WHERE id IN ($1, $2)`, candidate1, candidate2)
		_, _ = pool.Exec(ctx, `DELETE FROM batches WHERE id = $1`, inputID)
		_, _ = pool.Exec(ctx, `DELETE FROM sources WHERE abbr = $1`, abbr)
	}()
	_, err = pool.Exec(ctx, `INSERT INTO batches (id, source_type, trace_id, completed_at, succeeded) VALUES ($1, 'PARTY', 'input', NOW(), TRUE)`, inputID)
	require.NoError(t, err)
	insertCandidate := func(id uuid.UUID, suffix string) {
		_, insertErr := pool.Exec(ctx, `INSERT INTO candidates (id, batch_id, source_abbr, trace_id, fingerprint, url, title, ingestion_method) VALUES ($1, $2, $3, 'trace', $4, $5, $6, 'DIRECTORY')`, id, inputID, abbr, uuid.NewString()[:32], "https://example.test/candidate/"+suffix, "Candidate "+suffix)
		require.NoError(t, insertErr)
	}
	insertCandidate(candidate1, "one")
	_, err = pool.Exec(ctx, `INSERT INTO contents (id, batch_id, type, source_abbr, candidate_id, url, title, content, trace_id, published_at, fetched_at) VALUES ($1, $2, 'ARTICLE', $3, $4, $5, 'Content one', 'body', 'trace', NOW(), NOW())`, content1, inputID, abbr, candidate1, "https://example.test/content/one")
	require.NoError(t, err)

	r := NewPostgresRepository(pool)
	runtimeRepo := r.PipelineRuntime()
	_, err = runtimeRepo.CreatePipelineRoot(ctx, repo.CreateTaskParams{
		BatchID: rootID, ParentBatchID: &inputID, Kind: repo.TaskKindPipelineInit,
		SourceType: repo.SourceTypeParty, SourceAbbr: abbr, URL: "https://pipeline.test/root/" + rootID.String(), TraceID: "root",
		PipelineDefinitionHash:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		PipelineRequestFingerprint: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	})
	require.NoError(t, err)
	stagePayload := func(name string) []byte {
		payload, marshalErr := json.Marshal(pipeline.StageSpec{Name: name, Config: map[string]any{"task_type": "CAPTURE_INTEGRATION"}})
		require.NoError(t, marshalErr)
		return payload
	}
	for _, stage := range []struct {
		id   uuid.UUID
		name string
	}{
		{stage1ID, "stage_one"}, {stage2ID, "stage_two"},
	} {
		_, err = r.Tasks().CreateTask(ctx, repo.CreateTaskParams{
			BatchID: rootID, Kind: repo.TaskKindPipelineStage, SourceType: repo.SourceTypeParty, SourceAbbr: abbr,
			URL: "https://pipeline.test/stage/" + stage.name, Payload: stagePayload(stage.name), TraceID: "root",
			LogicalKey: stringPtr("pipeline:stage:" + stage.name),
		})
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `UPDATE tasks SET id = $1, status = 'RUNNING' WHERE batch_id = $2 AND url = $3`, stage.id, rootID, "https://pipeline.test/stage/"+stage.name)
		require.NoError(t, err)
	}
	builder := &integrationSnapshotBuilder{sourceAbbr: abbr}
	registry, err := pipeline.NewRegistry(builder)
	require.NoError(t, err)
	coordinator, err := pipeline.NewCoordinator(r.Tasks(), runtimeRepo, r.Scheduler(), registry)
	require.NoError(t, err)
	handler, err := pipeline.NewHandler(r.Tasks(), r.Scheduler(), runtimeRepo, coordinator, 3, "")
	require.NoError(t, err)
	runStage := func(id uuid.UUID) {
		ack, stageErr := handler.HandleTaskSignal(ctx, message.TaskSignal{TaskID: id, BatchID: rootID, Kind: repo.TaskKindPipelineStage}, pipeline.PipelineSpec{})
		require.NoError(t, stageErr)
		require.True(t, ack)
	}
	runStage(stage1ID)

	insertCandidate(candidate2, "two")
	_, err = pool.Exec(ctx, `INSERT INTO contents (id, batch_id, type, source_abbr, candidate_id, url, title, content, trace_id, published_at, fetched_at) VALUES ($1, $2, 'ARTICLE', $3, $4, $5, 'Content two', 'body', 'trace', NOW(), NOW())`, content2, inputID, abbr, candidate2, "https://example.test/content/two")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE contents SET deleted_at = NOW() WHERE id = $1`, content1)
	require.NoError(t, err)
	runStage(stage2ID)
	runStage(stage1ID)

	require.Len(t, builder.inputs, 3)
	for _, input := range builder.inputs {
		require.ElementsMatch(t, []uuid.UUID{candidate1}, input.CandidateIDs)
		require.ElementsMatch(t, []uuid.UUID{content1}, input.ContentIDs)
	}
}

func TestPipelineRootIdempotencyIsSemanticAndConcurrent(t *testing.T) {
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
	inputID, root1, root2 := uuid.New(), uuid.New(), uuid.New()
	_, err = pool.Exec(ctx, `INSERT INTO sources (abbr, name, type, base_url) VALUES ($1, $2, 'PARTY', 'https://example.test')`, abbr, abbr)
	require.NoError(t, err)
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM pipeline_input_candidates WHERE root_batch_id IN ($1, $2)`, root1, root2)
		_, _ = pool.Exec(ctx, `DELETE FROM pipeline_input_contents WHERE root_batch_id IN ($1, $2)`, root1, root2)
		_, _ = pool.Exec(ctx, `DELETE FROM tasks WHERE batch_id IN ($1, $2)`, root1, root2)
		_, _ = pool.Exec(ctx, `DELETE FROM batches WHERE id IN ($1, $2, $3)`, root1, root2, inputID)
		_, _ = pool.Exec(ctx, `DELETE FROM sources WHERE abbr = $1`, abbr)
	}()
	_, err = pool.Exec(ctx, `INSERT INTO batches (id, source_type, trace_id, completed_at, succeeded) VALUES ($1, 'PARTY', 'input', NOW(), TRUE)`, inputID)
	require.NoError(t, err)
	key := "concurrent-key"
	logicalKey := "pipeline:init"
	makeArg := func(root uuid.UUID, definition, fingerprint string) repo.CreateTaskParams {
		return repo.CreateTaskParams{
			BatchID: root, ParentBatchID: &inputID, Kind: repo.TaskKindPipelineInit, LogicalKey: &logicalKey,
			SourceType: repo.SourceTypeParty, SourceAbbr: abbr, URL: "https://pipeline.test/idempotent/" + root.String(), TraceID: "root",
			PipelineDefinitionHash: definition, PipelineIdempotencyKey: &key, PipelineRequestFingerprint: fingerprint,
		}
	}
	definition := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	fingerprint := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	runtime := NewPostgresRepository(pool).PipelineRuntime()
	results := make(chan struct {
		task repo.Task
		err  error
	}, 2)
	go func() {
		task, createErr := runtime.CreatePipelineRoot(ctx, makeArg(root1, definition, fingerprint))
		results <- struct {
			task repo.Task
			err  error
		}{task, createErr}
	}()
	go func() {
		task, createErr := runtime.CreatePipelineRoot(ctx, makeArg(root2, definition, fingerprint))
		results <- struct {
			task repo.Task
			err  error
		}{task, createErr}
	}()
	var recovered repo.Task
	active := 0
	for i := 0; i < 2; i++ {
		result := <-results
		if result.err == nil {
			active++
		} else {
			require.ErrorIs(t, result.err, repo.ErrTaskAlreadyActive)
		}
		if recovered.ID == uuid.Nil {
			recovered = result.task
		} else {
			require.Equal(t, recovered.ID, result.task.ID)
		}
	}
	require.Equal(t, 1, active)
	var rootCount, taskCount, snapshotCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM batches WHERE parent_id = $1 AND purpose = 'ANALYZER_PIPELINE_ROOT'`, inputID).Scan(&rootCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM tasks WHERE kind = 'PIPELINE_INIT' AND batch_id IN (SELECT id FROM batches WHERE parent_id = $1)`, inputID).Scan(&taskCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM pipeline_input_candidates WHERE root_batch_id IN (SELECT id FROM batches WHERE parent_id = $1)`, inputID).Scan(&snapshotCount))
	require.Equal(t, 1, rootCount)
	require.Equal(t, 1, taskCount)
	require.Zero(t, snapshotCount)

	_, err = runtime.CreatePipelineRoot(ctx, makeArg(uuid.New(), definition, "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"))
	require.ErrorIs(t, err, repo.ErrPipelineIdempotencyConflict)
}

func TestCreatePipelineRootCapturesConsistentRepeatableReadSnapshot(t *testing.T) {
	url := os.Getenv("PRISM_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set PRISM_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	require.NoError(t, err)
	defer pool.Close()
	require.NoError(t, pool.Ping(ctx))

	testCases := []struct {
		name           string
		initialContent bool
		mutate         func(t *testing.T, inputID uuid.UUID, abbr string)
		wantCandidates int
		wantContents   int
		wantContent    string
	}{
		{
			name: "concurrent candidate and content insert is excluded together",
			mutate: func(t *testing.T, inputID uuid.UUID, abbr string) {
				candidateID := uuid.New()
				_, err := pool.Exec(ctx, `INSERT INTO candidates (id, batch_id, source_abbr, trace_id, fingerprint, url, title, ingestion_method) VALUES ($1, $2, $3, 'snapshot-test', $4, $5, 'Concurrent candidate', 'DIRECTORY')`, candidateID, inputID, abbr, uuid.NewString()[:32], "https://example.test/candidate/concurrent")
				require.NoError(t, err)
				_, err = pool.Exec(ctx, `INSERT INTO contents (id, batch_id, type, source_abbr, candidate_id, url, title, content, trace_id, published_at, fetched_at) VALUES ($1, $2, 'ARTICLE', $3, $4, 'https://example.test/content/concurrent', 'Concurrent content', 'concurrent body', 'snapshot-test', NOW(), NOW())`, uuid.New(), inputID, abbr, candidateID)
				require.NoError(t, err)
			},
			wantCandidates: 1,
		},
		{
			name:           "concurrent soft delete preserves active content revision",
			initialContent: true,
			mutate: func(t *testing.T, inputID uuid.UUID, _ string) {
				_, err := pool.Exec(ctx, `UPDATE contents SET deleted_at = NOW(), content = 'mutated after snapshot' WHERE batch_id = $1`, inputID)
				require.NoError(t, err)
			},
			wantCandidates: 1,
			wantContents:   1,
			wantContent:    "original body",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			abbr := "it" + uuid.NewString()[:12]
			inputID, rootID := uuid.New(), uuid.New()
			require.NoError(t, insertTestSource(ctx, pool, abbr))
			t.Cleanup(func() { cleanupPipelineSnapshotTest(ctx, pool, inputID, rootID, abbr) })
			require.NoError(t, insertCompletedPipelineInput(ctx, pool, inputID, abbr, tc.initialContent))

			locked := make(chan struct{})
			resume := make(chan struct{})
			runtime := &PGPipelineRuntime{
				db: pool,
				q:  New(pool),
				afterPipelineInputLock: func(context.Context) error {
					close(locked)
					<-resume
					return nil
				},
			}
			result := make(chan error, 1)
			go func() {
				_, createErr := runtime.CreatePipelineRoot(ctx, pipelineRootParams(rootID, inputID, abbr, "snapshot-"+uuid.NewString()))
				result <- createErr
			}()
			<-locked
			tc.mutate(t, inputID, abbr)
			close(resume)
			require.NoError(t, <-result)

			candidates, listErr := runtime.ListPipelineInputCandidates(ctx, rootID)
			require.NoError(t, listErr)
			contents, listErr := runtime.ListPipelineInputContents(ctx, rootID)
			require.NoError(t, listErr)
			require.Len(t, candidates, tc.wantCandidates)
			require.Len(t, contents, tc.wantContents)
			if tc.wantContent != "" {
				require.Equal(t, tc.wantContent, contents[0].Content.Content)
				require.Nil(t, contents[0].DeletedAt)
			}
		})
	}
}

func TestCreatePipelineRootSerializationRetryIsIdempotent(t *testing.T) {
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
	require.NoError(t, insertTestSource(ctx, pool, abbr))
	t.Cleanup(func() { cleanupPipelineSnapshotTest(ctx, pool, inputID, rootID, abbr) })
	require.NoError(t, insertCompletedPipelineInput(ctx, pool, inputID, abbr, true))

	arg := pipelineRootParams(rootID, inputID, abbr, "serialization-"+uuid.NewString())
	snapshot := make(chan struct{})
	resume := make(chan struct{})
	failingRuntime := &PGPipelineRuntime{db: pool, q: New(pool), afterPipelineSnapshot: func(context.Context) error {
		close(snapshot)
		<-resume
		return nil
	}}
	result := make(chan error, 1)
	go func() {
		_, createErr := failingRuntime.CreatePipelineRoot(ctx, arg)
		result <- createErr
	}()
	<-snapshot
	_, err = pool.Exec(ctx, `UPDATE batches SET updated_at = NOW() WHERE id = $1`, inputID)
	require.NoError(t, err)
	close(resume)
	err = <-result
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, "40001", pgErr.Code)

	created, err := NewPostgresRepository(pool).PipelineRuntime().CreatePipelineRoot(ctx, arg)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, created.ID)
	var roots, initTasks, candidates, contents int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM batches WHERE id = $1`, rootID).Scan(&roots))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM tasks WHERE batch_id = $1 AND kind = 'PIPELINE_INIT'`, rootID).Scan(&initTasks))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM pipeline_input_candidates WHERE root_batch_id = $1`, rootID).Scan(&candidates))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM pipeline_input_contents WHERE root_batch_id = $1`, rootID).Scan(&contents))
	require.Equal(t, 1, roots)
	require.Equal(t, 1, initTasks)
	require.Equal(t, 1, candidates)
	require.Equal(t, 1, contents)
}

func insertCompletedPipelineInput(ctx context.Context, pool *pgxpool.Pool, inputID uuid.UUID, abbr string, withContent bool) error {
	if _, err := pool.Exec(ctx, `INSERT INTO batches (id, source_type, trace_id, completed_at, succeeded) VALUES ($1, 'PARTY', 'snapshot-test', NOW(), TRUE)`, inputID); err != nil {
		return err
	}
	candidateID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO candidates (id, batch_id, source_abbr, trace_id, fingerprint, url, title, ingestion_method) VALUES ($1, $2, $3, 'snapshot-test', $4, $5, 'Original candidate', 'DIRECTORY')`, candidateID, inputID, abbr, uuid.NewString()[:32], "https://example.test/candidate/"+candidateID.String()); err != nil {
		return err
	}
	if !withContent {
		return nil
	}
	_, err := pool.Exec(ctx, `INSERT INTO contents (id, batch_id, type, source_abbr, candidate_id, url, title, content, trace_id, published_at, fetched_at) VALUES ($1, $2, 'ARTICLE', $3, $4, $5, 'Original content', 'original body', 'snapshot-test', NOW(), NOW())`, uuid.New(), inputID, abbr, candidateID, "https://example.test/content/"+candidateID.String())
	return err
}

func cleanupPipelineSnapshotTest(ctx context.Context, pool *pgxpool.Pool, inputID, rootID uuid.UUID, abbr string) {
	_, _ = pool.Exec(ctx, `DELETE FROM pipeline_input_candidates WHERE root_batch_id = $1`, rootID)
	_, _ = pool.Exec(ctx, `DELETE FROM pipeline_input_contents WHERE root_batch_id = $1`, rootID)
	_, _ = pool.Exec(ctx, `DELETE FROM tasks WHERE batch_id = $1`, rootID)
	_, _ = pool.Exec(ctx, `DELETE FROM batches WHERE id = $1`, rootID)
	cleanupCollectionBatch(ctx, pool, inputID, abbr)
}

func pipelineRootParams(rootID, inputID uuid.UUID, abbr, idempotencyKey string) repo.CreateTaskParams {
	logicalKey := "pipeline:init"
	return repo.CreateTaskParams{
		BatchID: rootID, ParentBatchID: &inputID, Kind: repo.TaskKindPipelineInit, LogicalKey: &logicalKey,
		SourceType: repo.SourceTypeParty, SourceAbbr: abbr, URL: "https://pipeline.test/root/" + rootID.String(), TraceID: "snapshot-test",
		PipelineDefinitionHash:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		PipelineIdempotencyKey:     &idempotencyKey,
		PipelineRequestFingerprint: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}
}

func TestPipelineRootRejectsFailedInput(t *testing.T) {
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
	_, err = pool.Exec(ctx, `INSERT INTO batches (id, source_type, trace_id, completed_at, succeeded) VALUES ($1, 'PARTY', 'input', NOW(), FALSE)`, inputID)
	require.NoError(t, err)
	_, err = NewPostgresRepository(pool).PipelineRuntime().CreatePipelineRoot(ctx, repo.CreateTaskParams{
		BatchID: rootID, ParentBatchID: &inputID, Kind: repo.TaskKindPipelineInit, SourceType: repo.SourceTypeParty,
		SourceAbbr: abbr, URL: "https://pipeline.test/failed/" + rootID.String(), TraceID: "root",
	})
	require.ErrorIs(t, err, repo.ErrPipelineInputFailed)
	var rootCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM batches WHERE id = $1`, rootID).Scan(&rootCount))
	require.Zero(t, rootCount)
}

func TestPipelineBatchDoesNotFinishBeforeDeclaredCardinality(t *testing.T) {
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
	rootID, ownerID, childID := uuid.New(), uuid.New(), uuid.New()
	task1, task2 := uuid.New(), uuid.New()
	_, err = pool.Exec(ctx, `INSERT INTO sources (abbr, name, type, base_url) VALUES ($1, $2, 'PARTY', 'https://example.test')`, abbr, abbr)
	require.NoError(t, err)
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM tasks WHERE batch_id IN ($1, $2)`, rootID, childID)
		_, _ = pool.Exec(ctx, `DELETE FROM tasks WHERE id = $1`, ownerID)
		_, _ = pool.Exec(ctx, `DELETE FROM batches WHERE id IN ($1, $2)`, rootID, childID)
		_, _ = pool.Exec(ctx, `DELETE FROM sources WHERE abbr = $1`, abbr)
	}()
	_, err = pool.Exec(ctx, `INSERT INTO batches (id, source_type, trace_id, purpose) VALUES ($1, 'PARTY', 'root', 'ANALYZER_PIPELINE_ROOT')`, rootID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO tasks (id, batch_id, kind, source_type, source_abbr, url, trace_id) VALUES ($1, $2, 'PIPELINE_STAGE', 'PARTY', $3, 'https://pipeline.test/owner', 'root')`, ownerID, rootID, abbr)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO batches (id, source_type, trace_id, parent_id, parent_task_id, purpose, n_subtasks) VALUES ($1, 'PARTY', 'child', $2, $3, 'ANALYZER_PIPELINE_STAGE', 2)`, childID, rootID, ownerID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO tasks (id, batch_id, kind, source_type, source_abbr, url, trace_id, status) VALUES ($1, $2, 'EMBED_CANDIDATE', 'PARTY', $3, 'https://pipeline.test/work/1', 'child', 'COMPLETED')`, task1, childID, abbr)
	require.NoError(t, err)
	runtime := NewPostgresRepository(pool).PipelineRuntime()
	finished, err := runtime.FindFinishedBatches(ctx, 10)
	require.NoError(t, err)
	for _, batch := range finished {
		require.NotEqual(t, childID, batch.ID)
	}
	_, err = pool.Exec(ctx, `INSERT INTO tasks (id, batch_id, kind, source_type, source_abbr, url, trace_id, status) VALUES ($1, $2, 'EMBED_CANDIDATE', 'PARTY', $3, 'https://pipeline.test/work/2', 'child', 'FAILED')`, task2, childID, abbr)
	require.NoError(t, err)
	finished, err = runtime.FindFinishedBatches(ctx, 10)
	require.NoError(t, err)
	require.Len(t, finished, 1)
	require.Equal(t, childID, finished[0].ID)
	require.False(t, *finished[0].Succeeded)
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
	_, err = pool.Exec(ctx, `UPDATE batches SET purpose = 'ANALYZER_PIPELINE_STAGE' WHERE id = $1`, batchID)
	require.NoError(t, err)
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
	_, err = pool.Exec(ctx, `INSERT INTO batches (id, source_type, trace_id, parent_id, parent_task_id, purpose, n_subtasks, completed_at) VALUES ($1, 'PARTY', 'publish-test', $2, $3, 'ANALYZER_PIPELINE_STAGE', 0, NOW())`, batchID, parentID, ownerID)
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

func TestInitializePipelineStageFinishesEmptyChildImmediately(t *testing.T) {
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
	rootID, ownerID, childID := uuid.New(), uuid.New(), uuid.New()
	_, err = pool.Exec(ctx, `INSERT INTO sources (abbr, name, type, base_url) VALUES ($1, $2, 'PARTY', 'https://example.test')`, abbr, abbr)
	require.NoError(t, err)
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM tasks WHERE batch_id = $1`, rootID)
		_, _ = pool.Exec(ctx, `DELETE FROM batches WHERE id IN ($1, $2)`, childID, rootID)
		_, _ = pool.Exec(ctx, `DELETE FROM sources WHERE abbr = $1`, abbr)
	}()
	_, err = pool.Exec(ctx, `INSERT INTO batches (id, source_type, trace_id, purpose) VALUES ($1, 'PARTY', 'empty-child', 'ANALYZER_PIPELINE_ROOT')`, rootID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO tasks (id, batch_id, kind, source_type, source_abbr, url, trace_id) VALUES ($1, $2, 'PIPELINE_STAGE', 'PARTY', $3, 'https://pipeline.test/empty-owner', 'empty-child')`, ownerID, rootID, abbr)
	require.NoError(t, err)

	runtime := NewPostgresRepository(pool).PipelineRuntime()
	gotChildID, err := runtime.InitializePipelineStage(ctx, repo.InitializePipelineStageParams{
		ChildBatchID: childID, ParentBatchID: rootID, ParentTaskID: ownerID,
		SourceType: repo.SourceTypeParty, TraceID: "empty-child", NSubtasks: 0,
	})
	require.NoError(t, err)
	require.Equal(t, childID, gotChildID)
	var completed bool
	var succeeded bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT completed_at IS NOT NULL, succeeded FROM batches WHERE id = $1`, childID).Scan(&completed, &succeeded))
	require.True(t, completed)
	require.True(t, succeeded)
	ready, err := runtime.ListReadyPipelineBatches(ctx, 100)
	require.NoError(t, err)
	found := false
	for _, batch := range ready {
		if batch.ID == childID {
			found = true
			break
		}
	}
	require.True(t, found)
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
	inputID, rootID := uuid.New(), uuid.New()
	_, err = pool.Exec(ctx, `INSERT INTO sources (abbr, name, type, base_url) VALUES ($1, $2, 'PARTY', 'https://example.test')`, abbr, abbr)
	require.NoError(t, err)
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM tasks WHERE batch_id = $1`, rootID)
		_, _ = pool.Exec(ctx, `DELETE FROM batches WHERE id IN ($1, $2)`, rootID, inputID)
		_, _ = pool.Exec(ctx, `DELETE FROM sources WHERE abbr = $1`, abbr)
	}()
	r := NewPostgresRepository(pool)
	tasks := r.Tasks()
	require.NoError(t, tasks.EnsureBatch(ctx, repo.EnsureBatchParams{BatchID: inputID, SourceType: repo.SourceTypeParty, TraceID: "failure-input"}))
	require.NoError(t, tasks.EnsureBatch(ctx, repo.EnsureBatchParams{BatchID: rootID, ParentBatchID: &inputID, SourceType: repo.SourceTypeParty, TraceID: "failure-test"}))
	_, err = pool.Exec(ctx, `UPDATE batches SET purpose = 'ANALYZER_PIPELINE_ROOT' WHERE id = $1`, rootID)
	require.NoError(t, err)
	initTask, err := tasks.CreateTask(ctx, repo.CreateTaskParams{
		BatchID: rootID, Kind: repo.TaskKindPipelineInit, SourceType: repo.SourceTypeParty, SourceAbbr: abbr,
		URL: "https://pipeline.test/failure/" + rootID.String(), TraceID: "failure-test",
	})
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE tasks SET status = 'RUNNING', retry_count = 1 WHERE id = $1`, initTask.ID)
	require.NoError(t, err)
	runtime := r.PipelineRuntime()
	terminal, err := runtime.FailPipelineTask(ctx, initTask.ID, rootID, 1, "invalid pipeline")
	require.NoError(t, err)
	require.True(t, terminal)
	var nSubtasks int32
	require.NoError(t, pool.QueryRow(ctx, `SELECT n_subtasks FROM batches WHERE id = $1`, rootID).Scan(&nSubtasks))
	require.Equal(t, int32(1), nSubtasks)
	roots, err := runtime.FindFinishedRootBatches(ctx, 10)
	require.NoError(t, err)
	require.Len(t, roots, 1)
	require.Equal(t, int64(1), mustMarkRoot(t, runtime, rootID))
}

func TestFailPipelineInitRollsBackWhenRootFailureCannotBeRecorded(t *testing.T) {
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
	inputID, rootID, initID := uuid.New(), uuid.New(), uuid.New()
	triggerName := "reject_root_failure_" + uuid.NewString()[:8]
	functionName := triggerName + "_fn"
	_, err = pool.Exec(ctx, `INSERT INTO sources (abbr, name, type, base_url) VALUES ($1, $2, 'PARTY', 'https://example.test')`, abbr, abbr)
	require.NoError(t, err)
	defer func() {
		_, _ = pool.Exec(ctx, `DROP TRIGGER IF EXISTS `+triggerName+` ON batches`)
		_, _ = pool.Exec(ctx, `DROP FUNCTION IF EXISTS `+functionName+`()`)
		_, _ = pool.Exec(ctx, `DELETE FROM tasks WHERE batch_id = $1`, rootID)
		_, _ = pool.Exec(ctx, `DELETE FROM batches WHERE id IN ($1, $2)`, rootID, inputID)
		_, _ = pool.Exec(ctx, `DELETE FROM sources WHERE abbr = $1`, abbr)
	}()
	r := NewPostgresRepository(pool)
	tasks := r.Tasks()
	require.NoError(t, tasks.EnsureBatch(ctx, repo.EnsureBatchParams{BatchID: inputID, SourceType: repo.SourceTypeParty, TraceID: "rollback-input"}))
	require.NoError(t, tasks.EnsureBatch(ctx, repo.EnsureBatchParams{BatchID: rootID, ParentBatchID: &inputID, SourceType: repo.SourceTypeParty, TraceID: "rollback-test"}))
	_, err = pool.Exec(ctx, `UPDATE batches SET purpose = 'ANALYZER_PIPELINE_ROOT' WHERE id = $1`, rootID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO tasks (id, batch_id, kind, source_type, source_abbr, url, trace_id, status, retry_count) VALUES ($1, $2, 'PIPELINE_INIT', 'PARTY', $3, 'https://pipeline.test/rollback', 'rollback-test', 'RUNNING', 1)`, initID, rootID, abbr)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `CREATE FUNCTION `+functionName+`() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'reject root failure'; END; $$`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `CREATE TRIGGER `+triggerName+` BEFORE UPDATE OF n_subtasks ON batches FOR EACH ROW WHEN (NEW.id = '`+rootID.String()+`'::uuid) EXECUTE FUNCTION `+functionName+`()`)
	require.NoError(t, err)

	_, err = r.PipelineRuntime().FailPipelineTask(ctx, initID, rootID, 1, "invalid pipeline")
	require.Error(t, err)
	var status string
	var nSubtasks *int32
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM tasks WHERE id = $1`, initID).Scan(&status))
	require.NoError(t, pool.QueryRow(ctx, `SELECT n_subtasks FROM batches WHERE id = $1`, rootID).Scan(&nSubtasks))
	require.Equal(t, "RUNNING", status)
	require.Nil(t, nSubtasks)
}

func mustMarkRoot(t *testing.T, runtime repo.PipelineRuntime, batchID uuid.UUID) int64 {
	t.Helper()
	rows, err := runtime.MarkRootBatchFinished(context.Background(), batchID, false, "failure-test")
	require.NoError(t, err)
	return rows
}
