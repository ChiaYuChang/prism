package batch

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
)

// Finisher claims newly terminal pipeline batches and emits one post-commit
// notification for each winning transition.
type Finisher struct {
	logger    *slog.Logger
	tracer    trace.Tracer
	runtime   repo.PipelineRuntime
	publisher message.PipelineBatchFinishedPublisher
}

func NewFinisher(logger *slog.Logger, tracer trace.Tracer, runtime repo.PipelineRuntime, publisher message.PipelineBatchFinishedPublisher) (*Finisher, error) {
	if logger == nil || tracer == nil || runtime == nil || publisher == nil {
		return nil, fmt.Errorf("pipeline finisher dependency is missing")
	}
	return &Finisher{logger: logger, tracer: tracer, runtime: runtime, publisher: publisher}, nil
}

func (f *Finisher) Detect(ctx context.Context, limit int32) (int, error) {
	ctx, span := f.tracer.Start(ctx, "pipeline.finisher.detect")
	defer span.End()
	batches, err := f.runtime.FindFinishedBatches(ctx, limit)
	if err != nil {
		return 0, fmt.Errorf("find finished pipeline batches: %w", err)
	}
	count := 0
	for _, batch := range batches {
		succeeded := batch.Succeeded != nil && *batch.Succeeded
		traceID := ""
		if batch.TraceID != nil {
			traceID = *batch.TraceID
		}
		rows, err := f.runtime.MarkBatchFinished(ctx, batch.ID, succeeded, traceID)
		if err != nil {
			return count, fmt.Errorf("mark pipeline batch %s finished: %w", batch.ID, err)
		}
		if rows == 0 {
			continue
		}
	}
	// completed_at with pipeline_published_at NULL is the transactional outbox:
	// scan it independently so a prior publish failure remains retryable.
	ready, err := f.runtime.ListReadyPipelineBatches(ctx, limit)
	if err != nil {
		return count, fmt.Errorf("list ready pipeline batches: %w", err)
	}
	for _, batch := range ready {
		succeeded := batch.Succeeded != nil && *batch.Succeeded
		traceID := ""
		if batch.TraceID != nil {
			traceID = *batch.TraceID
		}
		rootID := batch.ID
		if batch.ParentID != nil {
			rootID = *batch.ParentID
		}
		ownerID := uuid.Nil
		if batch.ParentTaskID != nil {
			ownerID = *batch.ParentTaskID
		}
		if err := f.publisher.PublishPipelineBatchFinished(ctx, &message.PipelineBatchFinishedSignal{
			BatchID: batch.ID, RootBatchID: rootID, OwnerTaskID: ownerID,
			Succeeded: succeeded, TraceID: traceID,
		}); err != nil {
			if recordErr := f.runtime.RecordPipelinePublishFailure(ctx, batch.ID, err.Error()); recordErr != nil {
				return count, fmt.Errorf("publish pipeline batch %s finished: %w; record failure: %w", batch.ID, err, recordErr)
			}
			return count, fmt.Errorf("publish pipeline batch %s finished: %w", batch.ID, err)
		}
		if err := f.runtime.MarkPipelinePublished(ctx, batch.ID); err != nil {
			return count, fmt.Errorf("mark pipeline batch %s published: %w", batch.ID, err)
		}
		count++
	}
	rootBatches, err := f.runtime.FindFinishedRootBatches(ctx, limit)
	if err != nil {
		return count, fmt.Errorf("find finished pipeline root batches: %w", err)
	}
	for _, batch := range rootBatches {
		succeeded := batch.Succeeded != nil && *batch.Succeeded
		traceID := ""
		if batch.TraceID != nil {
			traceID = *batch.TraceID
		}
		rows, err := f.runtime.MarkRootBatchFinished(ctx, batch.ID, succeeded, traceID)
		if err != nil {
			return count, fmt.Errorf("mark pipeline root %s finished: %w", batch.ID, err)
		}
		if rows == 1 {
			count++
		}
	}
	return count, nil
}
