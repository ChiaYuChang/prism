package repo

import (
	"time"

	"github.com/google/uuid"
)

type CreateCandidateParams struct {
	BatchID         uuid.UUID  `validate:"omitempty"`
	Fingerprint     string     `validate:"required"`
	SourceAbbr      string     `validate:"required"`
	Title           string     `validate:"required"`
	URL             string     `validate:"required,url"`
	Description     *string    `validate:"omitempty"`
	PublishedAt     *time.Time `validate:"omitempty"`
	TraceID         string     `validate:"required"`
	IngestionMethod string     `validate:"required"`
	Metadata        []byte     `validate:"omitempty"`
}

type UpsertCandidateParams = CreateCandidateParams

type CreateSourceParams struct {
	Abbr    string `validate:"required,max=16"`
	Name    string `validate:"required,max=128"`
	Type    string `validate:"required,oneof=PARTY MEDIA"`
	BaseURL string `validate:"required,url"`
}

type UpdateSourceParams = CreateSourceParams

type ListCandidatesParams struct {
	Query      *string    `validate:"omitempty"`
	SourceAbbr *string    `validate:"omitempty"`
	Since      *time.Time `validate:"omitempty"`
	Until      *time.Time `validate:"omitempty"`
	Limit      int32      `validate:"min=1,max=500"`
	Offset     int32      `validate:"min=0"`
}

type ListOperatorParams struct {
	Limit int32 `validate:"min=1,max=500"`
	Next  int32 `validate:"min=1"`
}

type CreateTaskParams struct {
	BatchID        uuid.UUID      `validate:"required"`
	ParentBatchID  *uuid.UUID     `validate:"omitempty"`
	ParentTaskID   *uuid.UUID     `validate:"omitempty"`
	PreviousTaskID *uuid.UUID     `validate:"omitempty"`
	NextTaskID     *uuid.UUID     `validate:"omitempty"`
	LogicalKey     *string        `validate:"omitempty"`
	Kind           string         `validate:"required"`
	SourceType     string         `validate:"required"`
	SourceAbbr     string         `validate:"required"`
	URL            string         `validate:"required,url"`
	Payload        []byte         `validate:"omitempty"`
	PayloadHash    *string        `validate:"omitempty,len=64"`
	Meta           []byte         `validate:"omitempty"`
	TraceID        string         `validate:"required"`
	Frequency      *time.Duration `validate:"omitempty"`
	NextRunAt      *time.Time     `validate:"omitempty"`
	ExpiresAt      *time.Time     `validate:"omitempty"`
}

type EnsureBatchParams struct {
	BatchID       uuid.UUID
	ParentBatchID *uuid.UUID
	ParentTaskID  *uuid.UUID
	SourceType    string
	TraceID       string
}

type ExtendActiveTaskExpiryParams struct {
	SourceAbbr  string     `validate:"required"`
	Kind        string     `validate:"required"`
	PayloadHash string     `validate:"required,len=64"`
	ExpiresAt   *time.Time `validate:"omitempty"`
}

type UpsertScheduleParams struct {
	ID          uuid.UUID     `validate:"required"`
	Name        string        `validate:"required"`
	Enabled     bool          `validate:"omitempty"`
	ConfigHash  string        `validate:"required,len=64"`
	Kind        string        `validate:"required"`
	SourceType  string        `validate:"required"`
	SourceAbbr  string        `validate:"required"`
	URL         string        `validate:"required,url"`
	Payload     []byte        `validate:"omitempty"`
	Meta        []byte        `validate:"omitempty"`
	Frequency   time.Duration `validate:"required"`
	RunOnInsert bool          `validate:"omitempty"`
}

type MaterializeDueSchedulesParams struct {
	Limit         int32  `validate:"required,min=1,max=200"`
	TraceIDPrefix string `validate:"required"`
}

type CreateContentParams struct {
	BatchID     uuid.UUID `validate:"omitempty"`
	Type        string    `validate:"required"`
	SourceAbbr  string    `validate:"required"`
	CandidateID uuid.UUID `validate:"omitempty"`
	URL         string    `validate:"required,url"`
	Title       string    `validate:"required"`
	Content     string    `validate:"required"`
	Author      *string   `validate:"omitempty"`
	TraceID     string    `validate:"required"`
	PublishedAt time.Time `validate:"required"`
	FetchedAt   time.Time `validate:"required"`
	Metadata    []byte    `validate:"omitempty"`
}

type UpdateContentMetadataParams struct {
	ID          uuid.UUID  `validate:"required"`
	Author      *string    `validate:"omitempty"`
	PublishedAt *time.Time `validate:"omitempty"`
	Metadata    []byte     `validate:"omitempty"`
}

type CreateModelParams struct {
	Name        string     `validate:"required,max=64"`
	Provider    string     `validate:"required,max=32"`
	Type        string     `validate:"required,oneof=EXTRACTOR EMBEDDER ANALYZER"`
	PublishDate *time.Time `validate:"omitempty"`
	URL         *string    `validate:"omitempty,url"`
	Tag         *string    `validate:"omitempty,max=32"`
}

type CreatePromptVersionParams struct {
	Name      string `validate:"required"`
	Hash      string `validate:"required"`
	SizeBytes int64  `validate:"required,min=0"`
}

type CreateTokenParams struct {
	ID            uuid.UUID `validate:"required"`
	Type          string    `validate:"required"`
	Name          string    `validate:"required"`
	Permissions   uint8     `validate:"max=255"`
	HashAlgorithm string    `validate:"required"`
	TokenHash     string    `validate:"required"`
	ExpiresAt     time.Time `validate:"required"`
}

type RootAuthParams struct {
	HashAlgorithm string `validate:"required"`
	TokenHash     string `validate:"required"`
}

type CreateRootControlParams struct {
	ID            uuid.UUID `validate:"required"`
	Name          string    `validate:"required"`
	HashAlgorithm string    `validate:"required"`
	TokenHash     string    `validate:"required"`
	ExpiresAt     time.Time `validate:"required"`
}

type CreateRootAdminParams struct {
	RootAuthParams
	ID            uuid.UUID `validate:"required"`
	Name          string    `validate:"required"`
	HashAlgorithm string    `validate:"required"`
	TokenHash     string    `validate:"required"`
	ExpiresAt     time.Time `validate:"required"`
}

type RotateTokenParams struct {
	ID            uuid.UUID `validate:"required"`
	HashAlgorithm string    `validate:"required"`
	TokenHash     string    `validate:"required"`
	ExpiresAt     time.Time `validate:"required"`
}

type CreateCandidateEmbeddingParams struct {
	CandidateID uuid.UUID `validate:"required"`
	ModelID     int16     `validate:"required"`
	Category    string    `validate:"required"`
	InputHash   string    `validate:"required,len=64"`
	Vector      []float32 `validate:"required,min=1"`
	TraceID     string    `validate:"required"`
}

type CreateContentEmbeddingParams struct {
	ContentID uuid.UUID `validate:"required"`
	ModelID   int16     `validate:"required"`
	InputHash string    `validate:"required,len=64"`
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

type PlannerExtractionParams struct {
	ContentID uuid.UUID
	Title     string
	Summary   string
	RawResult []byte
	Topics    []string
	Phrases   []string
	Entities  []PlannerEntityParams
}

type PlannerEntityParams struct {
	Canonical string
	Type      string
	Surface   string
	Ordinal   *int16
}

type PersistPlannerResultParams struct {
	BatchID       uuid.UUID
	TraceID       string
	ModelID       int16
	PromptID      uuid.UUID
	SchemaName    string
	SchemaVersion int32
	Extractions   []PlannerExtractionParams
	Tasks         []CreateTaskParams
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

type CreateUserFetchParams struct {
	UserID *uuid.UUID `validate:"omitempty"`
}

type CreateUserFetchItemParams struct {
	FetchID        uuid.UUID  `validate:"required"`
	CandidateID    uuid.UUID  `validate:"required"`
	TaskID         *uuid.UUID `validate:"omitempty"`
	SnapshotStatus *string    `validate:"omitempty"`
}
