package pipeline

import (
	"context"
	"fmt"

	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/google/uuid"
)

// Handler is the task-topic and completion-topic adapter for Coordinator.
// It deliberately keeps Watermill out of the domain package so it is easy to
// exercise with repository fakes and to reuse from different worker binaries.
type Handler struct {
	tasks    repo.Tasks
	scout    repo.Scout
	pipeline repo.Pipeline
	reporter repo.TaskReporter
	runtime  repo.PipelineRuntime
	coord    *Coordinator
	retryMax int
}

func NewHandler(tasks repo.Tasks, scout repo.Scout, pipeline repo.Pipeline, reporter repo.TaskReporter, runtime repo.PipelineRuntime, coord *Coordinator, retryMax int) (*Handler, error) {
	if tasks == nil || scout == nil || pipeline == nil || reporter == nil || runtime == nil || coord == nil {
		return nil, fmt.Errorf("pipeline handler dependencies are required")
	}
	if retryMax < 1 {
		return nil, fmt.Errorf("retry max must be positive")
	}
	return &Handler{tasks: tasks, scout: scout, pipeline: pipeline, reporter: reporter, runtime: runtime, coord: coord, retryMax: retryMax}, nil
}

// HandleTaskSignal handles only pipeline task kinds. The boolean indicates
// whether the broker message can be acknowledged.
func (h *Handler) HandleTaskSignal(ctx context.Context, signal message.TaskSignal, spec PipelineSpec) (bool, error) {
	if signal.Kind != repo.TaskKindPipelineInit && signal.Kind != repo.TaskKindPipelineStage {
		return true, nil
	}
	task, err := h.tasks.GetTaskByID(ctx, signal.TaskID)
	if err != nil {
		return false, fmt.Errorf("get pipeline task %s: %w", signal.TaskID, err)
	}
	if task.Status != repo.TaskStatusRunning {
		return true, nil
	}
	if err := h.handleTask(ctx, task, signal.Kind, spec); err != nil {
		if failErr := h.reporter.FailTask(ctx, task.ID, h.retryMax, err.Error()); failErr != nil {
			return false, fmt.Errorf("handle pipeline task %s: %w; mark failed: %w", task.ID, err, failErr)
		}
		failedTask, getErr := h.tasks.GetTaskByID(ctx, task.ID)
		if getErr != nil {
			return false, fmt.Errorf("refresh failed pipeline task %s: %w", task.ID, getErr)
		}
		if failedTask.Status == repo.TaskStatusFailed {
			if convergeErr := h.runtime.ConvergePipelineFailure(ctx, task.ID, task.BatchID, err.Error()); convergeErr != nil {
				return false, fmt.Errorf("converge failed pipeline task %s: %w", task.ID, convergeErr)
			}
		}
		return true, err
	}
	return true, nil
}

func (h *Handler) handleTask(ctx context.Context, task repo.Task, kind string, spec PipelineSpec) error {
	switch kind {
	case repo.TaskKindPipelineInit:
		return h.coord.Initialize(ctx, task, spec)
	case repo.TaskKindPipelineStage:
		candidates, err := h.scout.ListCandidatesByBatchID(ctx, task.BatchID)
		if err != nil {
			return fmt.Errorf("list pipeline candidates: %w", err)
		}
		contents, err := h.pipeline.ListContentsByBatchID(ctx, task.BatchID)
		if err != nil {
			return fmt.Errorf("list pipeline contents: %w", err)
		}
		input := WorkSetInput{
			CandidateSourceAbbr: make(map[uuid.UUID]string, len(candidates)),
			ContentSourceAbbr:   make(map[uuid.UUID]string, len(contents)),
		}
		for _, candidate := range candidates {
			input.CandidateIDs = append(input.CandidateIDs, candidate.ID)
			input.CandidateSourceAbbr[candidate.ID] = candidate.SourceAbbr
		}
		for _, content := range contents {
			input.ContentIDs = append(input.ContentIDs, content.ID)
			input.ContentSourceAbbr[content.ID] = content.SourceAbbr
		}
		return h.coord.RunStage(ctx, task, input)
	default:
		return fmt.Errorf("unsupported pipeline task kind %q", kind)
	}
}

func (h *Handler) HandleBatchFinished(ctx context.Context, signal message.PipelineBatchFinishedSignal) (bool, error) {
	if signal.BatchID == uuid.Nil || signal.RootBatchID == uuid.Nil || signal.OwnerTaskID == uuid.Nil {
		return true, fmt.Errorf("invalid pipeline completion signal")
	}
	if err := h.coord.HandleBatchFinished(ctx, signal); err != nil {
		return false, err
	}
	return true, nil
}
