package message

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	wm "github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
)

const PipelineBatchFinishedTopic = "prism_pipeline_batch_finished"

// PipelineBatchFinishedSignal is the post-commit notification for a pipeline child batch.
type PipelineBatchFinishedSignal struct {
	BatchID     uuid.UUID `json:"batch_id"`
	RootBatchID uuid.UUID `json:"root_batch_id"`
	OwnerTaskID uuid.UUID `json:"owner_task_id"`
	Succeeded   bool      `json:"succeeded"`
	TraceID     string    `json:"trace_id"`
	SentAt      time.Time `json:"sent_at"`
}

func (s *PipelineBatchFinishedSignal) Marshal() ([]byte, error) { return json.Marshal(s) }

func (s *PipelineBatchFinishedSignal) Unmarshal(data []byte) error { return json.Unmarshal(data, s) }

type PipelineBatchFinishedPublisher interface {
	PublishPipelineBatchFinished(context.Context, *PipelineBatchFinishedSignal) error
}

type WatermillPipelineBatchFinishedPublisher struct{ publisher wm.Publisher }

func NewWatermillPipelineBatchFinishedPublisher(publisher wm.Publisher) (*WatermillPipelineBatchFinishedPublisher, error) {
	if publisher == nil {
		return nil, ErrNilPublisher
	}
	return &WatermillPipelineBatchFinishedPublisher{publisher: publisher}, nil
}

func (p *WatermillPipelineBatchFinishedPublisher) PublishPipelineBatchFinished(ctx context.Context, sig *PipelineBatchFinishedSignal) error {
	payload, err := sig.Marshal()
	if err != nil {
		return fmt.Errorf("marshal pipeline batch finished signal: %w", err)
	}
	msgID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generate pipeline batch finished message id: %w", err)
	}
	msg := wm.NewMessage(msgID.String(), payload)
	msg.Metadata.Set("trace_id", sig.TraceID)
	if err := InjectTraceContext(ctx, msg); err != nil {
		return fmt.Errorf("inject pipeline batch finished trace context: %w", err)
	}
	if err := p.publisher.Publish(PipelineBatchFinishedTopic, msg); err != nil {
		return fmt.Errorf("publish pipeline batch finished signal: %w", err)
	}
	return nil
}
