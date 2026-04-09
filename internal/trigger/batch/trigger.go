package batch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
)

const (
	PartySourceType = "PARTY"
)

var (
	ErrParamMissing = errors.New("param missing")
)

type CompletedBatch struct {
	BatchID          uuid.UUID
	SourceType       string
	TraceID          string
	CandidateCount   int64
	ContentCount     int
	CompletedTaskIDs []uuid.UUID
}

type BatchProgress struct {
	BatchID          uuid.UUID
	SourceType       string
	TraceID          string
	TotalTasks       int
	CompletedTasks   int
	CandidateCount   int64
	ContentCount     int
	CompletedTaskIDs []uuid.UUID
}

func (p BatchProgress) IsCompleted() bool {
	return p.TotalTasks > 0 &&
		p.TotalTasks == p.CompletedTasks &&
		p.CandidateCount > 0 &&
		int64(p.ContentCount) >= p.CandidateCount
}

type Trigger struct {
	logger       *slog.Logger
	tracer       trace.Tracer
	batchTrigger repo.BatchTrigger
}

func New(logger *slog.Logger, tracer trace.Tracer, batchTrigger repo.BatchTrigger) (*Trigger, error) {
	if logger == nil {
		return nil, fmt.Errorf("%w: logger", ErrParamMissing)
	}
	if tracer == nil {
		return nil, fmt.Errorf("%w: tracer", ErrParamMissing)
	}
	if batchTrigger == nil {
		return nil, fmt.Errorf("%w: batch_trigger_repository", ErrParamMissing)
	}
	return &Trigger{
		logger:       logger,
		tracer:       tracer,
		batchTrigger: batchTrigger,
	}, nil
}

func (t *Trigger) ScanCompletedBatches(ctx context.Context, limit int32) ([]CompletedBatch, error) {
	ctx, span := t.tracer.Start(ctx, "trigger.batch.scan_completed")
	defer span.End()

	contents, err := t.batchTrigger.ListRecentSeedContents(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("list recent seed contents: %w", err)
	}

	seen := map[uuid.UUID]struct{}{}
	completed := []CompletedBatch{}
	for _, content := range contents {
		if content.BatchID == uuid.Nil {
			continue
		}
		batchID := content.BatchID
		if _, ok := seen[batchID]; ok {
			continue
		}
		seen[batchID] = struct{}{}

		progress, err := t.GetBatchProgress(ctx, batchID)
		if err != nil {
			return nil, fmt.Errorf("check batch %s: %w", batchID, err)
		}
		if !progress.IsCompleted() {
			continue
		}

		completed = append(completed, CompletedBatch{
			BatchID:          batchID,
			SourceType:       progress.SourceType,
			TraceID:          progress.TraceID,
			CandidateCount:   progress.CandidateCount,
			ContentCount:     progress.ContentCount,
			CompletedTaskIDs: progress.CompletedTaskIDs,
		})
	}

	return completed, nil
}

func (t *Trigger) GetBatchProgress(ctx context.Context, batchID uuid.UUID) (BatchProgress, error) {
	var progress = BatchProgress{
		BatchID:    batchID,
		SourceType: PartySourceType,
	}

	tasks, err := t.batchTrigger.ListTasksByBatchID(ctx, batchID)
	if err != nil {
		return progress, err
	}
	
	for _, task := range tasks {
		if task.SourceType != PartySourceType {
			continue
		}
		progress.TotalTasks++
		progress.CompletedTaskIDs = append(progress.CompletedTaskIDs, task.ID)
		if progress.TraceID == "" {
			progress.TraceID = task.TraceID
		}
		if task.Status == "COMPLETED" {
			progress.CompletedTasks++
		}
	}
	if progress.TotalTasks == 0 {
		return progress, nil
	}

	candidateCount, err := t.batchTrigger.CountCandidatesByBatchID(ctx, batchID)
	if err != nil {
		return progress, err
	}
	progress.CandidateCount = candidateCount
	
	if candidateCount == 0 {
		return progress, nil
	}

	contents, err := t.batchTrigger.ListContentsByBatchID(ctx, batchID)
	if err != nil {
		return progress, err
	}
	progress.ContentCount = len(contents)

	return progress, nil
}
