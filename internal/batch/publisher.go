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

// Publisher delivers the existing PARTY batch.completed notifications.
type Publisher struct {
	logger    *slog.Logger
	tracer    trace.Tracer
	repo      repo.BatchTrigger
	publisher message.BatchCompletedPublisher
}

func NewPublisher(logger *slog.Logger, tracer trace.Tracer, r repo.BatchTrigger, p message.BatchCompletedPublisher) (*Publisher, error) {
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
	return &Publisher{logger: logger, tracer: tracer, repo: r, publisher: p}, nil
}

func (p *Publisher) Publish(ctx context.Context, limit int32) (int, error) {
	ctx, span := p.tracer.Start(ctx, "batch.publisher.publish")
	defer span.End()
	batches, err := p.repo.ListReadyToPublishBatches(ctx, limit)
	if err != nil {
		return 0, fmt.Errorf("list ready to publish batches: %w", err)
	}
	count := 0
	for _, batch := range batches {
		traceID := ""
		if batch.TraceID != nil {
			traceID = *batch.TraceID
		}
		if err := p.publisher.PublishBatchCompleted(ctx, &message.BatchCompletedSignal{BatchID: batch.ID, SourceType: batch.SourceType, TraceID: traceID, SentAt: time.Now()}); err != nil {
			p.logger.ErrorContext(ctx, "failed to publish batch completed signal", slog.String("batch_id", batch.ID.String()), slog.Any("error", err))
			if recErr := p.repo.RecordBatchPublishFailure(ctx, batch.ID, err.Error()); recErr != nil {
				return count, fmt.Errorf("publish batch %s signal: %w; record failure: %w", batch.ID, err, recErr)
			}
			continue
		}
		if err := p.repo.MarkBatchPublished(ctx, batch.ID); err != nil {
			return count, fmt.Errorf("mark batch %s as published: %w", batch.ID, err)
		}
		count++
	}
	return count, nil
}

// PipelinePublisher retries completion notifications that were committed but
// could not be delivered by the first finisher attempt.
type PipelinePublisher struct {
	runtime   repo.PipelineRuntime
	publisher message.PipelineBatchFinishedPublisher
}

func NewPipelinePublisher(runtime repo.PipelineRuntime, publisher message.PipelineBatchFinishedPublisher) (*PipelinePublisher, error) {
	if runtime == nil || publisher == nil {
		return nil, fmt.Errorf("runtime and publisher are required")
	}
	return &PipelinePublisher{runtime: runtime, publisher: publisher}, nil
}

func (p *PipelinePublisher) PublishPending(ctx context.Context, limit int32) (int, error) {
	batches, err := p.runtime.ListReadyPipelineBatches(ctx, limit)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, batch := range batches {
		if batch.ParentID == nil || batch.ParentTaskID == nil {
			return count, fmt.Errorf("pipeline batch %s has no parent or owner task", batch.ID)
		}
		traceID := ""
		if batch.TraceID != nil {
			traceID = *batch.TraceID
		}
		succeeded := batch.Succeeded != nil && *batch.Succeeded
		err := p.publisher.PublishPipelineBatchFinished(ctx, &message.PipelineBatchFinishedSignal{
			BatchID: batch.ID, RootBatchID: *batch.ParentID, OwnerTaskID: *batch.ParentTaskID,
			Succeeded: succeeded, TraceID: traceID,
		})
		if err != nil {
			_ = p.runtime.RecordPipelinePublishFailure(ctx, batch.ID, err.Error())
			return count, err
		}
		if err := p.runtime.MarkPipelinePublished(ctx, batch.ID); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}
