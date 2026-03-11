package message

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
    // SearchTaskTopic is the NATS/Watermill topic for triggering search tasks.
    SearchTaskTopic = "prism.search.task"
)

// SearchTaskSignal represents a signal to trigger a keyword search across media.
type SearchTaskSignal struct {
	// TaskID corresponds to search_tasks.id in Postgres
	TaskID uuid.UUID `json:"task_id"`
	
	// ContentID links to the content that triggered this search
	ContentID uuid.UUID `json:"content_id"`
	
	// Phrases contains the search strings to be executed (Composite Phrases)
	Phrases []string `json:"phrases"`
	
	// TraceID propagates OpenTelemetry tracing across workers
	TraceID string `json:"trace_id"`
	
	// SentAt provides a timestamp for tracking latency
	SentAt time.Time `json:"sent_at"`
}

// Marshal converts the signal into JSON bytes.
func (s *SearchTaskSignal) Marshal() ([]byte, error) {
	return json.Marshal(s)
}

// Unmarshal populates the signal from JSON bytes.
func (s *SearchTaskSignal) Unmarshal(data []byte) error {
	return json.Unmarshal(data, s)
}
