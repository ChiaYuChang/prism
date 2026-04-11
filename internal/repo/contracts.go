package repo

import (
	"time"

	"github.com/google/uuid"
)

type Task struct {
	ID         uuid.UUID
	BatchID    uuid.UUID
	TraceID    string
	Kind       string
	SourceType string
	SourceID   int32
	URL         string
	Payload     []byte
	PayloadHash *string
	Frequency   *time.Duration
	NextRunAt  time.Time
	ExpiresAt  *time.Time
	Status     string
	RetryCount int
	LastRunAt  *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type Source struct {
	ID        int32
	Abbr      string
	Name      string
	Type      string
	BaseURL   string
	CreatedAt time.Time
	DeletedAt *time.Time
}

type Model struct {
	ID          int16
	Name        string
	Provider    string
	Type        string
	PublishDate *time.Time
	URL         *string
	Tag         *string
	CreatedAt   time.Time
	DeletedAt   *time.Time
}

type Prompt struct {
	ID        uuid.UUID
	Hash      string
	Path      string
	CreatedAt time.Time
}

type Candidate struct {
	ID              uuid.UUID
	BatchID         uuid.UUID
	Fingerprint     string
	SourceID        int32
	Title           string
	URL             string
	Description     *string
	PublishedAt     *time.Time
	DiscoveredAt    time.Time
	TraceID         string
	IngestionMethod string
	Metadata        []byte
	CreatedAt       time.Time
}

type Content struct {
	ID          uuid.UUID
	BatchID     uuid.UUID
	Type        string
	SourceID    int32
	CandidateID uuid.UUID
	URL         string
	Title       string
	Content     string
	Author      *string
	TraceID     string
	PublishedAt time.Time
	FetchedAt   time.Time
	CreatedAt   time.Time
	DeletedAt   *time.Time
	Metadata    []byte
}

type CandidateEmbedding struct {
	ID          int64
	CandidateID uuid.UUID
	ModelID     int16
	Category    string
	TraceID     string
	CreatedAt   time.Time
}

type ContentEmbedding struct {
	ID        int64
	ContentID uuid.UUID
	ModelID   int16
	Category  string
	TraceID   string
	CreatedAt time.Time
}

type ContentExtraction struct {
	ID            uuid.UUID
	ContentID     uuid.UUID
	ModelID       int16
	PromptID      uuid.UUID
	SchemaName    string
	SchemaVersion int32
	Title         string
	Summary       string
	RawResult     []byte
	TraceID       string
	CreatedAt     time.Time
}

type Entity struct {
	ID        int32
	Canonical string
	Type      string
	CreatedAt time.Time
}
