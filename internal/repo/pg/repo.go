package pg

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ChiaYuChang/prism/pkg/utils"

	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/ChiaYuChang/prism/pkg/pgconv"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	pgvector "github.com/pgvector/pgvector-go"
)

// Root repository constructor.
func NewPostgresRepository(db DBTX) *PGRepository {
	return &PGRepository{db: db, q: New(db)}
}

// Repository roots.
type PGRepository struct {
	db DBTX
	q  *Queries
}

// Worker-scoped repository adapters.
type PGScheduler struct {
	q *Queries
}

type PGScout struct {
	q *Queries
}

type PGTasks struct {
	db DBTX
	q  *Queries
}

type PGPipeline struct {
	db DBTX
	q  *Queries
}

type PGPipelineRuntime struct {
	db                     DBTX
	q                      *Queries
	afterPipelineInputLock func(context.Context) error
	afterPipelineSnapshot  func(context.Context) error
}

type PGEmbeddings struct {
	q *Queries
}

type PGAnalysis struct {
	q *Queries
}

type PGBatchTrigger struct {
	q *Queries
}

type PGUserFetches struct {
	q *Queries
}

type PGAnalysisRuns struct {
	q *Queries
}

type PGReports struct {
	db DBTX
	q  *Queries
}

type PGSchedules struct {
	db DBTX
	q  *Queries
}

type PGOperator struct {
	q *Queries
}

type PGPrompts struct {
	q *Queries
}

type PGTokens struct {
	q *Queries
}

type PGSources struct {
	q *Queries
}

type PGModels struct {
	q *Queries
}

type pgBeginner interface {
	Begin(context.Context) (pgx.Tx, error)
}

var _ repo.Repository = (*PGRepository)(nil)
var _ repo.Scheduler = (*PGScheduler)(nil)
var _ repo.Scout = (*PGScout)(nil)
var _ repo.Tasks = (*PGTasks)(nil)
var _ repo.Pipeline = (*PGPipeline)(nil)
var _ repo.PipelineRuntime = (*PGPipelineRuntime)(nil)
var _ repo.Embeddings = (*PGEmbeddings)(nil)
var _ repo.Analysis = (*PGAnalysis)(nil)
var _ repo.BatchTrigger = (*PGBatchTrigger)(nil)
var _ repo.UserFetches = (*PGUserFetches)(nil)
var _ repo.AnalysisRuns = (*PGAnalysisRuns)(nil)
var _ repo.Reports = (*PGReports)(nil)
var _ repo.Schedules = (*PGSchedules)(nil)
var _ repo.Operator = (*PGOperator)(nil)
var _ repo.Prompts = (*PGPrompts)(nil)
var _ repo.Tokens = (*PGTokens)(nil)
var _ repo.Sources = (*PGSources)(nil)
var _ repo.Models = (*PGModels)(nil)

// Repository root getters.
func (r *PGRepository) Scheduler() repo.Scheduler {
	return &PGScheduler{q: r.q}
}

func (r *PGRepository) Scout() repo.Scout {
	return &PGScout{q: r.q}
}

func (r *PGRepository) Tasks() repo.Tasks {
	return &PGTasks{db: r.db, q: r.q}
}

func (r *PGRepository) Pipeline() repo.Pipeline {
	return &PGPipeline{db: r.db, q: r.q}
}

func (r *PGRepository) PipelineRuntime() repo.PipelineRuntime {
	return &PGPipelineRuntime{db: r.db, q: r.q}
}

func (r *PGRepository) Reports() repo.Reports {
	return &PGReports{db: r.db, q: r.q}
}

func (r *PGPipelineRuntime) InitializePipeline(ctx context.Context, arg repo.InitializePipelineParams) error {
	beginner, ok := r.db.(pgBeginner)
	if !ok {
		return fmt.Errorf("postgres repository does not support transactions")
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := r.q.WithTx(tx)
	if _, err := qtx.LockBatchForTaskInsert(ctx, arg.BatchID); err != nil {
		return fmt.Errorf("lock pipeline root batch %s: %w", arg.BatchID, err)
	}
	previousID := arg.InitTaskID
	for _, task := range arg.Tasks {
		task.PreviousTaskID = &previousID
		created, err := qtx.CreateTask(ctx, repoCreateTaskParamsToDB(task))
		if err != nil {
			return fmt.Errorf("create pipeline control task: %w", err)
		}
		expectedPayload := task.Payload
		if len(expectedPayload) == 0 {
			expectedPayload = []byte(`{}`)
		}
		if !created.Inserted && (!bytes.Equal(created.Payload, expectedPayload) || !created.PreviousTaskID.Valid || created.PreviousTaskID.Bytes != previousID) {
			return fmt.Errorf("%w: stage %v", repo.ErrPipelinePlanConflict, task.LogicalKey)
		}
		if rows, err := qtx.LinkTaskSuccessor(ctx, LinkTaskSuccessorParams{PredecessorID: previousID, SuccessorID: pgconv.UUIDToPgUUID(created.ID)}); err != nil {
			return fmt.Errorf("link pipeline control task: %w", err)
		} else if rows != 1 {
			return repo.ErrPipelinePlanConflict
		}
		previousID = created.ID
	}
	if _, err := qtx.SetBatchNSubtasks(ctx, SetBatchNSubtasksParams{
		BatchID: arg.BatchID, NSubtasks: pgconv.Int32PtrToPgInt4(&arg.NSubtasks),
	}); err != nil {
		return fmt.Errorf("set pipeline root subtasks: %w", err)
	}
	if err := qtx.CompleteTask(ctx, arg.InitTaskID); err != nil {
		return fmt.Errorf("complete pipeline init task: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit pipeline initialization: %w", err)
	}
	return nil
}

func (r *PGPipelineRuntime) GetPipelineBatch(ctx context.Context, batchID uuid.UUID) (repo.Batch, error) {
	row, err := r.q.GetBatchByID(ctx, batchID)
	if err != nil {
		return repo.Batch{}, err
	}
	batch := dbBatchToRepoBatch(
		row.ID, string(row.Purpose), pgconv.PgUUIDToUUIDPtr(row.ParentID), pgconv.PgInt4ToInt32Ptr(row.NSubtasks),
		pgconv.PgUUIDToUUIDPtr(row.ParentTaskID), pgconv.PgBoolToBoolPtr(row.Succeeded), string(row.SourceType),
		pgconv.PgTextToStringPtr(row.TraceID), *pgconv.PgTimestamptzToTimePtr(row.CreatedAt),
		*pgconv.PgTimestamptzToTimePtr(row.UpdatedAt), pgconv.PgTimestamptzToTimePtr(row.CompletedAt),
		pgconv.PgTimestamptzToTimePtr(row.PublishedAt), pgconv.PgTimestamptzToTimePtr(row.LastPublishAttemptAt),
		row.PublishRetryCount, pgconv.PgTextToStringPtr(row.PublishError), pgconv.PgTimestamptzToTimePtr(row.StalledAt),
	)
	batch.PipelineInputSnapshotAt = pgconv.PgTimestamptzToTimePtr(row.PipelineInputSnapshotAt)
	batch.AnalysisExecutionID = pgconv.PgUUIDToUUIDPtr(row.AnalysisExecutionID)
	return batch, nil
}

func (r *PGPipelineRuntime) ListPipelineInputCandidates(ctx context.Context, rootBatchID uuid.UUID) ([]repo.PipelineInputCandidate, error) {
	rows, err := r.q.ListPipelineInputCandidates(ctx, rootBatchID)
	if err != nil {
		return nil, err
	}
	out := make([]repo.PipelineInputCandidate, len(rows))
	for i, row := range rows {
		out[i] = repo.PipelineInputCandidate{Candidate: repo.Candidate{
			ID: row.CandidateID, BatchID: row.BatchID, Fingerprint: row.Fingerprint, SourceAbbr: row.SourceAbbr,
			Title: row.Title, URL: row.Url, Description: pgconv.PgTextToStringPtr(row.Description),
			PublishedAt: pgconv.PgTimestamptzToTimePtr(row.PublishedAt), DiscoveredAt: *pgconv.PgTimestamptzToTimePtr(row.DiscoveredAt),
			TraceID: row.TraceID, IngestionMethod: row.IngestionMethod, Metadata: row.Metadata,
			CreatedAt: *pgconv.PgTimestamptzToTimePtr(row.CreatedAt),
		}}
	}
	return out, nil
}

func (r *PGPipelineRuntime) ListPipelineInputContents(ctx context.Context, rootBatchID uuid.UUID) ([]repo.PipelineInputContent, error) {
	rows, err := r.q.ListPipelineInputContents(ctx, rootBatchID)
	if err != nil {
		return nil, err
	}
	out := make([]repo.PipelineInputContent, len(rows))
	for i, row := range rows {
		out[i] = repo.PipelineInputContent{Content: repo.Content{
			ID: row.ContentID, BatchID: row.BatchID, Type: string(row.Type), SourceAbbr: row.SourceAbbr,
			CandidateID: pgconv.PgUUIDToUUID(row.CandidateID), URL: row.Url, Title: row.Title, Content: row.Content,
			Author: pgconv.PgTextToStringPtr(row.Author), TraceID: row.TraceID,
			PublishedAt: *pgconv.PgTimestamptzToTimePtr(row.PublishedAt), FetchedAt: *pgconv.PgTimestamptzToTimePtr(row.FetchedAt),
			CreatedAt: *pgconv.PgTimestamptzToTimePtr(row.CreatedAt), DeletedAt: pgconv.PgTimestamptzToTimePtr(row.DeletedAt), Metadata: row.Metadata,
		}}
	}
	return out, nil
}

func (r *PGPipelineRuntime) CreatePipelineRoot(ctx context.Context, arg repo.CreateTaskParams) (repo.Task, error) {
	beginner, ok := r.db.(pgBeginner)
	if !ok {
		return repo.Task{}, fmt.Errorf("postgres repository does not support transactions")
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return repo.Task{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ"); err != nil {
		return repo.Task{}, fmt.Errorf("set pipeline root transaction isolation: %w", err)
	}
	if _, err := tx.Exec(ctx, "SELECT 1"); err != nil {
		return repo.Task{}, fmt.Errorf("establish pipeline root snapshot: %w", err)
	}
	if r.afterPipelineSnapshot != nil {
		if err := r.afterPipelineSnapshot(ctx); err != nil {
			return repo.Task{}, err
		}
	}
	qtx := r.q.WithTx(tx)
	if arg.ParentBatchID == nil && len(arg.PipelineInputCandidateIDs) == 0 {
		return repo.Task{}, fmt.Errorf("pipeline root input batch or selected inputs is required")
	}
	if arg.ParentBatchID != nil {
		input, lockErr := qtx.LockBatchForTaskInsert(ctx, *arg.ParentBatchID)
		if lockErr != nil {
			if errors.Is(lockErr, pgx.ErrNoRows) {
				return repo.Task{}, repo.ErrPipelineInputNotFound
			}
			return repo.Task{}, fmt.Errorf("lock pipeline input batch %s: %w", *arg.ParentBatchID, lockErr)
		}
		if len(arg.PipelineInputCandidateIDs) == 0 {
			if !input.CompletedAt.Valid {
				return repo.Task{}, repo.ErrPipelineInputNotTerminal
			}
			if input.Succeeded.Valid && !input.Succeeded.Bool {
				return repo.Task{}, repo.ErrPipelineInputFailed
			}
		}
	}
	if r.afterPipelineInputLock != nil {
		if err := r.afterPipelineInputLock(ctx); err != nil {
			return repo.Task{}, err
		}
	}
	var existing Batch
	if arg.PipelineIdempotencyKey != nil {
		existing, err = qtx.GetPipelineRootByIdempotency(ctx, GetPipelineRootByIdempotencyParams{
			ParentID: pgconv.UUIDToPgUUID(*arg.ParentBatchID), IdempotencyKey: pgconv.StringPtrToPgText(arg.PipelineIdempotencyKey),
		})
		if err == nil {
			if strings.TrimSpace(existing.PipelineDefinitionHash.String) != arg.PipelineDefinitionHash || strings.TrimSpace(existing.PipelineRequestFingerprint.String) != arg.PipelineRequestFingerprint {
				return repo.Task{}, repo.ErrPipelineIdempotencyConflict
			}
			if !existing.PipelineInputSnapshotAt.Valid {
				return repo.Task{}, repo.ErrPipelineSnapshotMissing
			}
			row, taskErr := qtx.GetTaskByBatchLogicalKey(ctx, GetTaskByBatchLogicalKeyParams{BatchID: existing.ID, LogicalKey: pgconv.StringPtrToPgText(arg.LogicalKey)})
			if taskErr != nil {
				return repo.Task{}, fmt.Errorf("recover pipeline init task: %w", taskErr)
			}
			if err := tx.Commit(ctx); err != nil {
				return repo.Task{}, err
			}
			return dbTaskToRepoTask(row), repo.ErrTaskAlreadyActive
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return repo.Task{}, fmt.Errorf("lookup pipeline idempotency key: %w", err)
		}
	}
	if err := qtx.EnsurePipelineRoot(ctx, repoCreateTaskParamsToEnsurePipelineRoot(arg)); err != nil {
		var pgErr *pgconn.PgError
		if arg.PipelineIdempotencyKey != nil && errors.As(err, &pgErr) && pgErr.Code == "23505" {
			_ = tx.Rollback(ctx)
			return r.recoverPipelineRoot(ctx, arg)
		}
		return repo.Task{}, fmt.Errorf("ensure pipeline root batch: %w", err)
	}
	if len(arg.PipelineInputCandidateIDs) > 0 {
		if err := qtx.SnapshotPipelineCandidatesByIDs(ctx, SnapshotPipelineCandidatesByIDsParams{
			RootBatchID: arg.BatchID, CandidateIds: arg.PipelineInputCandidateIDs,
		}); err != nil {
			return repo.Task{}, fmt.Errorf("snapshot selected pipeline candidates: %w", err)
		}
	} else if err := qtx.SnapshotPipelineCandidates(ctx, SnapshotPipelineCandidatesParams{RootBatchID: arg.BatchID, InputBatchID: pgconv.UUIDToPgUUID(*arg.ParentBatchID)}); err != nil {
		return repo.Task{}, fmt.Errorf("snapshot pipeline candidates: %w", err)
	}
	if len(arg.PipelineInputContentIDs) > 0 {
		if err := qtx.SnapshotPipelineContentsByIDs(ctx, SnapshotPipelineContentsByIDsParams{
			RootBatchID: arg.BatchID, ContentIds: arg.PipelineInputContentIDs,
		}); err != nil {
			return repo.Task{}, fmt.Errorf("snapshot selected pipeline contents: %w", err)
		}
	} else if err := qtx.SnapshotPipelineContents(ctx, SnapshotPipelineContentsParams{RootBatchID: arg.BatchID, InputBatchID: pgconv.UUIDToPgUUID(*arg.ParentBatchID)}); err != nil {
		return repo.Task{}, fmt.Errorf("snapshot pipeline contents: %w", err)
	}
	if _, err := qtx.MarkPipelineInputSnapshot(ctx, arg.BatchID); err != nil {
		return repo.Task{}, fmt.Errorf("mark pipeline input snapshot: %w", err)
	}
	row, err := qtx.CreateTask(ctx, repoCreateTaskParamsToDB(arg))
	if err != nil {
		return repo.Task{}, fmt.Errorf("create pipeline init task: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return repo.Task{}, fmt.Errorf("commit pipeline root: %w", err)
	}
	return dbCreateTaskRowToRepoTask(row), nil
}

func (r *PGPipelineRuntime) recoverPipelineRoot(ctx context.Context, arg repo.CreateTaskParams) (repo.Task, error) {
	existing, err := r.q.GetPipelineRootByIdempotency(ctx, GetPipelineRootByIdempotencyParams{
		ParentID: pgconv.UUIDToPgUUID(*arg.ParentBatchID), IdempotencyKey: pgconv.StringPtrToPgText(arg.PipelineIdempotencyKey),
	})
	if err != nil {
		return repo.Task{}, fmt.Errorf("recover concurrent pipeline root: %w", err)
	}
	if strings.TrimSpace(existing.PipelineDefinitionHash.String) != arg.PipelineDefinitionHash || strings.TrimSpace(existing.PipelineRequestFingerprint.String) != arg.PipelineRequestFingerprint {
		return repo.Task{}, repo.ErrPipelineIdempotencyConflict
	}
	if !existing.PipelineInputSnapshotAt.Valid {
		return repo.Task{}, repo.ErrPipelineSnapshotMissing
	}
	row, err := r.q.GetTaskByBatchLogicalKey(ctx, GetTaskByBatchLogicalKeyParams{BatchID: existing.ID, LogicalKey: pgconv.StringPtrToPgText(arg.LogicalKey)})
	if err != nil {
		return repo.Task{}, fmt.Errorf("recover concurrent pipeline init task: %w", err)
	}
	return dbTaskToRepoTask(row), repo.ErrTaskAlreadyActive
}

func (r *PGPipelineRuntime) InitializePipelineStage(ctx context.Context, arg repo.InitializePipelineStageParams) (uuid.UUID, error) {
	beginner, ok := r.db.(pgBeginner)
	if !ok {
		return uuid.Nil, fmt.Errorf("postgres repository does not support transactions")
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := r.q.WithTx(tx)
	childBatchID, err := qtx.EnsurePipelineChildBatch(ctx, EnsurePipelineChildBatchParams{
		ID: arg.ChildBatchID, SourceType: SourceType(arg.SourceType), TraceID: pgconv.StringPtrToPgText(&arg.TraceID),
		ParentID: pgconv.UUIDToPgUUID(arg.ParentBatchID), ParentTaskID: pgconv.UUIDToPgUUID(arg.ParentTaskID),
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("ensure pipeline child batch: %w", err)
	}
	locked, err := qtx.LockBatchForTaskInsert(ctx, childBatchID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("lock pipeline child batch %s: %w", childBatchID, err)
	}
	if locked.CompletedAt.Valid {
		if err := tx.Commit(ctx); err != nil {
			return uuid.Nil, fmt.Errorf("commit completed pipeline stage recovery: %w", err)
		}
		return childBatchID, nil
	}
	for _, task := range arg.Tasks {
		task.BatchID = childBatchID
		_, err := qtx.CreateTask(ctx, repoCreateTaskParamsToDB(task))
		if err != nil {
			return uuid.Nil, fmt.Errorf("create pipeline stage task: %w", err)
		}
	}
	if _, err := qtx.SetBatchNSubtasks(ctx, SetBatchNSubtasksParams{
		BatchID: childBatchID, NSubtasks: pgconv.Int32PtrToPgInt4(&arg.NSubtasks),
	}); err != nil {
		return uuid.Nil, fmt.Errorf("set pipeline child subtasks: %w", err)
	}
	if arg.NSubtasks == 0 {
		if _, err := qtx.MarkPipelineBatchFinished(ctx, MarkPipelineBatchFinishedParams{
			BatchID: childBatchID, Succeeded: pgtype.Bool{Bool: true, Valid: true}, TraceID: arg.TraceID,
		}); err != nil {
			return uuid.Nil, fmt.Errorf("finish empty pipeline child batch: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, fmt.Errorf("commit pipeline stage initialization: %w", err)
	}
	return childBatchID, nil
}

func (r *PGRepository) Embedding() repo.Embeddings {
	return &PGEmbeddings{q: r.q}
}

func (r *PGRepository) Analysis() repo.Analysis {
	return &PGAnalysis{q: r.q}
}

func (r *PGRepository) BatchTrigger() repo.BatchTrigger {
	return &PGBatchTrigger{q: r.q}
}

func (r *PGRepository) UserFetches() repo.UserFetches {
	return &PGUserFetches{q: r.q}
}

func (r *PGRepository) AnalysisRuns() repo.AnalysisRuns {
	return &PGAnalysisRuns{q: r.q}
}

func (r *PGRepository) Schedules() repo.Schedules {
	return &PGSchedules{db: r.db, q: r.q}
}

func (r *PGRepository) Operator() repo.Operator {
	return &PGOperator{q: r.q}
}

func (r *PGRepository) Prompts() repo.Prompts {
	return &PGPrompts{q: r.q}
}

func (r *PGRepository) Tokens() repo.Tokens {
	return &PGTokens{q: r.q}
}

func (r *PGRepository) Sources() repo.Sources {
	return &PGSources{q: r.q}
}

func (r *PGRepository) Models() repo.Models {
	return &PGModels{q: r.q}
}

func (r *PGPipelineRuntime) FindFinishedBatches(ctx context.Context, limit int32) ([]repo.Batch, error) {
	rows, err := r.q.FindFinishedPipelineBatches(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]repo.Batch, len(rows))
	for i, row := range rows {
		out[i] = dbBatchToRepoBatch(
			row.ID,
			string(row.Purpose),
			pgconv.PgUUIDToUUIDPtr(row.ParentID),
			pgconv.PgInt4ToInt32Ptr(row.NSubtasks),
			pgconv.PgUUIDToUUIDPtr(row.ParentTaskID),
			&row.CompletionSucceeded,
			string(row.SourceType),
			pgconv.PgTextToStringPtr(row.TraceID),
			*pgconv.PgTimestamptzToTimePtr(row.CreatedAt),
			*pgconv.PgTimestamptzToTimePtr(row.UpdatedAt),
			pgconv.PgTimestamptzToTimePtr(row.CompletedAt),
			pgconv.PgTimestamptzToTimePtr(row.PublishedAt),
			pgconv.PgTimestamptzToTimePtr(row.LastPublishAttemptAt),
			row.PublishRetryCount,
			pgconv.PgTextToStringPtr(row.PublishError),
			pgconv.PgTimestamptzToTimePtr(row.StalledAt),
		)
	}
	return out, nil
}

func (r *PGPipelineRuntime) SetNSubtasks(ctx context.Context, batchID uuid.UUID, count int32) (repo.Batch, error) {
	row, err := r.q.SetBatchNSubtasks(ctx, SetBatchNSubtasksParams{BatchID: batchID, NSubtasks: pgconv.Int32PtrToPgInt4(&count)})
	if err != nil {
		return repo.Batch{}, err
	}
	return dbBatchToRepoBatch(
		row.ID,
		string(row.Purpose),
		pgconv.PgUUIDToUUIDPtr(row.ParentID),
		pgconv.PgInt4ToInt32Ptr(row.NSubtasks),
		pgconv.PgUUIDToUUIDPtr(row.ParentTaskID),
		pgconv.PgBoolToBoolPtr(row.Succeeded),
		string(row.SourceType),
		pgconv.PgTextToStringPtr(row.TraceID),
		*pgconv.PgTimestamptzToTimePtr(row.CreatedAt),
		*pgconv.PgTimestamptzToTimePtr(row.UpdatedAt),
		pgconv.PgTimestamptzToTimePtr(row.CompletedAt),
		pgconv.PgTimestamptzToTimePtr(row.PublishedAt),
		pgconv.PgTimestamptzToTimePtr(row.LastPublishAttemptAt),
		row.PublishRetryCount,
		pgconv.PgTextToStringPtr(row.PublishError),
		pgconv.PgTimestamptzToTimePtr(row.StalledAt),
	), nil
}

func (r *PGPipelineRuntime) FindFinishedRootBatches(ctx context.Context, limit int32) ([]repo.Batch, error) {
	rows, err := r.q.FindFinishedPipelineRootBatches(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]repo.Batch, len(rows))
	for i, row := range rows {
		out[i] = dbBatchToRepoBatch(
			row.ID, string(row.Purpose), pgconv.PgUUIDToUUIDPtr(row.ParentID), pgconv.PgInt4ToInt32Ptr(row.NSubtasks),
			pgconv.PgUUIDToUUIDPtr(row.ParentTaskID), &row.CompletionSucceeded, string(row.SourceType),
			pgconv.PgTextToStringPtr(row.TraceID), *pgconv.PgTimestamptzToTimePtr(row.CreatedAt),
			*pgconv.PgTimestamptzToTimePtr(row.UpdatedAt), pgconv.PgTimestamptzToTimePtr(row.CompletedAt),
			pgconv.PgTimestamptzToTimePtr(row.PublishedAt), pgconv.PgTimestamptzToTimePtr(row.LastPublishAttemptAt),
			row.PublishRetryCount, pgconv.PgTextToStringPtr(row.PublishError), pgconv.PgTimestamptzToTimePtr(row.StalledAt),
		)
	}
	return out, nil
}

func (r *PGPipelineRuntime) MarkBatchFinished(ctx context.Context, batchID uuid.UUID, succeeded bool, traceID string) (int64, error) {
	return r.q.MarkPipelineBatchFinished(ctx, MarkPipelineBatchFinishedParams{
		BatchID:   batchID,
		Succeeded: pgtype.Bool{Bool: succeeded, Valid: true},
		TraceID:   traceID,
	})
}

func (r *PGPipelineRuntime) MarkRootBatchFinished(ctx context.Context, batchID uuid.UUID, succeeded bool, traceID string) (int64, error) {
	return r.q.MarkPipelineRootFinished(ctx, MarkPipelineRootFinishedParams{
		BatchID: batchID, Succeeded: pgtype.Bool{Bool: succeeded, Valid: true}, TraceID: traceID,
	})
}

func (r *PGPipelineRuntime) FailPipelineTask(ctx context.Context, taskID, rootBatchID uuid.UUID, retryMax int, reason string) (bool, error) {
	beginner, ok := r.db.(pgBeginner)
	if !ok {
		return false, fmt.Errorf("postgres repository does not support transactions")
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := r.q.WithTx(tx)
	if err := qtx.FailTask(ctx, FailTaskParams{
		ID: taskID, RetryMax: int32(retryMax), FailureMessage: reason,
	}); err != nil {
		return false, fmt.Errorf("fail pipeline task: %w", err)
	}
	task, err := qtx.GetTaskByID(ctx, taskID)
	if err != nil {
		return false, fmt.Errorf("get failed pipeline task: %w", err)
	}
	terminal := task.Status == TaskStatusFAILED
	if terminal && task.Kind == TaskKindPIPELINEINIT {
		if err := qtx.SetPipelineRootFailure(ctx, rootBatchID); err != nil {
			return false, fmt.Errorf("set failed pipeline root: %w", err)
		}
	} else if terminal {
		if _, err := qtx.CancelPendingTasksByBatchID(ctx, CancelPendingTasksByBatchIDParams{
			BatchID: rootBatchID, FailureMessage: pgconv.StringPtrToPgText(&reason),
		}); err != nil {
			return false, fmt.Errorf("cancel downstream pipeline tasks: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit pipeline task failure: %w", err)
	}
	return terminal, nil
}

func (r *PGPipelineRuntime) ConvergePipelineFailure(ctx context.Context, taskID, rootBatchID uuid.UUID, reason string) error {
	beginner, ok := r.db.(pgBeginner)
	if !ok {
		return fmt.Errorf("postgres repository does not support transactions")
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := r.q.WithTx(tx)
	task, err := qtx.GetTaskByID(ctx, taskID)
	if err != nil {
		return err
	}
	if task.Status != TaskStatusFAILED {
		return tx.Commit(ctx)
	}
	if task.Kind == TaskKindPIPELINEINIT {
		if err := qtx.SetPipelineRootFailure(ctx, rootBatchID); err != nil {
			return fmt.Errorf("set failed pipeline root: %w", err)
		}
	} else if _, err := qtx.CancelPendingTasksByBatchID(ctx, CancelPendingTasksByBatchIDParams{
		BatchID: rootBatchID, FailureMessage: pgconv.StringPtrToPgText(&reason),
	}); err != nil {
		return fmt.Errorf("cancel downstream pipeline tasks: %w", err)
	}
	return tx.Commit(ctx)
}

func (r *PGPipelineRuntime) ListReadyPipelineBatches(ctx context.Context, limit int32) ([]repo.Batch, error) {
	rows, err := r.q.ListReadyPipelineBatches(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]repo.Batch, len(rows))
	for i, row := range rows {
		out[i] = dbBatchToRepoBatch(
			row.ID, string(row.Purpose), pgconv.PgUUIDToUUIDPtr(row.ParentID), pgconv.PgInt4ToInt32Ptr(row.NSubtasks),
			pgconv.PgUUIDToUUIDPtr(row.ParentTaskID), pgconv.PgBoolToBoolPtr(row.Succeeded),
			string(row.SourceType), pgconv.PgTextToStringPtr(row.TraceID),
			*pgconv.PgTimestamptzToTimePtr(row.CreatedAt), *pgconv.PgTimestamptzToTimePtr(row.UpdatedAt),
			pgconv.PgTimestamptzToTimePtr(row.CompletedAt), pgconv.PgTimestamptzToTimePtr(row.PublishedAt),
			pgconv.PgTimestamptzToTimePtr(row.LastPublishAttemptAt), row.PublishRetryCount,
			pgconv.PgTextToStringPtr(row.PublishError), pgconv.PgTimestamptzToTimePtr(row.StalledAt),
		)
	}
	return out, nil
}

func (r *PGPipelineRuntime) MarkPipelinePublished(ctx context.Context, batchID uuid.UUID) error {
	return r.q.MarkPipelinePublished(ctx, batchID)
}

func (r *PGPipelineRuntime) RecordPipelinePublishFailure(ctx context.Context, batchID uuid.UUID, message string) error {
	return r.q.RecordPipelinePublishFailure(ctx, RecordPipelinePublishFailureParams{
		ID: batchID, PipelinePublishError: pgconv.StringPtrToPgText(&message),
	})
}

func (r *PGPipelineRuntime) CompleteAnalysisReport(ctx context.Context, arg repo.CompleteAnalysisReportParams) (repo.Report, error) {
	beginner, ok := r.db.(pgBeginner)
	if !ok {
		return repo.Report{}, fmt.Errorf("postgres repository does not support transactions")
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return repo.Report{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := r.q.WithTx(tx)

	task, err := qtx.LockReportTask(ctx, arg.TaskID)
	if err != nil {
		return repo.Report{}, fmt.Errorf("lock report task: %w", err)
	}
	if task.BatchID != arg.RootBatchID || task.Kind != TaskKindPIPELINESTAGE || task.Url != "pipeline://stage/generate_report" {
		return repo.Report{}, repo.ErrReportTaskStale
	}

	if task.Status == TaskStatusCOMPLETED {
		existing, getErr := qtx.GetReportByExecutionID(ctx, arg.AnalysisExecutionID)
		if getErr != nil {
			return repo.Report{}, repo.ErrReportTaskStale
		}
		if !reportMatchesParams(existing, arg.Report) {
			return repo.Report{}, repo.ErrReportConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return repo.Report{}, fmt.Errorf("commit idempotent report completion: %w", err)
		}
		return dbReportToRepo(existing), nil
	}
	if task.Status != TaskStatusRUNNING {
		return repo.Report{}, repo.ErrReportTaskStale
	}

	if _, err := qtx.LockReportRoot(ctx, LockReportRootParams{
		ID:                  arg.RootBatchID,
		AnalysisExecutionID: pgconv.UUIDToPgUUID(arg.AnalysisExecutionID),
	}); err != nil {
		return repo.Report{}, repo.ErrReportTaskStale
	}
	if _, err := qtx.LockAnalysisExecution(ctx, arg.AnalysisExecutionID); err != nil {
		return repo.Report{}, fmt.Errorf("lock analysis execution: %w", err)
	}

	existing, getErr := qtx.LockReportByExecutionID(ctx, arg.AnalysisExecutionID)
	if errors.Is(getErr, pgx.ErrNoRows) {
		existing, err = qtx.InsertReport(ctx, InsertReportParams{
			AnalysisExecutionID: arg.Report.AnalysisExecutionID,
			RootBatchID:         arg.Report.RootBatchID,
			StorageUri:          arg.Report.StorageURI,
			ByteSize:            arg.Report.ByteSize,
			Sha256:              arg.Report.SHA256,
			ExpiresAt:           pgconv.TimePtrToPgTimestamptz(&arg.Report.ExpiresAt),
		})
		if err != nil {
			return repo.Report{}, fmt.Errorf("insert report link: %w", err)
		}
	} else if getErr != nil {
		return repo.Report{}, fmt.Errorf("lock report link: %w", getErr)
	}
	if !reportMatchesParams(existing, arg.Report) {
		return repo.Report{}, repo.ErrReportConflict
	}

	rows, err := qtx.CompleteReportTask(ctx, CompleteReportTaskParams{
		TaskID:              arg.TaskID,
		RootBatchID:         arg.RootBatchID,
		AnalysisExecutionID: pgconv.UUIDToPgUUID(arg.AnalysisExecutionID),
		StorageUri:          arg.Report.StorageURI,
		ByteSize:            arg.Report.ByteSize,
		Sha256:              arg.Report.SHA256,
	})
	if err != nil {
		return repo.Report{}, fmt.Errorf("complete report task: %w", err)
	}
	if rows != 1 {
		return repo.Report{}, repo.ErrReportTaskStale
	}
	if err := tx.Commit(ctx); err != nil {
		return repo.Report{}, fmt.Errorf("commit report completion: %w", err)
	}
	return dbReportToRepo(existing), nil
}

// Scheduler repository.
func (r *PGScheduler) ClaimTasks(ctx context.Context, limit int32, kinds []string, sourceTypes []string) ([]repo.Task, error) {
	pgKinds := make([]TaskKind, len(kinds))
	for i, k := range kinds {
		pgKinds[i] = TaskKind(k)
	}
	pgSourceTypes := make([]SourceType, len(sourceTypes))
	for i, s := range sourceTypes {
		pgSourceTypes[i] = SourceType(s)
	}
	rows, err := r.q.ClaimTasks(ctx, ClaimTasksParams{
		Kinds:       pgKinds,
		SourceTypes: pgSourceTypes,
		MaxTasks:    limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]repo.Task, len(rows))
	for i, row := range rows {
		out[i] = dbTaskToRepoTask(row)
	}
	return out, nil
}

func (r *PGScheduler) ReleaseTasks(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	return r.q.ReleaseTasks(ctx, ids)
}

func (r *PGScheduler) CompleteTask(ctx context.Context, id uuid.UUID) error {
	return r.q.CompleteTask(ctx, id)
}

func (r *PGScheduler) FailTask(ctx context.Context, id uuid.UUID, retryMax int, failureMessage string) error {
	return r.q.FailTask(ctx, FailTaskParams{ID: id, RetryMax: int32(retryMax), FailureMessage: failureMessage})
}

func (r *PGScheduler) ListRunnableTasks(ctx context.Context, limit int32) ([]repo.Task, error) {
	rows, err := r.q.ListRunnableTasks(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]repo.Task, len(rows))
	for i, row := range rows {
		out[i] = dbTaskToRepoTask(row)
	}
	return out, nil
}

// Scout repository.
func (r *PGScout) GetSourceByAbbr(ctx context.Context, abbr string) (repo.Source, error) {
	row, err := r.q.GetSourceByAbbr(ctx, abbr)
	if err != nil {
		return repo.Source{}, err
	}
	return dbSourceToRepoSource(row), nil
}

func (r *PGScout) ListSourcesByType(ctx context.Context, sourceType string) ([]repo.Source, error) {
	rows, err := r.q.ListSourcesByType(ctx, SourceType(sourceType))
	if err != nil {
		return nil, err
	}
	out := make([]repo.Source, len(rows))
	for i, row := range rows {
		out[i] = dbSourceToRepoSource(row)
	}
	return out, nil
}

func (r *PGScout) GetCandidateByID(ctx context.Context, id uuid.UUID) (repo.Candidate, error) {
	row, err := r.q.GetCandidateByID(ctx, id)
	if err != nil {
		return repo.Candidate{}, err
	}
	return dbCandidateToRepoCandidate(row), nil
}

func (r *PGScout) GetCandidatesByIDs(ctx context.Context, ids []uuid.UUID) ([]repo.Candidate, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := r.q.GetCandidatesByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]repo.Candidate, len(rows))
	for i, row := range rows {
		out[i] = dbCandidateToRepoCandidate(row)
	}
	return out, nil
}

func (r *PGScout) ListCandidates(ctx context.Context, arg repo.ListCandidatesParams) ([]repo.Candidate, error) {
	rows, err := r.q.ListCandidates(ctx, repoListCandidatesParamsToDB(arg))
	if err != nil {
		return nil, err
	}
	out := make([]repo.Candidate, len(rows))
	for i, row := range rows {
		out[i] = dbCandidateToRepoCandidate(row)
	}
	return out, nil
}

func (r *PGScout) GetCandidateByFingerprint(ctx context.Context, fingerprint string) (repo.Candidate, error) {
	row, err := r.q.GetCandidateByFingerprint(ctx, fingerprint)
	if err != nil {
		return repo.Candidate{}, err
	}
	return dbCandidateToRepoCandidate(row), nil
}

func (r *PGScout) CountCandidatesByBatchID(ctx context.Context, batchID uuid.UUID) (int64, error) {
	return r.q.CountCandidatesByBatchID(ctx, pgconv.UUIDToPgUUID(batchID))
}

func (r *PGScout) ListCandidatesByBatchID(ctx context.Context, batchID uuid.UUID) ([]repo.Candidate, error) {
	rows, err := r.q.ListCandidatesByBatchID(ctx, pgconv.UUIDToPgUUID(batchID))
	if err != nil {
		return nil, err
	}
	out := make([]repo.Candidate, len(rows))
	for i, row := range rows {
		out[i] = dbCandidateToRepoCandidate(row)
	}
	return out, nil
}

func (r *PGScout) CreateCandidate(ctx context.Context, arg repo.CreateCandidateParams) (repo.Candidate, error) {
	row, err := r.q.CreateCandidate(ctx, CreateCandidateParams{
		BatchID:         pgconv.UUIDToPgUUID(arg.BatchID),
		Fingerprint:     arg.Fingerprint,
		SourceAbbr:      arg.SourceAbbr,
		Title:           arg.Title,
		Url:             arg.URL,
		Description:     pgconv.StringPtrToPgText(arg.Description),
		PublishedAt:     pgconv.TimePtrToPgTimestamptz(arg.PublishedAt),
		TraceID:         arg.TraceID,
		IngestionMethod: CandidateIngestionMethod(arg.IngestionMethod),
		Metadata:        arg.Metadata,
	})
	if err != nil {
		return repo.Candidate{}, err
	}
	return dbCandidateToRepoCandidate(row), nil
}

func (r *PGScout) UpsertCandidate(ctx context.Context, arg repo.UpsertCandidateParams) (repo.Candidate, error) {
	row, err := r.q.UpsertCandidate(ctx, UpsertCandidateParams{
		BatchID:         pgconv.UUIDToPgUUID(arg.BatchID),
		Fingerprint:     arg.Fingerprint,
		SourceAbbr:      arg.SourceAbbr,
		Title:           arg.Title,
		Url:             arg.URL,
		Description:     pgconv.StringPtrToPgText(arg.Description),
		PublishedAt:     pgconv.TimePtrToPgTimestamptz(arg.PublishedAt),
		TraceID:         arg.TraceID,
		IngestionMethod: CandidateIngestionMethod(arg.IngestionMethod),
		Metadata:        arg.Metadata,
	})
	if err != nil {
		return repo.Candidate{}, err
	}
	return dbCandidateToRepoCandidate(row), nil
}

// Tasks repository.
func (r *PGTasks) GetTaskByID(ctx context.Context, id uuid.UUID) (repo.Task, error) {
	row, err := r.q.GetTaskByID(ctx, id)
	if err != nil {
		return repo.Task{}, err
	}
	return dbTaskToRepoTask(row), nil
}

func (r *PGTasks) IsTaskRunning(ctx context.Context, id uuid.UUID) (bool, error) {
	isRunning, err := r.q.IsTaskRunning(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return isRunning, err
}

func (r *PGTasks) ListTasksByBatchID(ctx context.Context, batchID uuid.UUID) ([]repo.Task, error) {
	rows, err := r.q.ListTasksByBatchID(ctx, batchID)
	if err != nil {
		return nil, err
	}
	out := make([]repo.Task, len(rows))
	for i, row := range rows {
		out[i] = dbTaskToRepoTask(row)
	}
	return out, nil
}

func (r *PGTasks) ListTaskStatusSummary(ctx context.Context) ([]repo.TaskStatusSummary, error) {
	rows, err := r.q.ListTaskStatusSummary(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]repo.TaskStatusSummary, len(rows))
	for i, row := range rows {
		out[i] = repo.TaskStatusSummary{Kind: string(row.Kind), Status: repo.TaskStatus(row.Status), Count: row.Count}
	}
	return out, nil
}

func (r *PGTasks) ListRecentFailedTasks(ctx context.Context, limit int32) ([]repo.FailedTaskSummary, error) {
	rows, err := r.q.ListRecentFailedTasks(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]repo.FailedTaskSummary, len(rows))
	for i, row := range rows {
		var failure *string
		if row.FailureMessage.Valid {
			value := row.FailureMessage.String
			failure = &value
		}
		out[i] = repo.FailedTaskSummary{
			ID: row.ID, Kind: string(row.Kind), SourceAbbr: row.SourceAbbr,
			URL: row.Url, FailureMessage: failure, UpdatedAt: row.UpdatedAt.Time,
		}
	}
	return out, nil
}

func (r *PGTasks) RetryFailedTask(ctx context.Context, id uuid.UUID) (repo.Task, error) {
	row, err := r.q.RetryFailedTask(ctx, id)
	if err != nil {
		return repo.Task{}, err
	}
	if !row.Retried {
		return repo.Task{}, repo.ErrTaskNotFailed
	}
	return dbRetryFailedTaskRowToRepoTask(row), nil
}

func (r *PGTasks) CreateTask(ctx context.Context, arg repo.CreateTaskParams) (repo.Task, error) {
	return createTaskRepo(ctx, r.db, r.q, arg)
}

func (r *PGTasks) EnsureBatch(ctx context.Context, arg repo.EnsureBatchParams) error {
	return r.q.EnsureBatchExists(ctx, EnsureBatchExistsParams{
		ID: arg.BatchID, ParentID: pgconv.UUIDPtrToPgUUID(arg.ParentBatchID),
		ParentTaskID: pgconv.UUIDPtrToPgUUID(arg.ParentTaskID), SourceType: SourceType(arg.SourceType),
		TraceID: pgconv.StringPtrToPgText(&arg.TraceID),
	})
}

func createTaskRepo(ctx context.Context, db DBTX, q *Queries, arg repo.CreateTaskParams) (repo.Task, error) {
	beginner, ok := db.(pgBeginner)
	if !ok {
		return repo.Task{}, fmt.Errorf("postgres repository does not support transactions")
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return repo.Task{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := q.WithTx(tx)
	if err := qtx.EnsureBatchExists(ctx, repoCreateTaskParamsToEnsureBatchExists(arg)); err != nil {
		return repo.Task{}, fmt.Errorf("ensure batch %s exists: %w", arg.BatchID, err)
	}
	locked, err := qtx.LockBatchForTaskInsert(ctx, arg.BatchID)
	if err != nil {
		return repo.Task{}, fmt.Errorf("lock batch %s: %w", arg.BatchID, err)
	}
	if locked.CompletedAt.Valid {
		return repo.Task{}, fmt.Errorf("batch %s is already finished", arg.BatchID)
	}
	count, err := qtx.CountTasksByBatchID(ctx, arg.BatchID)
	if err != nil {
		return repo.Task{}, fmt.Errorf("count batch %s tasks: %w", arg.BatchID, err)
	}
	if locked.NSubtasks.Valid && count >= int64(locked.NSubtasks.Int32) {
		if arg.LogicalKey != nil {
			if existing, lookupErr := qtx.GetTaskByBatchLogicalKey(ctx, GetTaskByBatchLogicalKeyParams{BatchID: arg.BatchID, LogicalKey: pgconv.StringPtrToPgText(arg.LogicalKey)}); lookupErr == nil {
				return dbTaskToRepoTask(existing), repo.ErrTaskAlreadyActive
			}
		}
		return repo.Task{}, fmt.Errorf("batch %s has reached n_subtasks=%d", arg.BatchID, locked.NSubtasks.Int32)
	}

	row, err := qtx.CreateTask(ctx, repoCreateTaskParamsToDB(arg))
	if err != nil {
		// Zero rows = conflict at insert AND no PENDING/RUNNING row by SELECT
		// time. Race window where the colliding task transitioned to terminal
		// between ON CONFLICT and the recovery SELECT. Surface as
		// ErrTaskAlreadyActive with a zero task; PAGE_FETCH callers detect
		// the empty ID and fall back to a contents lookup.
		if errors.Is(err, pgx.ErrNoRows) {
			return repo.Task{}, repo.ErrTaskAlreadyActive
		}
		return repo.Task{}, err
	}
	task := dbCreateTaskRowToRepoTask(row)
	if !row.Inserted {
		if arg.LogicalKey != nil {
			if existing, lookupErr := qtx.GetTaskByBatchLogicalKey(ctx, GetTaskByBatchLogicalKeyParams{BatchID: arg.BatchID, LogicalKey: pgconv.StringPtrToPgText(arg.LogicalKey)}); lookupErr == nil {
				if err := tx.Commit(ctx); err != nil {
					return repo.Task{}, err
				}
				return dbTaskToRepoTask(existing), repo.ErrTaskAlreadyActive
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return repo.Task{}, err
	}
	if !row.Inserted {
		return task, repo.ErrTaskAlreadyActive
	}
	return task, nil
}

func (r *PGTasks) ExtendActiveTaskExpiry(ctx context.Context, arg repo.ExtendActiveTaskExpiryParams) error {
	return r.q.ExtendActiveTaskExpiry(ctx, repoExtendActiveTaskExpiryParamsToDB(arg))
}

func (r *PGTasks) CancelPendingTasksByBatchID(ctx context.Context, batchID uuid.UUID, reason string) (int64, error) {
	return r.q.CancelPendingTasksByBatchID(ctx, CancelPendingTasksByBatchIDParams{
		BatchID: batchID, FailureMessage: pgconv.StringPtrToPgText(&reason),
	})
}

// createTaskRepoInTx is used by callers that already own a larger transaction.
func createTaskRepoInTx(ctx context.Context, q *Queries, arg repo.CreateTaskParams) (repo.Task, error) {
	if err := q.EnsureBatchExists(ctx, repoCreateTaskParamsToEnsureBatchExists(arg)); err != nil {
		return repo.Task{}, fmt.Errorf("ensure batch %s exists: %w", arg.BatchID, err)
	}
	row, err := q.CreateTask(ctx, repoCreateTaskParamsToDB(arg))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return repo.Task{}, repo.ErrTaskAlreadyActive
		}
		return repo.Task{}, err
	}
	task := dbCreateTaskRowToRepoTask(row)
	if !row.Inserted {
		return task, repo.ErrTaskAlreadyActive
	}
	return task, nil
}

// Schedules repository.
func (r *PGSchedules) SyncSchedules(ctx context.Context, schedules []repo.UpsertScheduleParams) ([]repo.Schedule, error) {
	beginner, ok := r.db.(pgBeginner)
	if !ok {
		return nil, fmt.Errorf("postgres repository does not support transactions")
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := r.q.WithTx(tx)
	if err := qtx.MarkSchedulesConfigAbsent(ctx); err != nil {
		return nil, err
	}
	out := make([]repo.Schedule, len(schedules))
	for i, schedule := range schedules {
		row, err := qtx.UpsertSchedule(ctx, repoUpsertScheduleParamsToDB(schedule))
		if err != nil {
			return nil, err
		}
		out[i] = dbScheduleToRepoSchedule(row)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *PGSchedules) UpsertSchedule(ctx context.Context, arg repo.UpsertScheduleParams) (repo.Schedule, error) {
	row, err := r.q.UpsertSchedule(ctx, repoUpsertScheduleParamsToDB(arg))
	if err != nil {
		return repo.Schedule{}, err
	}
	return dbScheduleToRepoSchedule(row), nil
}

func (r *PGSchedules) ListSchedules(ctx context.Context, limit int32) ([]repo.Schedule, error) {
	rows, err := r.q.ListSchedules(ctx, ListSchedulesParams{Lim: limit})
	if err != nil {
		return nil, err
	}
	out := make([]repo.Schedule, len(rows))
	for i, row := range rows {
		out[i] = dbScheduleToRepoSchedule(row)
	}
	return out, nil
}

func (r *PGSchedules) MaterializeDueSchedules(ctx context.Context, arg repo.MaterializeDueSchedulesParams) ([]repo.ScheduleMaterialization, error) {
	beginner, ok := r.db.(pgBeginner)
	if !ok {
		return nil, fmt.Errorf("postgres repository does not support transactions")
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := r.q.WithTx(tx)
	rows, err := qtx.ClaimDueSchedules(ctx, arg.Limit)
	if err != nil {
		return nil, err
	}
	out := make([]repo.ScheduleMaterialization, 0, len(rows))
	for _, row := range rows {
		schedule := dbScheduleToRepoSchedule(row)
		payloadHash := schedulePayloadHash(schedule.ID, schedule.ConfigHash)
		task, taskErr := qtx.GetActiveTaskByPayloadDedup(ctx, GetActiveTaskByPayloadDedupParams{
			SourceAbbr:  schedule.SourceAbbr,
			Kind:        TaskKind(schedule.Kind),
			PayloadHash: pgconv.StringPtrToPgText(&payloadHash),
		})
		active := false
		var materializedTask repo.Task
		if taskErr == nil {
			active = true
			materializedTask = dbTaskToRepoTask(task)
		} else if errors.Is(taskErr, pgx.ErrNoRows) {
			batchID, err := uuid.NewV7()
			if err != nil {
				return nil, err
			}
			materializedTask, taskErr = createTaskRepoInTx(ctx, qtx, repo.CreateTaskParams{
				BatchID:     batchID,
				Kind:        schedule.Kind,
				SourceType:  schedule.SourceType,
				SourceAbbr:  schedule.SourceAbbr,
				URL:         schedule.URL,
				Payload:     schedule.Payload,
				PayloadHash: &payloadHash,
				Meta:        schedule.Meta,
				TraceID:     fmt.Sprintf("%s-%s", arg.TraceIDPrefix, schedule.ID.String()),
			})
			if errors.Is(taskErr, repo.ErrTaskAlreadyActive) {
				active = true
			}
		}
		if taskErr != nil {
			if !errors.Is(taskErr, repo.ErrTaskAlreadyActive) || materializedTask.ID == uuid.Nil {
				markErr := qtx.MarkScheduleError(ctx, MarkScheduleErrorParams{
					LastError: pgconv.StringPtrToPgText(errorStringPtr(taskErr)),
					ID:        schedule.ID,
				})
				if markErr != nil {
					return nil, fmt.Errorf("mark schedule %s error after task failure %w: %w", schedule.ID, taskErr, markErr)
				}
				continue
			}
		}
		if err := qtx.MarkScheduleMaterialized(ctx, MarkScheduleMaterializedParams{
			TaskID: pgconv.UUIDToPgUUID(materializedTask.ID),
			ID:     schedule.ID,
		}); err != nil {
			return nil, err
		}
		out = append(out, repo.ScheduleMaterialization{Schedule: schedule, Task: materializedTask, Active: active})
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

// Operator repository.
func (r *PGOperator) ListModels(ctx context.Context, params repo.ListOperatorParams) ([]repo.Model, error) {
	rows, err := r.q.ListModels(ctx, ListModelsParams{Lim: params.Limit, Off: operatorOffset(params)})
	if err != nil {
		return nil, err
	}
	out := make([]repo.Model, len(rows))
	for i, row := range rows {
		out[i] = dbModelToRepoModel(row)
	}
	return out, nil
}

func (r *PGOperator) ListSources(ctx context.Context, params repo.ListOperatorParams) ([]repo.Source, error) {
	rows, err := r.q.ListSources(ctx, ListSourcesParams{Lim: params.Limit, Off: operatorOffset(params)})
	if err != nil {
		return nil, err
	}
	out := make([]repo.Source, len(rows))
	for i, row := range rows {
		out[i] = dbSourceToRepoSource(row)
	}
	return out, nil
}

func (r *PGOperator) ListBatches(ctx context.Context, params repo.ListOperatorParams) ([]repo.Batch, error) {
	rows, err := r.q.ListBatches(ctx, ListBatchesParams{Lim: params.Limit, Off: operatorOffset(params)})
	if err != nil {
		return nil, err
	}
	out := make([]repo.Batch, len(rows))
	for i, row := range rows {
		out[i] = dbBatchToRepoBatch(
			row.ID,
			string(row.Purpose),
			pgconv.PgUUIDToUUIDPtr(row.ParentID),
			pgconv.PgInt4ToInt32Ptr(row.NSubtasks),
			pgconv.PgUUIDToUUIDPtr(row.ParentTaskID),
			pgconv.PgBoolToBoolPtr(row.Succeeded),
			string(row.SourceType),
			pgconv.PgTextToStringPtr(row.TraceID),
			*pgconv.PgTimestamptzToTimePtr(row.CreatedAt),
			*pgconv.PgTimestamptzToTimePtr(row.UpdatedAt),
			pgconv.PgTimestamptzToTimePtr(row.CompletedAt),
			pgconv.PgTimestamptzToTimePtr(row.PublishedAt),
			pgconv.PgTimestamptzToTimePtr(row.LastPublishAttemptAt),
			row.PublishRetryCount,
			pgconv.PgTextToStringPtr(row.PublishError),
			pgconv.PgTimestamptzToTimePtr(row.StalledAt),
		)
	}
	return out, nil
}

func (r *PGOperator) ListEntities(ctx context.Context, params repo.ListOperatorParams) ([]repo.Entity, error) {
	rows, err := r.q.ListEntities(ctx, ListEntitiesParams{Lim: params.Limit, Off: operatorOffset(params)})
	if err != nil {
		return nil, err
	}
	out := make([]repo.Entity, len(rows))
	for i, row := range rows {
		out[i] = dbEntityToRepoEntity(row)
	}
	return out, nil
}

func (r *PGOperator) ListSchedules(ctx context.Context, params repo.ListOperatorParams) ([]repo.Schedule, error) {
	rows, err := r.q.ListSchedules(ctx, ListSchedulesParams{Lim: params.Limit, Off: operatorOffset(params)})
	if err != nil {
		return nil, err
	}
	out := make([]repo.Schedule, len(rows))
	for i, row := range rows {
		out[i] = dbScheduleToRepoSchedule(row)
	}
	return out, nil
}

func (r *PGOperator) ListCandidateEmbeddingsGemma2025(ctx context.Context, params repo.ListOperatorParams) ([]repo.EmbeddingRecord, error) {
	rows, err := r.q.ListCandidateEmbeddingsGemma2025(ctx, ListCandidateEmbeddingsGemma2025Params{Lim: params.Limit, Off: operatorOffset(params)})
	if err != nil {
		return nil, err
	}
	out := make([]repo.EmbeddingRecord, len(rows))
	for i, row := range rows {
		out[i] = dbCandidateEmbeddingRowToRepoEmbeddingRecord(row)
	}
	return out, nil
}

func (r *PGOperator) ListContentEmbeddingsGemma2025(ctx context.Context, params repo.ListOperatorParams) ([]repo.EmbeddingRecord, error) {
	rows, err := r.q.ListContentEmbeddingsGemma2025(ctx, ListContentEmbeddingsGemma2025Params{Lim: params.Limit, Off: operatorOffset(params)})
	if err != nil {
		return nil, err
	}
	out := make([]repo.EmbeddingRecord, len(rows))
	for i, row := range rows {
		out[i] = dbContentEmbeddingRowToRepoEmbeddingRecord(row)
	}
	return out, nil
}

// Prompts repository.
func (r *PGPrompts) CreatePromptVersion(ctx context.Context, arg repo.CreatePromptVersionParams) (repo.PromptVersion, error) {
	row, err := r.q.CreatePromptVersion(ctx, CreatePromptVersionParams{
		Name:      arg.Name,
		Hash:      arg.Hash,
		SizeBytes: arg.SizeBytes,
	})
	if err != nil {
		return repo.PromptVersion{}, err
	}
	return dbPromptVersionRowToRepoPromptVersion(row.ID, row.Key, row.Version, row.Hash, row.SizeBytes, row.CreatedAt), nil
}

func (r *PGPrompts) GetPromptVersionByID(ctx context.Context, id uuid.UUID) (repo.PromptVersion, error) {
	row, err := r.q.GetPromptVersionByID(ctx, id)
	if err != nil {
		return repo.PromptVersion{}, err
	}
	return dbPromptVersionRowToRepoPromptVersion(row.ID, row.Key, row.Version, row.Hash, row.SizeBytes, row.CreatedAt), nil
}

func (r *PGPrompts) GetPromptVersionByNameAndVersion(ctx context.Context, name string, version int32) (repo.PromptVersion, error) {
	row, err := r.q.GetPromptVersionByNameAndVersion(ctx, GetPromptVersionByNameAndVersionParams{
		Name:    name,
		Version: version,
	})
	if err != nil {
		return repo.PromptVersion{}, err
	}
	return dbPromptVersionRowToRepoPromptVersion(row.ID, row.Key, row.Version, row.Hash, row.SizeBytes, row.CreatedAt), nil
}

func (r *PGPrompts) GetLatestPromptVersionByName(ctx context.Context, name string) (repo.PromptVersion, error) {
	row, err := r.q.GetLatestPromptVersionByName(ctx, name)
	if err != nil {
		return repo.PromptVersion{}, err
	}
	return dbPromptVersionRowToRepoPromptVersion(row.ID, row.Key, row.Version, row.Hash, row.SizeBytes, row.CreatedAt), nil
}

func (r *PGPrompts) ListPromptVersions(ctx context.Context, params repo.ListOperatorParams) ([]repo.PromptVersion, error) {
	rows, err := r.q.ListPromptVersions(ctx, ListPromptVersionsParams{Lim: params.Limit, Off: operatorOffset(params)})
	if err != nil {
		return nil, err
	}
	out := make([]repo.PromptVersion, len(rows))
	for i, row := range rows {
		out[i] = dbPromptVersionRowToRepoPromptVersion(row.ID, row.Key, row.Version, row.Hash, row.SizeBytes, row.CreatedAt)
	}
	return out, nil
}

func (r *PGPrompts) ListPromptVersionsByKey(ctx context.Context, key string, params repo.ListOperatorParams) ([]repo.PromptVersion, error) {
	rows, err := r.q.ListPromptVersionsByKey(ctx, ListPromptVersionsByKeyParams{Key: key, Lim: params.Limit, Off: operatorOffset(params)})
	if err != nil {
		return nil, err
	}
	out := make([]repo.PromptVersion, len(rows))
	for i, row := range rows {
		out[i] = dbPromptVersionRowToRepoPromptVersion(row.ID, row.Key, row.Version, row.Hash, row.SizeBytes, row.CreatedAt)
	}
	return out, nil
}

// Tokens repository.
func (r *PGTokens) CreateToken(ctx context.Context, arg repo.CreateTokenParams) (repo.Token, error) {
	row, err := r.q.CreateToken(ctx, CreateTokenParams{
		ID:            arg.ID,
		Type:          arg.Type,
		Name:          arg.Name,
		Permissions:   int16(arg.Permissions),
		HashAlgorithm: arg.HashAlgorithm,
		TokenHash:     arg.TokenHash,
		ExpiresAt:     pgtype.Timestamptz{Time: arg.ExpiresAt, Valid: true},
	})
	if err != nil {
		return repo.Token{}, err
	}
	return dbTokenToRepoToken(row)
}

func (r *PGTokens) GetTokenByID(ctx context.Context, id uuid.UUID) (repo.Token, error) {
	row, err := r.q.GetTokenByID(ctx, id)
	if err != nil {
		return repo.Token{}, err
	}
	return dbTokenToRepoToken(row)
}

func (r *PGTokens) GetRootToken(ctx context.Context) (repo.Token, error) {
	row, err := r.q.GetRootToken(ctx)
	if err != nil {
		return repo.Token{}, err
	}
	return dbTokenToRepoToken(row)
}

func (r *PGTokens) ListTokens(ctx context.Context, params repo.ListOperatorParams) ([]repo.Token, error) {
	rows, err := r.q.ListTokens(ctx, ListTokensParams{Lim: params.Limit, Off: operatorOffset(params)})
	if err != nil {
		return nil, err
	}
	out := make([]repo.Token, len(rows))
	for i, row := range rows {
		out[i], err = dbTokenToRepoToken(row)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r *PGTokens) RenewToken(ctx context.Context, id uuid.UUID, expiresAt time.Time) (repo.Token, error) {
	row, err := r.q.RenewToken(ctx, RenewTokenParams{ID: id, ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true}})
	if err != nil {
		return repo.Token{}, err
	}
	return dbTokenToRepoToken(row)
}

func (r *PGTokens) RotateToken(ctx context.Context, arg repo.RotateTokenParams) (repo.Token, error) {
	row, err := r.q.RotateToken(ctx, RotateTokenParams{
		ID:            arg.ID,
		HashAlgorithm: arg.HashAlgorithm,
		TokenHash:     arg.TokenHash,
		ExpiresAt:     pgtype.Timestamptz{Time: arg.ExpiresAt, Valid: true},
	})
	if err != nil {
		return repo.Token{}, err
	}
	return dbTokenToRepoToken(row)
}

func (r *PGTokens) RevokeToken(ctx context.Context, id uuid.UUID) (repo.Token, error) {
	row, err := r.q.RevokeToken(ctx, id)
	if err != nil {
		return repo.Token{}, err
	}
	return dbTokenToRepoToken(row)
}

func (r *PGTokens) RevokeAllTokens(ctx context.Context) (int64, error) {
	return r.q.RevokeAllTokens(ctx)
}

func (r *PGTokens) CountActiveAdminTokensExcluding(ctx context.Context, id uuid.UUID) (int64, error) {
	return r.q.CountActiveAdminTokensExcluding(ctx, id)
}

func (r *PGSources) Create(ctx context.Context, arg repo.CreateSourceParams) (repo.Source, error) {
	row, err := r.q.CreateSource(ctx, CreateSourceParams{Abbr: arg.Abbr, Name: arg.Name, Type: SourceType(arg.Type), BaseUrl: arg.BaseURL})
	if err != nil {
		return repo.Source{}, err
	}
	return dbSourceToRepoSource(row), nil
}

func (r *PGSources) Update(ctx context.Context, arg repo.UpdateSourceParams) (repo.Source, error) {
	row, err := r.q.UpdateSource(ctx, UpdateSourceParams{Abbr: arg.Abbr, Name: arg.Name, Type: SourceType(arg.Type), BaseUrl: arg.BaseURL})
	if err != nil {
		return repo.Source{}, err
	}
	return dbSourceToRepoSource(row), nil
}

func (r *PGSources) Delete(ctx context.Context, abbr string) (repo.Source, error) {
	row, err := r.q.DeleteSource(ctx, abbr)
	if err != nil {
		return repo.Source{}, err
	}
	return dbSourceToRepoSource(row), nil
}

func (r *PGSources) Restore(ctx context.Context, abbr string) (repo.Source, error) {
	row, err := r.q.RestoreSource(ctx, abbr)
	if err != nil {
		return repo.Source{}, err
	}
	return dbSourceToRepoSource(row), nil
}

func operatorOffset(params repo.ListOperatorParams) int32 {
	if params.Next <= 1 {
		return 0
	}
	return params.Next - 1
}

func schedulePayloadHash(id uuid.UUID, configHash string) string {
	sum := sha256.Sum256([]byte(id.String() + ":" + configHash))
	return fmt.Sprintf("%x", sum[:])
}

func errorStringPtr(err error) *string {
	if err == nil {
		return nil
	}
	s := err.Error()
	return &s
}

// Pipeline repository.
func (r *PGPipeline) GetContentByID(ctx context.Context, id uuid.UUID) (repo.Content, error) {
	row, err := r.q.GetContentByID(ctx, id)
	if err != nil {
		return repo.Content{}, err
	}
	return dbContentToRepoContent(row), nil
}

func (r *PGPipeline) GetContentByURL(ctx context.Context, rawURL string) (repo.Content, error) {
	row, err := r.q.GetContentByURL(ctx, rawURL)
	if err != nil {
		return repo.Content{}, err
	}
	return dbContentToRepoContent(row), nil
}

func (r *PGPipeline) GetContentByCandidateID(ctx context.Context, candidateID uuid.UUID) (repo.Content, error) {
	row, err := r.q.GetContentByCandidateID(ctx, pgtype.UUID{Bytes: candidateID, Valid: true})
	if err != nil {
		return repo.Content{}, err
	}
	return dbContentToRepoContent(row), nil
}

func (r *PGPipeline) CreateContent(ctx context.Context, arg repo.CreateContentParams) (repo.Content, error) {
	row, err := r.q.CreateContent(ctx, repoCreateContentParamsToDB(arg))
	if err != nil {
		return repo.Content{}, err
	}
	return dbContentToRepoContent(row), nil
}

func (r *PGPipeline) UpdateContentMetadata(ctx context.Context, arg repo.UpdateContentMetadataParams) (repo.Content, error) {
	row, err := r.q.UpdateContentMetadata(ctx, repoUpdateContentMetadataParamsToDB(arg))
	if err != nil {
		return repo.Content{}, err
	}
	return dbContentToRepoContent(row), nil
}

func (r *PGPipeline) ListContentsByBatchID(ctx context.Context, batchID uuid.UUID) ([]repo.Content, error) {
	rows, err := r.q.ListContentsByBatchID(ctx, pgtype.UUID{Bytes: batchID, Valid: true})
	if err != nil {
		return nil, err
	}
	out := make([]repo.Content, len(rows))
	for i, row := range rows {
		out[i] = dbContentToRepoContent(row)
	}
	return out, nil
}

func (r *PGPipeline) ListRecentSeedContents(ctx context.Context, limit int32) ([]repo.Content, error) {
	rows, err := r.q.ListRecentSeedContents(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]repo.Content, len(rows))
	for i, row := range rows {
		out[i] = dbContentToRepoContent(row)
	}
	return out, nil
}

func (r *PGPipeline) DeleteContent(ctx context.Context, id uuid.UUID) (repo.Content, error) {
	beginner, ok := r.db.(pgBeginner)
	if !ok {
		return repo.Content{}, fmt.Errorf("postgres repository does not support transactions")
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return repo.Content{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := r.q.WithTx(tx)
	row, err := qtx.SoftDeleteContent(ctx, id)
	if err != nil {
		return repo.Content{}, err
	}
	if err := qtx.SoftDeleteContentEmbeddings(ctx, id); err != nil {
		return repo.Content{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return repo.Content{}, err
	}
	return dbContentToRepoContent(row), nil
}

func (r *PGPipeline) RestoreContent(ctx context.Context, id uuid.UUID) (repo.Content, error) {
	beginner, ok := r.db.(pgBeginner)
	if !ok {
		return repo.Content{}, fmt.Errorf("postgres repository does not support transactions")
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return repo.Content{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := r.q.WithTx(tx)
	row, err := qtx.RestoreContent(ctx, id)
	if err != nil {
		return repo.Content{}, err
	}
	if err := qtx.RestoreContentEmbeddings(ctx, id); err != nil {
		return repo.Content{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return repo.Content{}, err
	}
	return dbContentToRepoContent(row), nil
}

// Batch Trigger repository.
func (r *PGBatchTrigger) ListPendingCompletionBatches(ctx context.Context, limit int32) ([]repo.Batch, error) {
	rows, err := r.q.ListPendingCompletionBatches(ctx, limit)
	if err != nil {
		return nil, err
	}

	out := make([]repo.Batch, len(rows))
	for i, row := range rows {
		out[i] = dbBatchToRepoBatch(
			row.ID,
			string(row.Purpose),
			pgconv.PgUUIDToUUIDPtr(row.ParentID),
			pgconv.PgInt4ToInt32Ptr(row.NSubtasks),
			pgconv.PgUUIDToUUIDPtr(row.ParentTaskID),
			pgconv.PgBoolToBoolPtr(row.Succeeded),
			string(row.SourceType),
			pgconv.PgTextToStringPtr(row.TraceID),
			*pgconv.PgTimestamptzToTimePtr(row.CreatedAt),
			*pgconv.PgTimestamptzToTimePtr(row.UpdatedAt),
			pgconv.PgTimestamptzToTimePtr(row.CompletedAt),
			pgconv.PgTimestamptzToTimePtr(row.PublishedAt),
			pgconv.PgTimestamptzToTimePtr(row.LastPublishAttemptAt),
			row.PublishRetryCount,
			pgconv.PgTextToStringPtr(row.PublishError),
			pgconv.PgTimestamptzToTimePtr(row.StalledAt),
		)
	}
	return out, nil
}

func (r *PGBatchTrigger) FindNewlyCompletedBatches(ctx context.Context, limit int32) ([]repo.Batch, error) {
	rows, err := r.q.FindNewlyCompletedBatches(ctx, limit)
	if err != nil {
		return nil, err
	}

	out := make([]repo.Batch, len(rows))
	for i, row := range rows {
		out[i] = repo.Batch{
			ID:         row.ID,
			SourceType: string(row.SourceType),
			TraceID:    pgconv.PgTextToStringPtr(row.TraceID),
		}
	}
	return out, nil
}

func (r *PGBatchTrigger) MarkBatchCompleted(ctx context.Context, batchID uuid.UUID, traceID string) (int64, error) {
	return r.q.MarkBatchCompleted(ctx, MarkBatchCompletedParams{
		ID:      batchID,
		TraceID: traceID,
	})
}

func (r *PGBatchTrigger) ListReadyToPublishBatches(ctx context.Context, limit int32) ([]repo.Batch, error) {
	rows, err := r.q.ListReadyToPublishBatches(ctx, limit)
	if err != nil {
		return nil, err
	}

	out := make([]repo.Batch, len(rows))
	for i, row := range rows {
		out[i] = dbBatchToRepoBatch(
			row.ID,
			string(row.Purpose),
			pgconv.PgUUIDToUUIDPtr(row.ParentID),
			pgconv.PgInt4ToInt32Ptr(row.NSubtasks),
			pgconv.PgUUIDToUUIDPtr(row.ParentTaskID),
			pgconv.PgBoolToBoolPtr(row.Succeeded),
			string(row.SourceType),
			pgconv.PgTextToStringPtr(row.TraceID),
			*pgconv.PgTimestamptzToTimePtr(row.CreatedAt),
			*pgconv.PgTimestamptzToTimePtr(row.UpdatedAt),
			pgconv.PgTimestamptzToTimePtr(row.CompletedAt),
			pgconv.PgTimestamptzToTimePtr(row.PublishedAt),
			pgconv.PgTimestamptzToTimePtr(row.LastPublishAttemptAt),
			row.PublishRetryCount,
			pgconv.PgTextToStringPtr(row.PublishError),
			pgconv.PgTimestamptzToTimePtr(row.StalledAt),
		)
	}
	return out, nil
}

func (r *PGBatchTrigger) MarkBatchPublished(ctx context.Context, batchID uuid.UUID) error {
	return r.q.MarkBatchPublished(ctx, batchID)
}

func (r *PGBatchTrigger) RecordBatchPublishFailure(ctx context.Context, batchID uuid.UUID, publishErr string) error {
	return r.q.RecordBatchPublishFailure(ctx, RecordBatchPublishFailureParams{
		ID:           batchID,
		PublishError: pgconv.StringPtrToPgText(&publishErr),
	})
}

func (r *PGBatchTrigger) ListTasksByBatchID(ctx context.Context, batchID uuid.UUID) ([]repo.Task, error) {
	rows, err := r.q.ListTasksByBatchID(ctx, batchID)
	if err != nil {
		return nil, err
	}
	out := make([]repo.Task, len(rows))
	for i, row := range rows {
		out[i] = dbTaskToRepoTask(row)
	}
	return out, nil
}

func (r *PGBatchTrigger) CountCandidatesByBatchID(ctx context.Context, batchID uuid.UUID) (int64, error) {
	return r.q.CountCandidatesByBatchID(ctx, pgconv.UUIDToPgUUID(batchID))
}

func (r *PGBatchTrigger) ListContentsByBatchID(ctx context.Context, batchID uuid.UUID) ([]repo.Content, error) {
	rows, err := r.q.ListContentsByBatchID(ctx, pgtype.UUID{Bytes: batchID, Valid: true})
	if err != nil {
		return nil, err
	}
	out := make([]repo.Content, len(rows))
	for i, row := range rows {
		out[i] = dbContentToRepoContent(row)
	}
	return out, nil
}

// Models repository.
func (r *PGModels) GetModelByID(ctx context.Context, id int16) (repo.Model, error) {
	row, err := r.q.GetModelByID(ctx, id)
	if err != nil {
		return repo.Model{}, err
	}
	return dbModelToRepoModel(row), nil
}

func (r *PGModels) GetModelByNameAndType(ctx context.Context, name string, modelType string) (repo.Model, error) {
	row, err := r.q.GetModelByNameAndType(ctx, GetModelByNameAndTypeParams{
		Name: name,
		Type: ModelType(modelType),
	})
	if err != nil {
		return repo.Model{}, err
	}
	return dbModelToRepoModel(row), nil
}

func (r *PGModels) GetEmbedderByName(ctx context.Context, name string) (repo.Model, error) {
	return r.GetModelByNameAndType(ctx, name, string(ModelTypeEMBEDDER))
}

func (r *PGModels) GetExtractorByName(ctx context.Context, name string) (repo.Model, error) {
	return r.GetModelByNameAndType(ctx, name, string(ModelTypeEXTRACTOR))
}

func (r *PGModels) GetAnalyzerByName(ctx context.Context, name string) (repo.Model, error) {
	return r.GetModelByNameAndType(ctx, name, string(ModelTypeANALYZER))
}

// Embeddings repository.

func (r *PGEmbeddings) GetCandidateEmbeddingInputHash(ctx context.Context, candidateID uuid.UUID, modelID int16, category string) (string, error) {
	return r.q.GetCandidateEmbeddingInputHash(ctx, GetCandidateEmbeddingInputHashParams{
		CandidateID: candidateID,
		ModelID:     modelID,
		Category:    EmbeddingCategory(category),
	})
}

func (r *PGEmbeddings) GetContentEmbeddingInputHash(ctx context.Context, contentID uuid.UUID, modelID int16) (string, error) {
	return r.q.GetContentEmbeddingInputHash(ctx, GetContentEmbeddingInputHashParams{
		ContentID: contentID,
		ModelID:   modelID,
	})
}

func (r *PGOperator) CreateModel(ctx context.Context, arg repo.CreateModelParams) (repo.Model, error) {
	publishDate := pgtype.Date{}
	if arg.PublishDate != nil {
		publishDate = pgtype.Date{Time: *arg.PublishDate, Valid: true}
	}
	row, err := r.q.CreateModel(ctx, CreateModelParams{
		Name:        arg.Name,
		Provider:    arg.Provider,
		Type:        ModelType(arg.Type),
		PublishDate: publishDate,
		Url:         pgconv.StringPtrToPgText(arg.URL),
		Tag:         pgconv.StringPtrToPgText(arg.Tag),
	})
	if err != nil {
		return repo.Model{}, err
	}
	return dbModelToRepoModel(row), nil
}

func (r *PGEmbeddings) UpsertCandidateEmbedding(ctx context.Context, arg repo.CreateCandidateEmbeddingParams) (repo.CandidateEmbedding, error) {
	row, err := r.q.UpsertCandidateEmbeddingGemma2025(ctx, UpsertCandidateEmbeddingGemma2025Params{
		CandidateID: arg.CandidateID,
		ModelID:     arg.ModelID,
		Category:    EmbeddingCategory(arg.Category),
		Vector:      pgvector.NewVector(arg.Vector),
		InputHash:   arg.InputHash,
		TraceID:     arg.TraceID,
	})
	if err != nil {
		return repo.CandidateEmbedding{}, err
	}
	return dbCandidateEmbeddingToRepoCandidateEmbedding(row), nil
}

func (r *PGEmbeddings) UpsertContentEmbedding(ctx context.Context, arg repo.CreateContentEmbeddingParams) (repo.ContentEmbedding, error) {
	row, err := r.q.UpsertContentEmbeddingGemma2025(ctx, UpsertContentEmbeddingGemma2025Params{
		ContentID: arg.ContentID,
		ModelID:   arg.ModelID,
		Vector:    pgvector.NewVector(arg.Vector),
		InputHash: arg.InputHash,
		TraceID:   arg.TraceID,
	})
	if err != nil {
		return repo.ContentEmbedding{}, err
	}
	return dbContentEmbeddingToRepoContentEmbedding(row), nil
}

func (r *PGAnalysis) CreateContentExtraction(ctx context.Context, arg repo.CreateContentExtractionParams) (repo.ContentExtraction, error) {
	row, err := r.q.CreateContentExtraction(ctx, CreateContentExtractionParams{
		ContentID:     arg.ContentID,
		ModelID:       arg.ModelID,
		PromptID:      arg.PromptID,
		SchemaName:    arg.SchemaName,
		SchemaVersion: arg.SchemaVersion,
		Title:         arg.Title,
		Summary:       arg.Summary,
		RawResult:     arg.RawResult,
		TraceID:       arg.TraceID,
	})
	if err != nil {
		return repo.ContentExtraction{}, err
	}
	return dbContentExtractionToRepoContentExtraction(row), nil
}

func (r *PGAnalysis) GetContentExtractionByID(ctx context.Context, id uuid.UUID) (repo.ContentExtraction, error) {
	row, err := r.q.GetContentExtractionByID(ctx, id)
	if err != nil {
		return repo.ContentExtraction{}, err
	}
	return dbContentExtractionToRepoContentExtraction(row), nil
}

func (r *PGAnalysis) GetContentExtractionSnapshot(ctx context.Context, arg repo.GetContentExtractionSnapshotParams) (repo.ContentExtraction, error) {
	row, err := r.q.GetContentExtractionSnapshot(ctx, GetContentExtractionSnapshotParams{
		ContentID:     arg.ContentID,
		ModelID:       arg.ModelID,
		PromptID:      arg.PromptID,
		SchemaVersion: arg.SchemaVersion,
	})
	if err != nil {
		return repo.ContentExtraction{}, err
	}
	return dbContentExtractionToRepoContentExtraction(row), nil
}

func (r *PGAnalysis) UpsertEntity(ctx context.Context, arg repo.UpsertEntityParams) (repo.Entity, error) {
	row, err := r.q.UpsertEntity(ctx, UpsertEntityParams{
		Canonical: arg.Canonical,
		Type:      EntityType(arg.Type),
	})
	if err != nil {
		return repo.Entity{}, err
	}
	return dbEntityToRepoEntity(row), nil
}

func (r *PGAnalysis) GetEntityByCanonicalAndType(ctx context.Context, canonical string, entityType string) (repo.Entity, error) {
	row, err := r.q.GetEntityByCanonicalAndType(ctx, GetEntityByCanonicalAndTypeParams{
		Canonical: canonical,
		Type:      EntityType(entityType),
	})
	if err != nil {
		return repo.Entity{}, err
	}
	return dbEntityToRepoEntity(row), nil
}

func (r *PGAnalysis) CreateContentExtractionEntity(ctx context.Context, arg repo.CreateContentExtractionEntityParams) error {
	return r.q.CreateContentExtractionEntity(ctx, CreateContentExtractionEntityParams{
		ExtractionID: arg.ExtractionID,
		EntityID:     arg.EntityID,
		Surface:      arg.Surface,
		Ordinal:      pgconv.Int16PtrToPgInt2(arg.Ordinal),
	})
}

func (r *PGAnalysis) ReplaceContentExtractionTopics(ctx context.Context, extractionID uuid.UUID, topics []string) error {
	return r.q.ReplaceContentExtractionTopics(ctx, ReplaceContentExtractionTopicsParams{
		ExtractionID: extractionID,
		Column2:      topics,
	})
}

func (r *PGAnalysis) ReplaceContentExtractionPhrases(ctx context.Context, extractionID uuid.UUID, phrases []string) error {
	return r.q.ReplaceContentExtractionPhrases(ctx, ReplaceContentExtractionPhrasesParams{
		ExtractionID: extractionID,
		Column2:      phrases,
	})
}

// User-facing fetch repository. Parallel to BatchTrigger; serves the
// user-facing observation layer for POST /page_fetch. See
// docs/plan/spec.md §6.
func (r *PGUserFetches) Create(ctx context.Context, arg repo.CreateUserFetchParams) (repo.UserFetch, error) {
	row, err := r.q.CreateUserFetch(ctx, pgconv.UUIDPtrToPgUUID(arg.UserID))
	if err != nil {
		return repo.UserFetch{}, err
	}
	return dbUserFetchToRepo(row), nil
}

func (r *PGUserFetches) Get(ctx context.Context, id uuid.UUID) (repo.UserFetch, error) {
	row, err := r.q.GetUserFetch(ctx, id)
	if err != nil {
		return repo.UserFetch{}, err
	}
	return dbUserFetchToRepo(row), nil
}

func (r *PGUserFetches) CreateItem(ctx context.Context, arg repo.CreateUserFetchItemParams) (repo.UserFetchItem, error) {
	row, err := r.q.CreateUserFetchItem(ctx, CreateUserFetchItemParams{
		FetchID:        arg.FetchID,
		CandidateID:    arg.CandidateID,
		TaskID:         pgconv.UUIDPtrToPgUUID(arg.TaskID),
		SnapshotStatus: pgconv.StringPtrToPgText(arg.SnapshotStatus),
	})
	if err != nil {
		return repo.UserFetchItem{}, err
	}
	return dbUserFetchItemToRepo(row), nil
}

func (r *PGUserFetches) GetProgress(ctx context.Context, fetchID uuid.UUID) (repo.UserFetchProgress, error) {
	row, err := r.q.GetUserFetchProgress(ctx, fetchID)
	if err != nil {
		return repo.UserFetchProgress{}, err
	}
	return repo.UserFetchProgress{
		Total:                       row.Total,
		PendingCandidateIDs:         row.PendingCandidateIds,
		RunningCandidateIDs:         row.RunningCandidateIds,
		CompletedCandidateIDs:       row.CompletedCandidateIds,
		FailedCandidateIDs:          row.FailedCandidateIds,
		AlreadyCompleteCandidateIDs: row.AlreadyCompleteCandidateIds,
		Terminal:                    row.Terminal.Bool,
	}, nil
}

func (r *PGUserFetches) MarkCompleted(ctx context.Context, fetchID uuid.UUID) error {
	return r.q.MarkUserFetchCompleted(ctx, fetchID)
}

func dbUserFetchToRepo(row Fetch) repo.UserFetch {
	return repo.UserFetch{
		ID:          row.ID,
		UserID:      pgconv.PgUUIDToUUIDPtr(row.UserID),
		CreatedAt:   *pgconv.PgTimestamptzToTimePtr(row.CreatedAt),
		CompletedAt: pgconv.PgTimestamptzToTimePtr(row.CompletedAt),
	}
}

func dbUserFetchItemToRepo(row FetchItem) repo.UserFetchItem {
	return repo.UserFetchItem{
		FetchID:        row.FetchID,
		CandidateID:    row.CandidateID,
		TaskID:         pgconv.PgUUIDToUUIDPtr(row.TaskID),
		SnapshotStatus: pgconv.PgTextToStringPtr(row.SnapshotStatus),
		CreatedAt:      *pgconv.PgTimestamptzToTimePtr(row.CreatedAt),
	}
}

func (r *PGAnalysisRuns) CreateSession(ctx context.Context, arg repo.CreateAnalysisSessionParams) (repo.AnalysisRun, error) {
	beginner, ok := r.q.db.(pgBeginner)
	if !ok {
		return repo.AnalysisRun{}, fmt.Errorf("postgres repository does not support transactions")
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return repo.AnalysisRun{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := r.q.WithTx(tx)

	fetch, err := qtx.CreateUserFetch(ctx, pgconv.UUIDPtrToPgUUID(arg.UserID))
	if err != nil {
		return repo.AnalysisRun{}, fmt.Errorf("create user fetch: %w", err)
	}

	var originalSelectedCandidateIDs []uuid.UUID
	for _, c := range arg.SelectedCandidates {
		originalSelectedCandidateIDs = append(originalSelectedCandidateIDs, c.ID)
	}

	run, err := qtx.CreateAnalysisRun(ctx, CreateAnalysisRunParams{
		ID:                           arg.AnalysisID,
		UserID:                       pgconv.UUIDPtrToPgUUID(arg.UserID),
		FetchID:                      fetch.ID,
		Topic:                        arg.Topic,
		Brief:                        arg.Brief,
		FetchFailurePolicy:           arg.FetchFailurePolicy,
		Status:                       "FETCHING",
		OriginalSelectedCandidateIds: originalSelectedCandidateIDs,
		UnavailableCandidateIds:      []uuid.UUID{},
	})
	if err != nil {
		return repo.AnalysisRun{}, fmt.Errorf("create analysis run: %w", err)
	}

	for _, c := range arg.SelectedCandidates {
		content, contentErr := qtx.GetContentByCandidateID(ctx, pgconv.UUIDToPgUUID(c.ID))
		if contentErr == nil && !content.DeletedAt.Valid && strings.TrimSpace(content.Content) != "" {
			snapshot := repo.UserFetchItemSnapshotAlreadyComplete
			if _, err := qtx.CreateUserFetchItem(ctx, CreateUserFetchItemParams{
				FetchID:        fetch.ID,
				CandidateID:    c.ID,
				SnapshotStatus: pgconv.StringPtrToPgText(&snapshot),
			}); err != nil {
				return repo.AnalysisRun{}, fmt.Errorf("create fetch item for existing content: %w", err)
			}
			continue
		}

		if contentErr != nil && !errors.Is(contentErr, pgx.ErrNoRows) {
			return repo.AnalysisRun{}, fmt.Errorf("check existing content for candidate %s: %w", c.ID, contentErr)
		}

		canonicalURL, err := utils.NormalizeURL(c.URL)
		if err != nil {
			return repo.AnalysisRun{}, fmt.Errorf("normalize candidate %s url: %w", c.ID, err)
		}

		meta, err := json.Marshal(map[string]any{"candidate_id": c.ID.String()})
		if err != nil {
			return repo.AnalysisRun{}, fmt.Errorf("marshal candidate meta: %w", err)
		}

		task, err := createTaskRepo(ctx, qtx.db, qtx, repo.CreateTaskParams{
			BatchID:    arg.AnalysisID,
			Kind:       repo.TaskKindPageFetch,
			SourceType: repo.SourceTypeMedia,
			SourceAbbr: c.SourceAbbr,
			URL:        canonicalURL,
			Meta:       meta,
			TraceID:    c.TraceID,
		})

		switch {
		case err == nil:
			taskID := task.ID
			if _, err := qtx.CreateUserFetchItem(ctx, CreateUserFetchItemParams{
				FetchID:     fetch.ID,
				CandidateID: c.ID,
				TaskID:      pgconv.UUIDPtrToPgUUID(&taskID),
			}); err != nil {
				return repo.AnalysisRun{}, fmt.Errorf("create fetch item for task %s: %w", taskID, err)
			}

		case errors.Is(err, repo.ErrTaskAlreadyActive):
			if task.ID != uuid.Nil {
				taskID := task.ID
				if _, err := qtx.CreateUserFetchItem(ctx, CreateUserFetchItemParams{
					FetchID:     fetch.ID,
					CandidateID: c.ID,
					TaskID:      pgconv.UUIDPtrToPgUUID(&taskID),
				}); err != nil {
					return repo.AnalysisRun{}, fmt.Errorf("create fetch item for existing task %s: %w", taskID, err)
				}
				continue
			}

			// Race: task finished between checks
			content, contentErr := qtx.GetContentByURL(ctx, canonicalURL)
			if contentErr != nil {
				if errors.Is(contentErr, pgx.ErrNoRows) {
					return repo.AnalysisRun{}, fmt.Errorf("page_fetch race: active task drained without contents row (design invariant)")
				}
				return repo.AnalysisRun{}, fmt.Errorf("fetch contents after task drained: %w", contentErr)
			}
			if content.DeletedAt.Valid || strings.TrimSpace(content.Content) == "" {
				return repo.AnalysisRun{}, fmt.Errorf("page_fetch race: active task drained but content is unavailable")
			}
			snapshot := repo.UserFetchItemSnapshotAlreadyComplete
			if _, err := qtx.CreateUserFetchItem(ctx, CreateUserFetchItemParams{
				FetchID:        fetch.ID,
				CandidateID:    c.ID,
				SnapshotStatus: pgconv.StringPtrToPgText(&snapshot),
			}); err != nil {
				return repo.AnalysisRun{}, fmt.Errorf("create fetch item after task race: %w", err)
			}

		default:
			return repo.AnalysisRun{}, fmt.Errorf("create page_fetch task: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return repo.AnalysisRun{}, fmt.Errorf("commit analysis session: %w", err)
	}

	return dbAnalysisRunToRepo(run), nil
}

func (r *PGAnalysisRuns) Create(ctx context.Context, arg repo.CreateAnalysisRunParams) (repo.AnalysisRun, error) {
	row, err := r.q.CreateAnalysisRun(ctx, CreateAnalysisRunParams{
		ID: arg.ID, UserID: pgconv.UUIDPtrToPgUUID(arg.UserID), FetchID: arg.FetchID,
		Topic: arg.Topic, Brief: arg.Brief, FetchFailurePolicy: arg.FetchFailurePolicy,
		Status: arg.Status, OriginalSelectedCandidateIds: arg.OriginalSelectedCandidateIDs,
		UnavailableCandidateIds: arg.UnavailableCandidateIDs,
	})
	if err != nil {
		return repo.AnalysisRun{}, err
	}
	return dbAnalysisRunToRepo(row), nil
}

func (r *PGAnalysisRuns) GetByID(ctx context.Context, id uuid.UUID) (repo.AnalysisRun, error) {
	row, err := r.q.GetAnalysisRunByID(ctx, id)
	if err != nil {
		return repo.AnalysisRun{}, err
	}
	return dbAnalysisRunToRepo(row), nil
}

func (r *PGAnalysisRuns) GetByFetchID(ctx context.Context, fetchID uuid.UUID) (repo.AnalysisRun, error) {
	row, err := r.q.GetAnalysisRunByFetchID(ctx, fetchID)
	if err != nil {
		return repo.AnalysisRun{}, err
	}
	return dbAnalysisRunToRepo(row), nil
}

func (r *PGAnalysisRuns) ListItems(ctx context.Context, fetchID uuid.UUID) ([]repo.AnalysisRunItem, error) {
	rows, err := r.q.ListAnalysisRunItems(ctx, fetchID)
	if err != nil {
		return nil, err
	}
	items := make([]repo.AnalysisRunItem, len(rows))
	for i, row := range rows {
		items[i] = repo.AnalysisRunItem{
			CandidateID:    row.CandidateID,
			TaskID:         pgconv.PgUUIDToUUIDPtr(row.TaskID),
			SnapshotStatus: pgconv.PgTextToStringPtr(row.SnapshotStatus),
			TaskStatus:     taskStatusPtr(row.TaskStatus),
			ContentID:      pgconv.PgUUIDToUUIDPtr(row.ContentID),
		}
	}
	return items, nil
}

func (r *PGAnalysisRuns) SetManifest(ctx context.Context, arg repo.SetAnalysisRunManifestParams) (repo.AnalysisRun, error) {
	row, err := r.q.SetAnalysisRunManifest(ctx, SetAnalysisRunManifestParams{
		ID: arg.ID, ReadyCandidateIds: arg.ReadyCandidateIDs,
		ReadyContentIds: arg.ReadyContentIDs, FailedCandidateIds: arg.FailedCandidateIDs,
	})
	if err != nil {
		return repo.AnalysisRun{}, err
	}
	return dbAnalysisRunToRepo(row), nil
}

func (r *PGAnalysisRuns) SetStatus(ctx context.Context, arg repo.SetAnalysisRunStatusParams) (repo.AnalysisRun, error) {
	row, err := r.q.SetAnalysisRunStatus(ctx, SetAnalysisRunStatusParams{
		ID: arg.ID, Status: arg.Status, FailedCandidateIds: arg.FailedCandidateIDs,
		FailureCode: pgconv.StringPtrToPgText(arg.FailureCode),
	})
	if err != nil {
		return repo.AnalysisRun{}, err
	}
	return dbAnalysisRunToRepo(row), nil
}

func (r *PGAnalysisRuns) SetRoot(ctx context.Context, id, rootBatchID uuid.UUID) (repo.AnalysisRun, error) {
	row, err := r.q.SetAnalysisRunRoot(ctx, SetAnalysisRunRootParams{
		ID: id, RootBatchID: pgconv.UUIDToPgUUID(rootBatchID),
	})
	if err != nil {
		return repo.AnalysisRun{}, err
	}
	return dbAnalysisRunToRepo(row), nil
}

func (r *PGAnalysisRuns) SetExecution(ctx context.Context, id, executionID uuid.UUID) (repo.AnalysisRun, error) {
	row, err := r.q.SetAnalysisRunExecution(ctx, SetAnalysisRunExecutionParams{
		ExecutionID: pgconv.UUIDToPgUUID(executionID), ID: id,
	})
	if err != nil {
		return repo.AnalysisRun{}, err
	}
	return dbAnalysisRunToRepo(row), nil
}

func (r *PGAnalysisRuns) CancelItems(ctx context.Context, fetchID uuid.UUID) error {
	return r.q.CancelAnalysisRunItems(ctx, fetchID)
}

func (r *PGAnalysisRuns) RetryFailedItems(ctx context.Context, runID uuid.UUID) error {
	beginner, ok := r.q.db.(pgBeginner)
	if !ok {
		return fmt.Errorf("postgres repository does not support transactions")
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	run, err := r.GetByID(ctx, runID)
	if err != nil {
		return fmt.Errorf("get run: %w", err)
	}

	items, err := qtx.ListUserFetchItems(ctx, run.FetchID)
	if err != nil {
		return fmt.Errorf("list fetch items: %w", err)
	}

	var failedCandidateIDs []uuid.UUID
	for _, item := range items {
		if item.TaskStatus.Valid && string(item.TaskStatus.TaskStatus) == string(repo.TaskStatusFailed) {
			failedCandidateIDs = append(failedCandidateIDs, item.CandidateID)
		}
	}

	if len(failedCandidateIDs) == 0 {
		return nil // nothing to retry
	}

	candidates, err := qtx.GetCandidatesByIDs(ctx, failedCandidateIDs)
	if err != nil {
		return fmt.Errorf("get candidates: %w", err)
	}

	candidateMap := make(map[uuid.UUID]Candidate, len(candidates))
	for _, c := range candidates {
		candidateMap[c.ID] = c
	}

	for _, cID := range failedCandidateIDs {
		c, ok := candidateMap[cID]
		if !ok {
			return fmt.Errorf("candidate %s not found for retry", cID)
		}

		canonicalURL, err := utils.NormalizeURL(c.Url)
		if err != nil {
			return fmt.Errorf("normalize url: %w", err)
		}

		meta, err := json.Marshal(c)
		if err != nil {
			return fmt.Errorf("marshal candidate meta: %w", err)
		}

		task, err := createTaskRepo(ctx, qtx.db, qtx, repo.CreateTaskParams{
			BatchID:    runID, // Re-use the analysis ID as batch ID
			Kind:       repo.TaskKindPageFetch,
			SourceType: repo.SourceTypeMedia,
			SourceAbbr: c.SourceAbbr,
			URL:        canonicalURL,
			Meta:       meta,
			TraceID:    c.TraceID,
		})
		switch {
		case err == nil:
			taskID := task.ID
			if err := qtx.UpdateUserFetchItemStatus(ctx, UpdateUserFetchItemStatusParams{
				FetchID:     run.FetchID,
				CandidateID: cID,
				TaskID:      pgconv.UUIDPtrToPgUUID(&taskID),
			}); err != nil {
				return fmt.Errorf("update fetch item: %w", err)
			}

		case errors.Is(err, repo.ErrTaskAlreadyActive):
			if task.ID != uuid.Nil {
				taskID := task.ID
				if err := qtx.UpdateUserFetchItemStatus(ctx, UpdateUserFetchItemStatusParams{
					FetchID:     run.FetchID,
					CandidateID: cID,
					TaskID:      pgconv.UUIDPtrToPgUUID(&taskID),
				}); err != nil {
					return fmt.Errorf("update fetch item for existing task: %w", err)
				}
				continue
			}

			// Race: task finished between checks
			content, contentErr := qtx.GetContentByURL(ctx, canonicalURL)
			if contentErr != nil {
				if errors.Is(contentErr, pgx.ErrNoRows) {
					return fmt.Errorf("retry race: active task drained without contents row (design invariant)")
				}
				return fmt.Errorf("fetch contents after retry task drained: %w", contentErr)
			}
			if content.DeletedAt.Valid || strings.TrimSpace(content.Content) == "" {
				return fmt.Errorf("retry race: active task drained but content is unavailable")
			}
			snapshot := repo.UserFetchItemSnapshotAlreadyComplete
			if err := qtx.UpdateUserFetchItemStatus(ctx, UpdateUserFetchItemStatusParams{
				FetchID:        run.FetchID,
				CandidateID:    cID,
				SnapshotStatus: pgconv.StringPtrToPgText(&snapshot),
			}); err != nil {
				return fmt.Errorf("update fetch item after retry task race: %w", err)
			}

		default:
			return fmt.Errorf("create task for retry: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func (r *PGReports) GetByID(ctx context.Context, id uuid.UUID) (repo.Report, error) {
	row, err := r.q.GetReportByID(ctx, id)
	if err != nil {
		return repo.Report{}, err
	}
	return dbReportToRepo(row), nil
}

func (r *PGReports) EnsureExecution(ctx context.Context, id uuid.UUID, fingerprint string) error {
	return r.q.EnsureAnalysisExecution(ctx, EnsureAnalysisExecutionParams{
		ID: id, ReportFingerprint: fingerprint,
	})
}

func (r *PGReports) GetByExecutionID(ctx context.Context, id uuid.UUID) (repo.Report, error) {
	row, err := r.q.GetReportByExecutionID(ctx, id)
	if err != nil {
		return repo.Report{}, err
	}
	return dbReportToRepo(row), nil
}

func (r *PGReports) FindCacheHit(ctx context.Context, id uuid.UUID) (repo.Report, error) {
	row, err := r.q.FindReportCacheHit(ctx, id)
	if err != nil {
		return repo.Report{}, err
	}
	return dbReportToRepo(row), nil
}

func (r *PGReports) MarkMissing(ctx context.Context, id uuid.UUID, requestID *string) (bool, error) {
	return r.markAvailability(ctx, id, requestID, "ARTIFACT_MISSING", "report-read", func(q *Queries) (int64, error) {
		return q.MarkReportMissing(ctx, id)
	})
}

func (r *PGReports) MarkCorrupt(ctx context.Context, arg repo.MarkReportCorruptParams) (bool, error) {
	return r.markAvailability(ctx, arg.ReportID, arg.RequestID, "ARTIFACT_CORRUPT", "report-read", func(q *Queries) (int64, error) {
		return q.MarkReportCorrupt(ctx, MarkReportCorruptParams{
			ID:                       arg.ReportID,
			ArtifactCorruptionReason: pgconv.StringPtrToPgText(&arg.Reason),
		})
	})
}

func (r *PGReports) markAvailability(ctx context.Context, id uuid.UUID, requestID *string, eventType, component string, mark func(*Queries) (int64, error)) (bool, error) {
	beginner, ok := r.db.(pgBeginner)
	if !ok {
		return false, fmt.Errorf("postgres repository does not support transactions")
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := r.q.WithTx(tx)
	rows, err := mark(qtx)
	if err != nil {
		return false, err
	}
	if rows == 1 {
		report, err := qtx.GetReportByID(ctx, id)
		if err != nil {
			return false, err
		}
		if err := qtx.InsertReportAuditEvent(ctx, reportAuditEventParams(repo.ReportAuditEventParams{
			ReportID:            report.ID,
			AnalysisExecutionID: report.AnalysisExecutionID,
			EventType:           eventType,
			ActorComponent:      component,
			RequestID:           requestID,
			StorageURI:          report.StorageUri,
			StorageOutcome:      "NOT_ATTEMPTED",
		})); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return rows == 1, nil
}

func (r *PGReports) BeginRemoval(ctx context.Context, arg repo.BeginReportRemovalParams) (repo.Report, error) {
	beginner, ok := r.db.(pgBeginner)
	if !ok {
		return repo.Report{}, fmt.Errorf("postgres repository does not support transactions")
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return repo.Report{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := r.q.WithTx(tx)
	report, err := qtx.LockReportForRemoval(ctx, arg.AnalysisExecutionID)
	if err != nil {
		return repo.Report{}, err
	}
	if report.ArtifactRemovedAt.Valid {
		if err := tx.Commit(ctx); err != nil {
			return repo.Report{}, err
		}
		return dbReportToRepo(report), nil
	}
	if rows, err := qtx.MarkReportRemoved(ctx, MarkReportRemovedParams{
		ActorTokenID: pgconv.UUIDToPgUUID(arg.ActorTokenID),
		Reason:       pgconv.StringPtrToPgText(&arg.Reason),
		ID:           report.ID,
	}); err != nil {
		return repo.Report{}, err
	} else if rows != 1 {
		return repo.Report{}, repo.ErrReportTaskStale
	}
	if err := qtx.InsertReportAuditEvent(ctx, reportAuditEventParams(repo.ReportAuditEventParams{
		ReportID:            report.ID,
		AnalysisExecutionID: report.AnalysisExecutionID,
		EventType:           "ADMIN_REMOVE",
		ActorTokenID:        &arg.ActorTokenID,
		ActorComponent:      "operator",
		ActorName:           arg.ActorName,
		Reason:              &arg.Reason,
		RequestID:           arg.RequestID,
		StorageURI:          report.StorageUri,
		StorageOutcome:      "NOT_ATTEMPTED",
	})); err != nil {
		return repo.Report{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return repo.Report{}, err
	}
	updated, err := r.q.GetReportByExecutionID(ctx, arg.AnalysisExecutionID)
	if err != nil {
		return repo.Report{}, err
	}
	report = updated
	return dbReportToRepo(report), nil
}

func (r *PGReports) RecordAuditEvent(ctx context.Context, arg repo.ReportAuditEventParams) error {
	return r.q.InsertReportAuditEvent(ctx, reportAuditEventParams(arg))
}

func reportAuditEventParams(arg repo.ReportAuditEventParams) InsertReportAuditEventParams {
	return InsertReportAuditEventParams{
		ReportID:            arg.ReportID,
		AnalysisExecutionID: arg.AnalysisExecutionID,
		EventType:           arg.EventType,
		ActorTokenID:        pgconv.UUIDPtrToPgUUID(arg.ActorTokenID),
		ActorComponent:      arg.ActorComponent,
		ActorName:           pgconv.StringPtrToPgText(arg.ActorName),
		Reason:              pgconv.StringPtrToPgText(arg.Reason),
		RequestID:           pgconv.StringPtrToPgText(arg.RequestID),
		StorageUri:          arg.StorageURI,
		StorageOutcome:      arg.StorageOutcome,
		StorageError:        pgconv.StringPtrToPgText(arg.StorageError),
	}
}

func dbReportToRepo(row Report) repo.Report {
	return repo.Report{
		ID:                       row.ID,
		AnalysisExecutionID:      row.AnalysisExecutionID,
		RootBatchID:              row.RootBatchID,
		StorageURI:               row.StorageUri,
		ByteSize:                 row.ByteSize,
		SHA256:                   row.Sha256,
		ExpiresAt:                row.ExpiresAt.Time,
		ArtifactMissingAt:        pgconv.PgTimestamptzToTimePtr(row.ArtifactMissingAt),
		ArtifactCorruptAt:        pgconv.PgTimestamptzToTimePtr(row.ArtifactCorruptAt),
		ArtifactCorruptionReason: pgconv.PgTextToStringPtr(row.ArtifactCorruptionReason),
		ArtifactRemovedAt:        pgconv.PgTimestamptzToTimePtr(row.ArtifactRemovedAt),
		ArtifactRemovedBy:        pgconv.PgUUIDToUUIDPtr(row.ArtifactRemovedBy),
		ArtifactRemovalReason:    pgconv.PgTextToStringPtr(row.ArtifactRemovalReason),
		CreatedAt:                row.CreatedAt.Time,
		UpdatedAt:                row.UpdatedAt.Time,
	}
}

func reportMatchesParams(row Report, arg repo.CreateReportParams) bool {
	return row.AnalysisExecutionID == arg.AnalysisExecutionID &&
		row.RootBatchID == arg.RootBatchID &&
		row.StorageUri == arg.StorageURI &&
		row.ByteSize == arg.ByteSize &&
		row.Sha256 == arg.SHA256
}

func dbAnalysisRunToRepo(row AnalysisRun) repo.AnalysisRun {
	return repo.AnalysisRun{
		ID:                           row.ID,
		UserID:                       pgconv.PgUUIDToUUIDPtr(row.UserID),
		FetchID:                      row.FetchID,
		Topic:                        row.Topic,
		Brief:                        row.Brief,
		FetchFailurePolicy:           row.FetchFailurePolicy,
		Status:                       row.Status,
		OriginalSelectedCandidateIDs: row.OriginalSelectedCandidateIds,
		UnavailableCandidateIDs:      row.UnavailableCandidateIds,
		ReadyCandidateIDs:            row.ReadyCandidateIds,
		ReadyContentIDs:              row.ReadyContentIds,
		FailedCandidateIDs:           row.FailedCandidateIds,
		RootBatchID:                  pgconv.PgUUIDToUUIDPtr(row.RootBatchID),
		ExecutionID:                  pgconv.PgUUIDToUUIDPtr(row.ExecutionID),
		ReportID:                     pgconv.PgUUIDToUUIDPtr(row.ReportID),
		FailureCode:                  pgconv.PgTextToStringPtr(row.FailureCode),
		ConfirmedAt:                  pgconv.PgTimestamptzToTimePtr(row.ConfirmedAt),
		CreatedAt:                    row.CreatedAt,
		UpdatedAt:                    row.UpdatedAt,
	}
}

func stringPtrIfNotEmpty(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func taskStatusPtr(value any) *string {
	switch typed := value.(type) {
	case string:
		return stringPtrIfNotEmpty(typed)
	case []byte:
		return stringPtrIfNotEmpty(string(typed))
	default:
		return nil
	}
}
