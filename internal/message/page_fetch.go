package message

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	wm "github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
)

const (
	PageFetchTopic = "prism.page_fetch"
)

var (
	ErrNilPublisher = errors.New("publisher is nil")
)

type PageFetchSignal struct {
	CandidateID uuid.UUID  `json:"candidate_id"`
	BatchID     uuid.UUID  `json:"batch_id,omitempty"`
	SourceID    int32      `json:"source_id"`
	SourceType  string     `json:"source_type"`
	URL         string     `json:"url"`
	TraceID     string     `json:"trace_id"`
	SentAt      time.Time  `json:"sent_at"`
}

type PageFetchPublisher interface {
	PublishPageFetch(ctx context.Context, sig *PageFetchSignal) error
}

type WatermillPageFetchPublisher struct {
	publisher wm.Publisher
}

func NewWatermillPageFetchPublisher(publisher wm.Publisher) (*WatermillPageFetchPublisher, error) {
	if publisher == nil {
		return nil, ErrNilPublisher
	}
	return &WatermillPageFetchPublisher{publisher: publisher}, nil
}

func (p *WatermillPageFetchPublisher) PublishPageFetch(ctx context.Context, sig *PageFetchSignal) error {
	payload, err := json.Marshal(sig)
	if err != nil {
		return fmt.Errorf("marshal page fetch signal: %w", err)
	}

	msgID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generate page fetch message id: %w", err)
	}

	msg := wm.NewMessage(msgID.String(), payload)
	msg.Metadata.Set("trace_id", sig.TraceID)

	if err := p.publisher.Publish(PageFetchTopic, msg); err != nil {
		return fmt.Errorf("publish page fetch signal: %w", err)
	}
	return nil
}
