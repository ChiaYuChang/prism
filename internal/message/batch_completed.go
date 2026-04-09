package message

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	wm "github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
)

const (
	BatchCompletedTopic = "prism.batch.completed"
)

type BatchCompletedSignal struct {
	BatchID    uuid.UUID `json:"batch_id"`
	SourceType string    `json:"source_type"`
	TraceID    string    `json:"trace_id"`
	SentAt     time.Time `json:"sent_at"`
}

func (s *BatchCompletedSignal) Marshal() ([]byte, error) {
	return json.Marshal(s)
}

func (s *BatchCompletedSignal) Unmarshal(data []byte) error {
	return json.Unmarshal(data, s)
}

type BatchCompletedPublisher interface {
	PublishBatchCompleted(ctx context.Context, sig *BatchCompletedSignal) error
}

type WatermillBatchCompletedPublisher struct {
	publisher wm.Publisher
}

func NewWatermillBatchCompletedPublisher(publisher wm.Publisher) (*WatermillBatchCompletedPublisher, error) {
	if publisher == nil {
		return nil, ErrNilPublisher
	}
	return &WatermillBatchCompletedPublisher{publisher: publisher}, nil
}

func (p *WatermillBatchCompletedPublisher) PublishBatchCompleted(ctx context.Context, sig *BatchCompletedSignal) error {
	payload, err := sig.Marshal()
	if err != nil {
		return fmt.Errorf("marshal batch completed signal: %w", err)
	}

	msgID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generate batch completed message id: %w", err)
	}

	msg := wm.NewMessage(msgID.String(), payload)
	msg.Metadata.Set("trace_id", sig.TraceID)
	if err := p.publisher.Publish(BatchCompletedTopic, msg); err != nil {
		return fmt.Errorf("publish batch completed signal: %w", err)
	}
	return nil
}
