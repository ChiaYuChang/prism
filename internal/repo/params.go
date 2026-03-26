package repo

import (
	"time"

	"github.com/google/uuid"
)

type CreateCandidateParams struct {
	BatchID         *uuid.UUID `validate:"omitempty"`
	Fingerprint     string     `validate:"required"`
	SourceID        int32      `validate:"required"`
	Title           string     `validate:"required"`
	URL             string     `validate:"required,url"`
	Description     *string    `validate:"omitempty"`
	PublishedAt     *time.Time `validate:"omitempty"`
	TraceID         string     `validate:"required"`
	IngestionMethod string     `validate:"required"`
	Metadata        []byte     `validate:"omitempty"`
}

type UpsertCandidateParams = CreateCandidateParams

type CreateTaskParams struct {
	BatchID    uuid.UUID      `validate:"required"`
	Kind       string         `validate:"required"`
	SourceType string         `validate:"required"`
	SourceID   int32          `validate:"required"`
	URL        string         `validate:"required,url"`
	Payload    []byte         `validate:"omitempty"`
	TraceID    string         `validate:"required"`
	Frequency  *time.Duration `validate:"omitempty"`
	NextRunAt  *time.Time     `validate:"omitempty"`
	ExpiresAt  *time.Time     `validate:"omitempty"`
}

type CreateContentParams struct {
	BatchID     *uuid.UUID `validate:"omitempty"`
	Type        string     `validate:"required"`
	SourceID    int32      `validate:"required"`
	CandidateID *uuid.UUID `validate:"omitempty"`
	URL         string     `validate:"required,url"`
	Title       string     `validate:"required"`
	Content     string     `validate:"required"`
	Author      *string    `validate:"omitempty"`
	TraceID     string     `validate:"required"`
	PublishedAt time.Time  `validate:"required"`
	FetchedAt   time.Time  `validate:"required"`
	Metadata    []byte     `validate:"omitempty"`
}

type UpdateContentMetadataParams struct {
	ID          uuid.UUID  `validate:"required"`
	Author      *string    `validate:"omitempty"`
	PublishedAt *time.Time `validate:"omitempty"`
	Metadata    []byte     `validate:"omitempty"`
}

type UpsertPromptParams struct {
	Hash string `validate:"required"`
	Path string `validate:"required"`
}

type CreateCandidateEmbeddingParams struct {
	CandidateID uuid.UUID `validate:"required"`
	ModelID     int16     `validate:"required"`
	Category    string    `validate:"required"`
	Vector      []float32 `validate:"required,min=1"`
	TraceID     string    `validate:"required"`
}

type CreateContentEmbeddingParams struct {
	ContentID uuid.UUID `validate:"required"`
	ModelID   int16     `validate:"required"`
	Category  string    `validate:"required"`
	Vector    []float32 `validate:"required,min=1"`
	TraceID   string    `validate:"required"`
}

type CreateContentExtractionParams struct {
	ContentID     uuid.UUID `validate:"required"`
	ModelID       int16     `validate:"required"`
	PromptID      uuid.UUID `validate:"required"`
	SchemaName    string    `validate:"required"`
	SchemaVersion int32     `validate:"required"`
	Title         string    `validate:"required"`
	Summary       string    `validate:"required"`
	RawResult     []byte    `validate:"required"`
	TraceID       string    `validate:"required"`
}

type GetContentExtractionSnapshotParams struct {
	ContentID     uuid.UUID `validate:"required"`
	ModelID       int16     `validate:"required"`
	PromptID      uuid.UUID `validate:"required"`
	SchemaVersion int32     `validate:"required"`
}

type UpsertEntityParams struct {
	Canonical string `validate:"required"`
	Type      string `validate:"required"`
}

type CreateContentExtractionEntityParams struct {
	ExtractionID uuid.UUID `validate:"required"`
	EntityID     int32     `validate:"required"`
	Surface      string    `validate:"required"`
	Ordinal      *int16    `validate:"omitempty"`
}
