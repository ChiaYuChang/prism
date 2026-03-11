package repo

import (
	"context"

	"github.com/ChiaYuChang/prism/internal/model"
	"github.com/google/uuid"
)

// SearchTaskRepository handles the lifecycle of discovery search tasks.
type SearchTaskRepository interface {
	// ClaimSearchTasks retrieves and locks a batch of pending tasks.
	ClaimSearchTasks(ctx context.Context, limit int32) ([]model.SearchTask, error)
	// CompleteSearchTask marks a task as COMPLETED and updates its next scheduled run time.
	CompleteSearchTask(ctx context.Context, id uuid.UUID) error
	// FailSearchTask marks a task as FAILED.
	FailSearchTask(ctx context.Context, id uuid.UUID) error
}

// DiscoveryRepository handles persistence tasks during the discovery phase.
type DiscoveryRepository interface {
	// IsURLProcessed checks if a specific URL already exists in the fingerprint database (Deduplication).
	IsURLProcessed(ctx context.Context, url string) (bool, error)
	// SaveActiveSearchTask saves search tasks (search_tasks) extracted by AI.
	SaveActiveSearchTask(ctx context.Context, task model.SearchTask) error
	// StoreDiscoveryResult stores initial information of discovered reports into the buffer (fingerprints).
	StoreDiscoveryResult(ctx context.Context, buffer model.ArticleFingerprint) error
}

// ContentRepository handles news content and media source entities.
type ContentRepository interface {
	// UpsertSource registers or updates media source information.
	UpsertSource(ctx context.Context, source model.Source) (int, error)
	// CreateContent saves parsed structured news content.
	CreateContent(ctx context.Context, article model.ArticleContent) (string, error)
	// GetContentByID retrieves specific content for analysis.
	GetContentByID(ctx context.Context, id string) (*model.ArticleContent, error)
}

// VectorRepository handles semantic vector search operations.
type VectorRepository interface {
	// SaveEmbedding stores vector representation of content.
	SaveEmbedding(ctx context.Context, contentID uuid.UUID, modelID int16, vector []float32) error
	// SearchSimilar performs cosine similarity search based on a vector.
	SearchSimilar(ctx context.Context, vector []float32, limit int) ([]model.ArticleContent, error)
}
