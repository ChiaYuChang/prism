package pg

import (
	"context"

	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/ChiaYuChang/prism/pkg/pgconv"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	pgvector "github.com/pgvector/pgvector-go"
)

// Root repository constructor.
func NewPostgresRepository(db DBTX) *PGRepository {
	return &PGRepository{q: New(db)}
}

// Repository roots.
type PGRepository struct {
	q *Queries
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

// Scheduler repository.
func (r *PGScheduler) ClaimTasks(ctx context.Context, limit int32) ([]repo.Task, error) {
	rows, err := r.q.ClaimTasks(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]repo.Task, len(rows))
	for i, row := range rows {
		out[i] = dbTaskToRepoTask(row)
	}
	return out, nil
}

func (r *PGScheduler) CompleteTask(ctx context.Context, id uuid.UUID) error {
	return r.q.CompleteTask(ctx, id)
}

func (r *PGScheduler) FailTask(ctx context.Context, id uuid.UUID) error {
	return r.q.FailTask(ctx, id)
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
func (r *PGScout) GetSourceByID(ctx context.Context, id int32) (repo.Source, error) {
	row, err := r.q.GetSourceByID(ctx, id)
	if err != nil {
		return repo.Source{}, err
	}
	return dbSourceToRepoSource(row), nil
}

func (r *PGScout) GetCandidateByFingerprint(ctx context.Context, fingerprint string) (repo.Candidate, error) {
	row, err := r.q.GetCandidateByFingerprint(ctx, fingerprint)
	if err != nil {
		return repo.Candidate{}, err
	}
	return dbCandidateToRepoCandidate(row), nil
}

func (r *PGScout) CreateCandidate(ctx context.Context, arg repo.CreateCandidateParams) (repo.Candidate, error) {
	row, err := r.q.CreateCandidate(ctx, CreateCandidateParams{
		BatchID:         pgconv.UUIDPtrToPgUUID(arg.BatchID),
		Fingerprint:     arg.Fingerprint,
		SourceID:        arg.SourceID,
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
		BatchID:         pgconv.UUIDPtrToPgUUID(arg.BatchID),
		Fingerprint:     arg.Fingerprint,
		SourceID:        arg.SourceID,
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

func (r *PGTasks) CreateTask(ctx context.Context, arg repo.CreateTaskParams) (repo.Task, error) {
	row, err := r.q.CreateTask(ctx, CreateTaskParams{
		BatchID:    arg.BatchID,
		Kind:       TaskKind(arg.Kind),
		SourceType: SourceType(arg.SourceType),
		SourceID:   arg.SourceID,
		Url:        arg.URL,
		Payload:    arg.Payload,
		TraceID:    arg.TraceID,
		Frequency:  pgconv.DurationPtrToPgInterval(arg.Frequency),
		NextRunAt:  pgconv.TimePtrToPgTimestamptz(arg.NextRunAt),
		ExpiresAt:  pgconv.TimePtrToPgTimestamptz(arg.ExpiresAt),
	})
	if err != nil {
		return repo.Task{}, err
	}
	return dbTaskToRepoTask(row), nil
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
	row, err := r.q.CreateContent(ctx, CreateContentParams{
		BatchID:     pgconv.UUIDPtrToPgUUID(arg.BatchID),
		Type:        ContentType(arg.Type),
		SourceID:    arg.SourceID,
		CandidateID: pgconv.UUIDPtrToPgUUID(arg.CandidateID),
		Url:         arg.URL,
		Title:       arg.Title,
		Content:     arg.Content,
		Author:      pgconv.StringPtrToPgText(arg.Author),
		TraceID:     arg.TraceID,
		PublishedAt: pgconv.TimePtrToPgTimestamptz(&arg.PublishedAt),
		FetchedAt:   pgconv.TimePtrToPgTimestamptz(&arg.FetchedAt),
		Metadata:    arg.Metadata,
	})
	if err != nil {
		return repo.Content{}, err
	}
	return dbContentToRepoContent(row), nil
}

func (r *PGPipeline) UpdateContentMetadata(ctx context.Context, arg repo.UpdateContentMetadataParams) (repo.Content, error) {
	row, err := r.q.UpdateContentMetadata(ctx, UpdateContentMetadataParams{
		Author:      pgconv.StringPtrToPgText(arg.Author),
		PublishedAt: pgconv.TimePtrToPgTimestamptz(arg.PublishedAt),
		Metadata:    arg.Metadata,
		ID:          arg.ID,
	})
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

// Analysis repository.
func (r *PGAnalysis) GetPromptByID(ctx context.Context, id uuid.UUID) (repo.Prompt, error) {
	row, err := r.q.GetPromptByID(ctx, id)
	if err != nil {
		return repo.Prompt{}, err
	}
	return dbPromptToRepoPrompt(row), nil
}

func (r *PGAnalysis) GetPromptByHash(ctx context.Context, hash string) (repo.Prompt, error) {
	row, err := r.q.GetPromptByHash(ctx, hash)
	if err != nil {
		return repo.Prompt{}, err
	}
	return dbPromptToRepoPrompt(row), nil
}

func (r *PGAnalysis) UpsertPrompt(ctx context.Context, arg repo.UpsertPromptParams) (repo.Prompt, error) {
	row, err := r.q.UpsertPrompt(ctx, UpsertPromptParams{Hash: arg.Hash, Path: arg.Path})
	if err != nil {
		return repo.Prompt{}, err
	}
	return dbPromptToRepoPrompt(row), nil
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
