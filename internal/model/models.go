package model

import (
	"crypto/md5"
	"encoding/base64"
	"time"

	"github.com/google/uuid"
)

// TaskStatus represents the lifecycle state of a search task.
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "PENDING"
	TaskStatusRunning   TaskStatus = "RUNNING"
	TaskStatusFailed    TaskStatus = "FAILED"
	TaskStatusCompleted TaskStatus = "COMPLETED"
)

// Task represents a task in the discovery pipeline.
type Task struct {
	ID         uuid.UUID     `json:"id"`
	ContentID  uuid.UUID     `json:"content_id"`
	Phrases    []string      `json:"phrases"`
	TraceID    string        `json:"trace_id"`
	Status     TaskStatus    `json:"status"`
	RetryCount int           `json:"retry_count"`
	Frequency  time.Duration `json:"frequency"`
	NextRunAt  time.Time     `json:"next_run_at"`
	LastRunAt  time.Time     `json:"last_run_at"`
}

// Candidates is a temporary buffer for storing discovered articles before parsing.
type Candidates struct {
	BatchID         uuid.UUID      `json:"batch_id"`
	SourceID        int            `json:"source_id"`        // Source id
	TraceID         string         `json:"trace_id"`         // Trace ID of the article
	URL             string         `json:"url"`              // URL of the article
	Title           string         `json:"title"`            // Title of the article
	Description     string         `json:"description"`      // Description of the article
	IngestionMethod string         `json:"ingestion_method"` // DIRECTORY, SEARCH, SUBSCRIPTION, MANUAL
	PublishedAt     time.Time      `json:"published_at"`     // Published at
	DiscoveredAt    time.Time      `json:"discovered_at"`    // Discovered at
	Metadata        map[string]any `json:"metadata"`
}

// Fingerprint returns a unique identifier for the candidate based on its URL, title, and published time.
func (c Candidates) Fingerprint() string {
	hasher := md5.New()
	hasher.Write([]byte(c.URL))
	hasher.Write([]byte(c.Title))
	hasher.Write([]byte(c.PublishedAt.UTC().Format(time.DateTime)))
	return base64.StdEncoding.EncodeToString(hasher.Sum(nil))
}

// ArticleContent represents a standardized news article object after parsing.
type ArticleContent struct {
	ID            uuid.UUID      `json:"id"`
	Type          string         `json:"type"`
	SourceID      int            `json:"source_id"`
	FingerprintID int            `json:"fingerprint_id"`
	URL           string         `json:"url"`
	Title         string         `json:"title"`
	Content       string         `json:"content"`
	Author        string         `json:"author"`
	TraceID       string         `json:"trace_id"`
	PublishedAt   time.Time      `json:"published_at"`
	FetchedAt     time.Time      `json:"fetched_at"`
	Metadata      map[string]any `json:"metadata"`
}

// ArchiveRecord is a container for raw data stored in object storage (e.g., S3).
type ArchiveRecord struct {
	Fingerprint   string         `json:"fingerprint"`
	FingerprintID int            `json:"fingerprint_id"`
	URL           string         `json:"url"`
	Payload       string         `json:"payload"` // Gzip + Base64 encoded string
	TraceID       string         `json:"trace_id"`
	Timestamp     time.Time      `json:"timestamp"`
	Metadata      map[string]any `json:"metadata"`
}

// Source represents a media entity (e.g., PTS, Liberty Times).
type Source struct {
	ID      int32  `json:"id"`
	Abbr    string `json:"abbr"`
	Name    string `json:"name"`
	Type    string `json:"type"` // e.g., "MEDIA", "PARTY"
	BaseURL string `json:"base_url"`
}
