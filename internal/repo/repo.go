package repo

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Repository interface {
	Scheduler() Scheduler
	Scout() Scout
	Tasks() Tasks
	Pipeline() Pipeline
	PipelineRuntime() PipelineRuntime
	Embedding() Embeddings
	Analysis() Analysis
	BatchTrigger() BatchTrigger
	UserFetches() UserFetches
	AnalysisRuns() AnalysisRuns
	Reports() Reports
	Schedules() Schedules
	Operator() Operator
	Prompts() Prompts
	Planner() PlannerResults
	Tokens() Tokens
	RootControl() RootControl
	Sources() Sources
	Models() Models
}

type RootControl interface {
	InitRoot(ctx context.Context, arg CreateRootControlParams) (Token, error)
	CheckRoot(ctx context.Context, arg RootAuthParams) (bool, error)
	CreateAdmin(ctx context.Context, arg CreateRootAdminParams) (Token, error)
	RevokeAll(ctx context.Context, arg RootAuthParams) (int64, error)
}

// TaskReporter is the push side of the task lifecycle: workers use it to
// report the outcome of a claimed task. It is intentionally narrower than
// Scheduler so worker handlers only depend on what they actually call.
type TaskReporter interface {
	CompleteTask(ctx context.Context, id uuid.UUID) error
	FailTask(ctx context.Context, id uuid.UUID, retryMax int, failureMessage string) error
}

type Scheduler interface {
	TaskReporter
	// ClaimTasks claims up to limit runnable tasks of the given kinds.
	// sourceTypes filters by source_type; an empty slice matches all source types.
	ClaimTasks(ctx context.Context, limit int32, kinds []string, sourceTypes []string) ([]Task, error)
	// ReleaseTasks resets RUNNING tasks back to PENDING, undoing the retry_count
	// increment from ClaimTasks. Used when dispatch is skipped (e.g. rate-limited).
	ReleaseTasks(ctx context.Context, ids []uuid.UUID) error
	ListRunnableTasks(ctx context.Context, limit int32) ([]Task, error)
}

type Scout interface {
	GetSourceByAbbr(ctx context.Context, abbr string) (Source, error)
	ListSourcesByType(ctx context.Context, sourceType string) ([]Source, error)
	GetCandidateByID(ctx context.Context, id uuid.UUID) (Candidate, error)
	GetCandidatesByIDs(ctx context.Context, ids []uuid.UUID) ([]Candidate, error)
	GetCandidateByFingerprint(ctx context.Context, fingerprint string) (Candidate, error)
	ListCandidates(ctx context.Context, arg ListCandidatesParams) ([]Candidate, error)
	CountCandidatesByBatchID(ctx context.Context, batchID uuid.UUID) (int64, error)
	ListCandidatesByBatchID(ctx context.Context, batchID uuid.UUID) ([]Candidate, error)
	CreateCandidate(ctx context.Context, arg CreateCandidateParams) (Candidate, error)
	UpsertCandidate(ctx context.Context, arg UpsertCandidateParams) (Candidate, error)
}

type Sources interface {
	Create(ctx context.Context, arg CreateSourceParams) (Source, error)
	Update(ctx context.Context, arg UpdateSourceParams) (Source, error)
	Delete(ctx context.Context, abbr string) (Source, error)
	Restore(ctx context.Context, abbr string) (Source, error)
}

type Tasks interface {
	EnsureBatch(ctx context.Context, arg EnsureBatchParams) error
	GetTaskByID(ctx context.Context, id uuid.UUID) (Task, error)
	IsTaskRunning(ctx context.Context, id uuid.UUID) (bool, error)
	ListTasksByBatchID(ctx context.Context, batchID uuid.UUID) ([]Task, error)
	ListTaskStatusSummary(ctx context.Context) ([]TaskStatusSummary, error)
	ListRecentFailedTasks(ctx context.Context, limit int32) ([]FailedTaskSummary, error)
	RetryFailedTask(ctx context.Context, id uuid.UUID) (Task, error)
	// CreateTask is insert-or-recover: on unique-violation against an
	// existing PENDING/RUNNING task it returns the existing row alongside
	// repo.ErrTaskAlreadyActive, so callers that need the existing task_id
	// (e.g. the user-fetch handler) avoid a second round-trip.
	CreateTask(ctx context.Context, arg CreateTaskParams) (Task, error)
	ExtendActiveTaskExpiry(ctx context.Context, arg ExtendActiveTaskExpiryParams) error
	CancelPendingTasksByBatchID(ctx context.Context, batchID uuid.UUID, reason string) (int64, error)
}

type Schedules interface {
	SyncSchedules(ctx context.Context, schedules []UpsertScheduleParams) ([]Schedule, error)
	UpsertSchedule(ctx context.Context, arg UpsertScheduleParams) (Schedule, error)
	ListSchedules(ctx context.Context, limit int32) ([]Schedule, error)
	MaterializeDueSchedules(ctx context.Context, arg MaterializeDueSchedulesParams) ([]ScheduleMaterialization, error)
}

type Operator interface {
	ListModels(ctx context.Context, params ListOperatorParams) ([]Model, error)
	CreateModel(ctx context.Context, arg CreateModelParams) (Model, error)
	ListSources(ctx context.Context, params ListOperatorParams) ([]Source, error)
	ListBatches(ctx context.Context, params ListOperatorParams) ([]Batch, error)
	ListEntities(ctx context.Context, params ListOperatorParams) ([]Entity, error)
	ListSchedules(ctx context.Context, params ListOperatorParams) ([]Schedule, error)
	ListCandidateEmbeddingsGemma2025(ctx context.Context, params ListOperatorParams) ([]EmbeddingRecord, error)
	ListContentEmbeddingsGemma2025(ctx context.Context, params ListOperatorParams) ([]EmbeddingRecord, error)
}

type Prompts interface {
	CreatePromptVersion(ctx context.Context, arg CreatePromptVersionParams) (PromptVersion, error)
	GetPromptVersionByID(ctx context.Context, id uuid.UUID) (PromptVersion, error)
	GetPromptVersionByNameAndVersion(ctx context.Context, name string, version int32) (PromptVersion, error)
	GetLatestPromptVersionByName(ctx context.Context, name string) (PromptVersion, error)
	ListPromptVersions(ctx context.Context, params ListOperatorParams) ([]PromptVersion, error)
	ListPromptVersionsByKey(ctx context.Context, key string, params ListOperatorParams) ([]PromptVersion, error)
}

type PlannerResults interface {
	PersistPlannerResult(ctx context.Context, arg PersistPlannerResultParams) (PlannerResult, error)
}

type Tokens interface {
	CreateToken(ctx context.Context, arg CreateTokenParams) (Token, error)
	GetRootToken(ctx context.Context) (Token, error)
	GetTokenByID(ctx context.Context, id uuid.UUID) (Token, error)
	ListTokens(ctx context.Context, params ListOperatorParams) ([]Token, error)
	RenewToken(ctx context.Context, id uuid.UUID, expiresAt time.Time) (Token, error)
	RotateToken(ctx context.Context, arg RotateTokenParams) (Token, error)
	RevokeToken(ctx context.Context, id uuid.UUID) (Token, error)
	RevokeAllTokens(ctx context.Context) (int64, error)
	CountActiveAdminTokensExcluding(ctx context.Context, id uuid.UUID) (int64, error)
}

type Pipeline interface {
	GetContentByID(ctx context.Context, id uuid.UUID) (Content, error)
	GetContentByURL(ctx context.Context, url string) (Content, error)
	GetContentByCandidateID(ctx context.Context, candidateID uuid.UUID) (Content, error)
	CreateContent(ctx context.Context, arg CreateContentParams) (Content, error)
	UpdateContentMetadata(ctx context.Context, arg UpdateContentMetadataParams) (Content, error)
	ListContentsByBatchID(ctx context.Context, batchID uuid.UUID) ([]Content, error)
	ListRecentSeedContents(ctx context.Context, limit int32) ([]Content, error)
	DeleteContent(ctx context.Context, id uuid.UUID) (Content, error)
	RestoreContent(ctx context.Context, id uuid.UUID) (Content, error)
}

type PipelineRuntime interface {
	GetPipelineBatch(ctx context.Context, batchID uuid.UUID) (Batch, error)
	ListPipelineInputCandidates(ctx context.Context, rootBatchID uuid.UUID) ([]PipelineInputCandidate, error)
	ListPipelineInputContents(ctx context.Context, rootBatchID uuid.UUID) ([]PipelineInputContent, error)
	InitializePipeline(ctx context.Context, arg InitializePipelineParams) error
	InitializePipelineStage(ctx context.Context, arg InitializePipelineStageParams) (uuid.UUID, error)
	CreatePipelineRoot(ctx context.Context, arg CreateTaskParams) (Task, error)
	FindFinishedBatches(ctx context.Context, limit int32) ([]Batch, error)
	FindFinishedRootBatches(ctx context.Context, limit int32) ([]Batch, error)
	SetNSubtasks(ctx context.Context, batchID uuid.UUID, count int32) (Batch, error)
	MarkBatchFinished(ctx context.Context, batchID uuid.UUID, succeeded bool, traceID string) (int64, error)
	MarkRootBatchFinished(ctx context.Context, batchID uuid.UUID, succeeded bool, traceID string) (int64, error)
	// FailPipelineTask records the task failure and converges its root state in
	// one transaction. terminal reports whether the task exhausted its retries.
	FailPipelineTask(ctx context.Context, taskID, rootBatchID uuid.UUID, retryMax int, reason string) (terminal bool, err error)
	ConvergePipelineFailure(ctx context.Context, taskID, rootBatchID uuid.UUID, reason string) error
	ListReadyPipelineBatches(ctx context.Context, limit int32) ([]Batch, error)
	MarkPipelinePublished(ctx context.Context, batchID uuid.UUID) error
	RecordPipelinePublishFailure(ctx context.Context, batchID uuid.UUID, message string) error
	CompleteAnalysisReport(ctx context.Context, arg CompleteAnalysisReportParams) (Report, error)
}

type BatchTrigger interface {
	ListPendingCompletionBatches(ctx context.Context, limit int32, sourceType string) ([]Batch, error)
	FindNewlyCompletedBatches(ctx context.Context, limit int32, sourceType string) ([]Batch, error)
	// MarkBatchCompleted is an optimistic claim. Returns rows-affected so
	// callers can distinguish the winning instance (1) from a loser racing
	// against another instance (0). Only the winner should publish.
	MarkBatchCompleted(ctx context.Context, batchID uuid.UUID, traceID string) (int64, error)
	ListReadyToPublishBatches(ctx context.Context, limit int32, sourceType string) ([]Batch, error)
	MarkBatchPublished(ctx context.Context, batchID uuid.UUID) error
	RecordBatchPublishFailure(ctx context.Context, batchID uuid.UUID, publishErr string) error
	ListTasksByBatchID(ctx context.Context, batchID uuid.UUID) ([]Task, error)
	CountCandidatesByBatchID(ctx context.Context, batchID uuid.UUID) (int64, error)
	ListContentsByBatchID(ctx context.Context, batchID uuid.UUID) ([]Content, error)
}

type Models interface {
	GetModelByID(ctx context.Context, id int16) (Model, error)
	GetModelByNameAndType(ctx context.Context, name string, modelType string) (Model, error)
	GetEmbedderByName(ctx context.Context, name string) (Model, error)
	GetExtractorByName(ctx context.Context, name string) (Model, error)
	GetAnalyzerByName(ctx context.Context, name string) (Model, error)
}

type Embeddings interface {
	GetCandidateEmbeddingInputHash(ctx context.Context, candidateID uuid.UUID, modelID int16, category string) (string, error)
	GetContentEmbeddingInputHash(ctx context.Context, contentID uuid.UUID, modelID int16) (string, error)
	UpsertCandidateEmbedding(ctx context.Context, arg CreateCandidateEmbeddingParams) (CandidateEmbedding, error)
	UpsertContentEmbedding(ctx context.Context, arg CreateContentEmbeddingParams) (ContentEmbedding, error)
}

// UserFetches is the user-facing observation layer for POST /page_fetch.
// See docs/plan/spec.md §6 for why this is parallel to BatchTrigger and not
// merged into it.
type UserFetches interface {
	Create(ctx context.Context, arg CreateUserFetchParams) (UserFetch, error)
	Get(ctx context.Context, id uuid.UUID) (UserFetch, error)
	CreateItem(ctx context.Context, arg CreateUserFetchItemParams) (UserFetchItem, error)
	GetProgress(ctx context.Context, fetchID uuid.UUID) (UserFetchProgress, error)
	// MarkCompleted is reserved for the v2 notification dispatcher.
	// v1 callers compute terminal on-the-fly from GetProgress and may
	// skip this entirely.
	MarkCompleted(ctx context.Context, fetchID uuid.UUID) error
}

type AnalysisRuns interface {
	Create(ctx context.Context, arg CreateAnalysisRunParams) (AnalysisRun, error)
	GetByID(ctx context.Context, id uuid.UUID) (AnalysisRun, error)
	GetByFetchID(ctx context.Context, fetchID uuid.UUID) (AnalysisRun, error)
	ListItems(ctx context.Context, fetchID uuid.UUID) ([]AnalysisRunItem, error)
	SetManifest(ctx context.Context, arg SetAnalysisRunManifestParams) (AnalysisRun, error)
	SetStatus(ctx context.Context, arg SetAnalysisRunStatusParams) (AnalysisRun, error)
	SetRoot(ctx context.Context, id, rootBatchID uuid.UUID) (AnalysisRun, error)
	SetExecution(ctx context.Context, id, executionID uuid.UUID) (AnalysisRun, error)
	CancelItems(ctx context.Context, fetchID uuid.UUID) error
}

type Reports interface {
	EnsureExecution(ctx context.Context, id uuid.UUID, fingerprint string) error
	GetByID(ctx context.Context, id uuid.UUID) (Report, error)
	GetByExecutionID(ctx context.Context, id uuid.UUID) (Report, error)
	FindCacheHit(ctx context.Context, id uuid.UUID) (Report, error)
	MarkMissing(ctx context.Context, id uuid.UUID, requestID *string) (bool, error)
	MarkCorrupt(ctx context.Context, arg MarkReportCorruptParams) (bool, error)
	BeginRemoval(ctx context.Context, arg BeginReportRemovalParams) (Report, error)
	RecordAuditEvent(ctx context.Context, arg ReportAuditEventParams) error
}

type Analysis interface {
	CreateContentExtraction(ctx context.Context, arg CreateContentExtractionParams) (ContentExtraction, error)
	GetContentExtractionByID(ctx context.Context, id uuid.UUID) (ContentExtraction, error)
	GetContentExtractionSnapshot(ctx context.Context, arg GetContentExtractionSnapshotParams) (ContentExtraction, error)
	UpsertEntity(ctx context.Context, arg UpsertEntityParams) (Entity, error)
	GetEntityByCanonicalAndType(ctx context.Context, canonical string, entityType string) (Entity, error)
	CreateContentExtractionEntity(ctx context.Context, arg CreateContentExtractionEntityParams) error
	ReplaceContentExtractionTopics(ctx context.Context, extractionID uuid.UUID, topics []string) error
	ReplaceContentExtractionPhrases(ctx context.Context, extractionID uuid.UUID, phrases []string) error
}
