package repo

import (
	"context"

	"github.com/google/uuid"
)

type Repository interface {
	Scheduler() Scheduler
	Scout() Scout
	Tasks() Tasks
	Pipeline() Pipeline
	Embedding() Embeddings
	Analysis() Analysis
	BatchTrigger() BatchTrigger
}

type Scheduler interface {
	// ClaimTasks claims up to limit runnable tasks of the given kinds.
	// sourceTypes filters by source_type; an empty slice matches all source types.
	ClaimTasks(ctx context.Context, limit int32, kinds []string, sourceTypes []string) ([]Task, error)
	CompleteTask(ctx context.Context, id uuid.UUID) error
	FailTask(ctx context.Context, id uuid.UUID) error
	// ReleaseTasks resets RUNNING tasks back to PENDING, undoing the retry_count
	// increment from ClaimTasks. Used when dispatch is skipped (e.g. rate-limited).
	ReleaseTasks(ctx context.Context, ids []uuid.UUID) error
	ListRunnableTasks(ctx context.Context, limit int32) ([]Task, error)
}

type Scout interface {
	GetSourceByAbbr(ctx context.Context, abbr string) (Source, error)
	ListSourcesByType(ctx context.Context, sourceType string) ([]Source, error)
	GetCandidateByFingerprint(ctx context.Context, fingerprint string) (Candidate, error)
	CountCandidatesByBatchID(ctx context.Context, batchID uuid.UUID) (int64, error)
	CreateCandidate(ctx context.Context, arg CreateCandidateParams) (Candidate, error)
	UpsertCandidate(ctx context.Context, arg UpsertCandidateParams) (Candidate, error)
}

type Tasks interface {
	GetTaskByID(ctx context.Context, id uuid.UUID) (Task, error)
	ListTasksByBatchID(ctx context.Context, batchID uuid.UUID) ([]Task, error)
	CreateTask(ctx context.Context, arg CreateTaskParams) (Task, error)
	ExtendActiveTaskExpiry(ctx context.Context, arg ExtendActiveTaskExpiryParams) error
}

type Pipeline interface {
	GetContentByID(ctx context.Context, id uuid.UUID) (Content, error)
	GetContentByURL(ctx context.Context, url string) (Content, error)
	GetContentByCandidateID(ctx context.Context, candidateID uuid.UUID) (Content, error)
	CreateContent(ctx context.Context, arg CreateContentParams) (Content, error)
	UpdateContentMetadata(ctx context.Context, arg UpdateContentMetadataParams) (Content, error)
	ListContentsByBatchID(ctx context.Context, batchID uuid.UUID) ([]Content, error)
	ListRecentSeedContents(ctx context.Context, limit int32) ([]Content, error)
}

type BatchTrigger interface {
	ListPendingCompletionBatches(ctx context.Context, limit int32, sourceType string) ([]Batch, error)
	FindNewlyCompletedBatches(ctx context.Context, limit int32, sourceType string) ([]Batch, error)
	MarkBatchCompleted(ctx context.Context, batchID uuid.UUID, traceID string) error
	ListReadyToPublishBatches(ctx context.Context, limit int32, sourceType string) ([]Batch, error)
	MarkBatchPublished(ctx context.Context, batchID uuid.UUID) error
	RecordBatchPublishFailure(ctx context.Context, batchID uuid.UUID, publishErr string) error
	ListTasksByBatchID(ctx context.Context, batchID uuid.UUID) ([]Task, error)
	CountCandidatesByBatchID(ctx context.Context, batchID uuid.UUID) (int64, error)
	ListContentsByBatchID(ctx context.Context, batchID uuid.UUID) ([]Content, error)
}

type Embeddings interface {
	GetModelByID(ctx context.Context, id int16) (Model, error)
	GetModelByNameAndType(ctx context.Context, name string, modelType string) (Model, error)
	CreateCandidateEmbedding(ctx context.Context, arg CreateCandidateEmbeddingParams) (CandidateEmbedding, error)
	CreateContentEmbedding(ctx context.Context, arg CreateContentEmbeddingParams) (ContentEmbedding, error)
}

type Analysis interface {
	GetPromptByID(ctx context.Context, id uuid.UUID) (Prompt, error)
	GetPromptByHash(ctx context.Context, hash string) (Prompt, error)
	UpsertPrompt(ctx context.Context, arg UpsertPromptParams) (Prompt, error)
	CreateContentExtraction(ctx context.Context, arg CreateContentExtractionParams) (ContentExtraction, error)
	GetContentExtractionByID(ctx context.Context, id uuid.UUID) (ContentExtraction, error)
	GetContentExtractionSnapshot(ctx context.Context, arg GetContentExtractionSnapshotParams) (ContentExtraction, error)
	UpsertEntity(ctx context.Context, arg UpsertEntityParams) (Entity, error)
	GetEntityByCanonicalAndType(ctx context.Context, canonical string, entityType string) (Entity, error)
	CreateContentExtractionEntity(ctx context.Context, arg CreateContentExtractionEntityParams) error
	ReplaceContentExtractionTopics(ctx context.Context, extractionID uuid.UUID, topics []string) error
	ReplaceContentExtractionPhrases(ctx context.Context, extractionID uuid.UUID, phrases []string) error
}
