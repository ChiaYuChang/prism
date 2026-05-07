// Package batch provides functionality for managing and monitoring the lifecycle of
// asynchronous data collection batches. It includes components for detecting
// when a batch has completed all its constituent tasks and for tracking
// the progress of active batches.
package batch

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
)

type Detector struct {
	logger *slog.Logger
	tracer trace.Tracer
	repo   repo.BatchTrigger
}

func NewDetector(logger *slog.Logger, tracer trace.Tracer, r repo.BatchTrigger) (*Detector, error) {
	if logger == nil {
		return nil, fmt.Errorf("%w: logger", ErrParamMissing)
	}
	if tracer == nil {
		return nil, fmt.Errorf("%w: tracer", ErrParamMissing)
	}
	if r == nil {
		return nil, fmt.Errorf("%w: batch_trigger_repository", ErrParamMissing)
	}
	return &Detector{
		logger: logger,
		tracer: tracer,
		repo:   r,
	}, nil
}

// Detect scans for batches that are functionally complete using an efficient bulk query.
func (d *Detector) Detect(ctx context.Context, limit int32) ([]CompletedBatch, error) {
	ctx, span := d.tracer.Start(ctx, "batch.detector.detect")
	defer span.End()

	// Use the optimized bulk query for efficient detection.
	batches, err := d.repo.FindNewlyCompletedBatches(ctx, limit, repo.SourceTypeParty)
	if err != nil {
		return nil, fmt.Errorf("find newly completed batches: %w", err)
	}

	completed := []CompletedBatch{}
	for _, batch := range batches {
		traceID := ""
		if batch.TraceID != nil {
			traceID = *batch.TraceID
		}

		// Optimistic claim. rows == 1 means this instance won the race and
		// owns the batch.completed publish; rows == 0 means another
		// instance already marked it and we drop it silently to avoid a
		// duplicate signal.
		rows, err := d.repo.MarkBatchCompleted(ctx, batch.ID, traceID)
		if err != nil {
			return nil, fmt.Errorf("mark batch %s completed: %w", batch.ID, err)
		}
		if rows == 0 {
			d.logger.InfoContext(ctx, "batch already claimed by another instance; skipping",
				slog.String("batch_id", batch.ID.String()),
				slog.String("source_type", batch.SourceType),
			)
			continue
		}

		completed = append(completed, CompletedBatch{
			BatchID:    batch.ID,
			SourceType: batch.SourceType,
			TraceID:    traceID,
		})

		d.logger.InfoContext(ctx, "batch marked as completed",
			slog.String("batch_id", batch.ID.String()),
			slog.String("source_type", batch.SourceType),
		)
	}

	return completed, nil
}

// GetBatchProgress provides a detailed snapshot of a batch's progress.
// Intended for user-facing status queries, not for the main detection loop.
func (d *Detector) GetBatchProgress(ctx context.Context, batchID uuid.UUID) (BatchProgress, error) {
	var progress = BatchProgress{
		BatchID:         batchID,
		SourceType:      repo.SourceTypeParty,
		TaskIDsByStatus: map[repo.TaskStatus][]uuid.UUID{},
	}

	tasks, err := d.repo.ListTasksByBatchID(ctx, batchID)
	if err != nil {
		return progress, err
	}

	for _, task := range tasks {
		if task.SourceType != repo.SourceTypeParty {
			continue
		}
		progress.TotalTasks++
		progress.TaskIDsByStatus[task.Status] = append(progress.TaskIDsByStatus[task.Status], task.ID)
		if progress.TraceID == "" {
			progress.TraceID = task.TraceID
		}
		if task.Status == repo.TaskStatusCompleted {
			progress.CompletedTasks++
		}
	}
	if progress.TotalTasks == 0 {
		return progress, nil
	}

	candidateCount, err := d.repo.CountCandidatesByBatchID(ctx, batchID)
	if err != nil {
		return progress, err
	}
	progress.CandidateCount = candidateCount

	if candidateCount == 0 {
		return progress, nil
	}

	contents, err := d.repo.ListContentsByBatchID(ctx, batchID)
	if err != nil {
		return progress, err
	}
	progress.ContentCount = len(contents)

	return progress, nil
}
