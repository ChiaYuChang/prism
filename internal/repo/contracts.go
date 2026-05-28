package repo

import (
	"time"

	"github.com/google/uuid"
)

type Task struct {
	ID          uuid.UUID
	BatchID     uuid.UUID
	TraceID     string
	Kind        string
	SourceType  string
	SourceAbbr  string
	URL         string
	Payload     []byte
	PayloadHash *string
	Meta        []byte
	Frequency   *time.Duration
	NextRunAt   time.Time
	ExpiresAt   *time.Time
	Status      TaskStatus
	RetryCount  int
	LastRunAt   *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Batch struct {
	ID                   uuid.UUID
	SourceType           string
	TraceID              *string
	CreatedAt            time.Time
	UpdatedAt            time.Time
	CompletedAt          *time.Time
	PublishedAt          *time.Time
	LastPublishAttemptAt *time.Time
	PublishRetryCount    int
	PublishError         *string
	StalledAt            *time.Time
}

type Source struct {
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
	SourceAbbr      string
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
	SourceAbbr  string
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

type UserFetch struct {
	ID          uuid.UUID
	UserID      *uuid.UUID
	CreatedAt   time.Time
	CompletedAt *time.Time
}

type UserFetchItem struct {
	FetchID        uuid.UUID
	CandidateID    uuid.UUID
	TaskID         *uuid.UUID
	SnapshotStatus *string
	CreatedAt      time.Time
}

// UserFetchProgress is the aggregator output for GET /api/v1/fetches/{id}.
// Candidate ID groups use COALESCE(snapshot_status, tasks.status) over items.
type UserFetchProgress struct {
	Total                       int64
	PendingCandidateIDs         []uuid.UUID
	RunningCandidateIDs         []uuid.UUID
	CompletedCandidateIDs       []uuid.UUID
	FailedCandidateIDs          []uuid.UUID
	AlreadyCompleteCandidateIDs []uuid.UUID
	Terminal                    bool
}

// UserFetchItemSnapshotAlreadyComplete is the only snapshot value used in v1.
// Items in this state were promoted to contents before the request was
// created, so they do not reference an active task.
const UserFetchItemSnapshotAlreadyComplete = "ALREADY_COMPLETE"
