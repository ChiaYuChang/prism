package message

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	// TaskTopic is the NATS/Watermill topic for triggering runnable tasks.
	TaskTopic = "prism.task"
)

var ErrNilPublisher = errors.New("publisher is nil")

// TaskSignal represents one runnable task dispatched from the scheduler.
type TaskSignal struct {
	// TaskID corresponds to tasks.id in Postgres.
	TaskID uuid.UUID `json:"task_id"`

	// BatchID groups all tasks created by one cron- or trigger-generated batch.
	BatchID uuid.UUID `json:"batch_id"`

	// Kind identifies the executable task kind, e.g. DIRECTORY_FETCH or KEYWORD_SEARCH.
	Kind string `json:"kind"`

	// SourceType identifies the logical source family, e.g. PARTY or MEDIA.
	SourceType string `json:"source_type"`

	// SourceAbbr identifies the source by its stable abbreviation (sources.abbr PK).
	SourceAbbr string `json:"source_abbr"`

	// URL is the request target for the worker.
	URL string `json:"url"`

	// Payload stores provider- or source-specific request details.
	Payload json.RawMessage `json:"payload"`

	// Meta carries task-kind-specific context for logging and observability (e.g. candidate_id for PAGE_FETCH).
	Meta json.RawMessage `json:"meta,omitempty"`

	// TraceID propagates OpenTelemetry tracing across workers.
	TraceID string `json:"trace_id"`

	// SentAt provides a timestamp for tracking latency.
	SentAt time.Time `json:"sent_at"`
}

// Marshal converts the signal into JSON bytes.
func (s *TaskSignal) Marshal() ([]byte, error) {
	return json.Marshal(s)
}

// Unmarshal populates the signal from JSON bytes.
func (s *TaskSignal) Unmarshal(data []byte) error {
	return json.Unmarshal(data, s)
}
