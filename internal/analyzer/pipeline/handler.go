package pipeline

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/google/uuid"
)

// Handler is the task-topic and completion-topic adapter for Coordinator.
// It deliberately keeps Watermill out of the domain package so it is easy to
// exercise with repository fakes and to reuse from different worker binaries.
type Handler struct {
	tasks          repo.Tasks
	reporter       repo.TaskReporter
	runtime        repo.PipelineRuntime
	coord          *Coordinator
	retryMax       int
	definitionHash string
}

func NewHandler(tasks repo.Tasks, reporter repo.TaskReporter, runtime repo.PipelineRuntime, coord *Coordinator, retryMax int, definitionHash string) (*Handler, error) {
	if tasks == nil || reporter == nil || runtime == nil || coord == nil {
		return nil, fmt.Errorf("pipeline handler dependencies are required")
	}
	if retryMax < 1 {
		return nil, fmt.Errorf("retry max must be positive")
	}
	return &Handler{tasks: tasks, reporter: reporter, runtime: runtime, coord: coord, retryMax: retryMax, definitionHash: definitionHash}, nil
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
		if task.Status == repo.TaskStatusFailed {
			reason := "pipeline task failed"
			if task.FailureMessage != nil {
				reason = *task.FailureMessage
			}
			if err := h.runtime.ConvergePipelineFailure(ctx, task.ID, task.BatchID, reason); err != nil {
				return false, fmt.Errorf("retry convergence for failed pipeline task %s: %w", task.ID, err)
			}
		}
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
		if h.definitionHash != "" {
			var payload struct {
				DefinitionHash string `json:"pipeline_definition_hash"`
			}
			if err := json.Unmarshal(task.Payload, &payload); err != nil {
				return fmt.Errorf("decode pipeline definition metadata: %w", err)
			}
			if payload.DefinitionHash != h.definitionHash {
				return fmt.Errorf("pipeline definition hash mismatch: task=%s worker=%s", payload.DefinitionHash, h.definitionHash)
			}
		}
		return h.coord.Initialize(ctx, task, spec)
	case repo.TaskKindPipelineStage:
		root, err := h.runtime.GetPipelineBatch(ctx, task.BatchID)
		if err != nil {
			return fmt.Errorf("get pipeline root batch: %w", err)
		}
		if root.PipelineInputSnapshotAt == nil {
			return repo.ErrPipelineSnapshotMissing
		}
		candidates, err := h.runtime.ListPipelineInputCandidates(ctx, task.BatchID)
		if err != nil {
			return fmt.Errorf("list pipeline input candidates: %w", err)
		}
		contents, err := h.runtime.ListPipelineInputContents(ctx, task.BatchID)
		if err != nil {
			return fmt.Errorf("list pipeline input contents: %w", err)
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
