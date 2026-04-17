package batch

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/repo"
	"go.opentelemetry.io/otel/trace"
)

type Publisher struct {
	logger    *slog.Logger
	tracer    trace.Tracer
	repo      repo.BatchTrigger
	publisher message.BatchCompletedPublisher
}

func NewPublisher(
	logger *slog.Logger,
	tracer trace.Tracer,
	r repo.BatchTrigger,
	p message.BatchCompletedPublisher,
) (*Publisher, error) {
	if logger == nil {
		return nil, fmt.Errorf("%w: logger", ErrParamMissing)
	}
	if tracer == nil {
		return nil, fmt.Errorf("%w: tracer", ErrParamMissing)
	}
	if r == nil {
		return nil, fmt.Errorf("%w: batch_trigger_repository", ErrParamMissing)
	}
	if p == nil {
		return nil, fmt.Errorf("%w: batch_completed_publisher", ErrParamMissing)
	}
	return &Publisher{
		logger:    logger,
		tracer:    tracer,
		repo:      r,
		publisher: p,
	}, nil
}

// Publish scans for completed but unpublished batches and sends them to the MQ.
func (p *Publisher) Publish(ctx context.Context, limit int32) (int, error) {
	ctx, span := p.tracer.Start(ctx, "batch.publisher.publish")
	defer span.End()

	batches, err := p.repo.ListReadyToPublishBatches(ctx, limit, repo.SourceTypeParty)
	if err != nil {
		return 0, fmt.Errorf("list ready to publish batches: %w", err)
	}

	publishedCount := 0
	for _, batch := range batches {
		traceID := ""
		if batch.TraceID != nil {
			traceID = *batch.TraceID
		}

		sig := &message.BatchCompletedSignal{
			BatchID:    batch.ID,
			SourceType: batch.SourceType,
			TraceID:    traceID,
			SentAt:     time.Now(),
		}

		if err := p.publisher.PublishBatchCompleted(ctx, sig); err != nil {
			p.logger.ErrorContext(ctx, "failed to publish batch completed signal",
				slog.String("batch_id", batch.ID.String()),
				slog.Any("error", err),
			)
			if recErr := p.repo.RecordBatchPublishFailure(ctx, batch.ID, err.Error()); recErr != nil {
				return publishedCount, fmt.Errorf("publish batch %s signal: %w; record failure: %w", batch.ID, err, recErr)
			}
			continue
		}

		if err := p.repo.MarkBatchPublished(ctx, batch.ID); err != nil {
			return publishedCount, fmt.Errorf("mark batch %s as published: %w", batch.ID, err)
		}

		publishedCount++
		p.logger.InfoContext(ctx, "batch completed signal published",
			slog.String("batch_id", batch.ID.String()),
			slog.String("source_type", batch.SourceType),
		)
	}

	return publishedCount, nil
}
