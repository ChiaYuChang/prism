package pg

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/ChiaYuChang/prism/pkg/pgconv"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
	q *Queries
}

type PGPipeline struct {
	q *Queries
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

type pgBeginner interface {
	Begin(context.Context) (pgx.Tx, error)
}

var _ repo.Repository = (*PGRepository)(nil)
var _ repo.Scheduler = (*PGScheduler)(nil)
var _ repo.Scout = (*PGScout)(nil)
var _ repo.Tasks = (*PGTasks)(nil)
var _ repo.Pipeline = (*PGPipeline)(nil)
var _ repo.Embeddings = (*PGEmbeddings)(nil)
var _ repo.Analysis = (*PGAnalysis)(nil)
var _ repo.BatchTrigger = (*PGBatchTrigger)(nil)
var _ repo.UserFetches = (*PGUserFetches)(nil)
var _ repo.Schedules = (*PGSchedules)(nil)
var _ repo.Operator = (*PGOperator)(nil)
var _ repo.Prompts = (*PGPrompts)(nil)
var _ repo.Tokens = (*PGTokens)(nil)
var _ repo.Sources = (*PGSources)(nil)

// Repository root getters.
func (r *PGRepository) Scheduler() repo.Scheduler {
	return &PGScheduler{q: r.q}
}

func (r *PGRepository) Scout() repo.Scout {
	return &PGScout{q: r.q}
}

func (r *PGRepository) Tasks() repo.Tasks {
	return &PGTasks{q: r.q}
}

func (r *PGRepository) Pipeline() repo.Pipeline {
	return &PGPipeline{q: r.q}
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
	return createTaskRepo(ctx, r.q, arg)
}

func createTaskRepo(ctx context.Context, q *Queries, arg repo.CreateTaskParams) (repo.Task, error) {
	if err := q.EnsureBatchExists(ctx, repoCreateTaskParamsToEnsureBatchExists(arg)); err != nil {
		return repo.Task{}, fmt.Errorf("ensure batch %s exists: %w", arg.BatchID, err)
	}

	row, err := q.CreateTask(ctx, repoCreateTaskParamsToDB(arg))
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
		return task, repo.ErrTaskAlreadyActive
	}
	return task, nil
}

func (r *PGTasks) ExtendActiveTaskExpiry(ctx context.Context, arg repo.ExtendActiveTaskExpiryParams) error {
	return r.q.ExtendActiveTaskExpiry(ctx, repoExtendActiveTaskExpiryParamsToDB(arg))
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
			materializedTask, taskErr = createTaskRepo(ctx, qtx, repo.CreateTaskParams{
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
		HashAlgorithm: arg.HashAlgorithm,
		TokenHash:     arg.TokenHash,
		ExpiresAt:     pgtype.Timestamptz{Time: arg.ExpiresAt, Valid: true},
	})
	if err != nil {
		return repo.Token{}, err
	}
	return dbTokenToRepoToken(row), nil
}

func (r *PGTokens) GetTokenByID(ctx context.Context, id uuid.UUID) (repo.Token, error) {
	row, err := r.q.GetTokenByID(ctx, id)
	if err != nil {
		return repo.Token{}, err
	}
	return dbTokenToRepoToken(row), nil
}

func (r *PGTokens) GetRootToken(ctx context.Context) (repo.Token, error) {
	row, err := r.q.GetRootToken(ctx)
	if err != nil {
		return repo.Token{}, err
	}
	return dbTokenToRepoToken(row), nil
}

func (r *PGTokens) ListTokens(ctx context.Context, params repo.ListOperatorParams) ([]repo.Token, error) {
	rows, err := r.q.ListTokens(ctx, ListTokensParams{Lim: params.Limit, Off: operatorOffset(params)})
	if err != nil {
		return nil, err
	}
	out := make([]repo.Token, len(rows))
	for i, row := range rows {
		out[i] = dbTokenToRepoToken(row)
	}
	return out, nil
}

func (r *PGTokens) RenewToken(ctx context.Context, id uuid.UUID, expiresAt time.Time) (repo.Token, error) {
	row, err := r.q.RenewToken(ctx, RenewTokenParams{ID: id, ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true}})
	if err != nil {
		return repo.Token{}, err
	}
	return dbTokenToRepoToken(row), nil
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
	return dbTokenToRepoToken(row), nil
}

func (r *PGTokens) RevokeToken(ctx context.Context, id uuid.UUID) (repo.Token, error) {
	row, err := r.q.RevokeToken(ctx, id)
	if err != nil {
		return repo.Token{}, err
	}
	return dbTokenToRepoToken(row), nil
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

// Batch Trigger repository.
func (r *PGBatchTrigger) ListPendingCompletionBatches(ctx context.Context, limit int32, sourceType string) ([]repo.Batch, error) {
	rows, err := r.q.ListPendingCompletionBatches(ctx, ListPendingCompletionBatchesParams{
		SourceType: SourceType(sourceType),
		Limit:      limit,
	})
	if err != nil {
		return nil, err
	}

	out := make([]repo.Batch, len(rows))
	for i, row := range rows {
		out[i] = dbBatchToRepoBatch(
			row.ID,
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

func (r *PGBatchTrigger) FindNewlyCompletedBatches(ctx context.Context, limit int32, sourceType string) ([]repo.Batch, error) {
	rows, err := r.q.FindNewlyCompletedBatches(ctx, FindNewlyCompletedBatchesParams{
		SourceType: SourceType(sourceType),
		Limit:      limit,
	})
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

func (r *PGBatchTrigger) ListReadyToPublishBatches(ctx context.Context, limit int32, sourceType string) ([]repo.Batch, error) {
	rows, err := r.q.ListReadyToPublishBatches(ctx, ListReadyToPublishBatchesParams{
		SourceType: SourceType(sourceType),
		Limit:      limit,
	})
	if err != nil {
		return nil, err
	}

	out := make([]repo.Batch, len(rows))
	for i, row := range rows {
		out[i] = dbBatchToRepoBatch(
			row.ID,
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

// Embeddings repository.
func (r *PGEmbeddings) GetModelByID(ctx context.Context, id int16) (repo.Model, error) {
	row, err := r.q.GetModelByID(ctx, id)
	if err != nil {
		return repo.Model{}, err
	}
	return dbModelToRepoModel(row), nil
}

func (r *PGEmbeddings) GetModelByNameAndType(ctx context.Context, name string, modelType string) (repo.Model, error) {
	row, err := r.q.GetModelByNameAndType(ctx, GetModelByNameAndTypeParams{
		Name: name,
		Type: ModelType(modelType),
	})
	if err != nil {
		return repo.Model{}, err
	}
	return dbModelToRepoModel(row), nil
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

func (r *PGEmbeddings) CreateCandidateEmbedding(ctx context.Context, arg repo.CreateCandidateEmbeddingParams) (repo.CandidateEmbedding, error) {
	row, err := r.q.CreateCandidateEmbeddingGemma2025(ctx, CreateCandidateEmbeddingGemma2025Params{
		CandidateID: arg.CandidateID,
		ModelID:     arg.ModelID,
		Category:    EmbeddingCategory(arg.Category),
		Vector:      pgvector.NewVector(arg.Vector),
		TraceID:     arg.TraceID,
	})
	if err != nil {
		return repo.CandidateEmbedding{}, err
	}
	return dbCandidateEmbeddingToRepoCandidateEmbedding(row), nil
}

func (r *PGEmbeddings) CreateContentEmbedding(ctx context.Context, arg repo.CreateContentEmbeddingParams) (repo.ContentEmbedding, error) {
	row, err := r.q.CreateContentEmbeddingGemma2025(ctx, CreateContentEmbeddingGemma2025Params{
		ContentID: arg.ContentID,
		ModelID:   arg.ModelID,
		Category:  EmbeddingCategory(arg.Category),
		Vector:    pgvector.NewVector(arg.Vector),
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
