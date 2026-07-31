package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/google/uuid"
)

var ErrCoordinatorParamMissing = errors.New("coordinator parameter missing")

// Coordinator owns the durable transitions around PIPELINE_INIT and
// PIPELINE_STAGE. Task creation is idempotent through logical_key, while the
// batch repository enforces n_subtasks and parent ownership.
type Coordinator struct {
	tasks    repo.Tasks
	runtime  repo.PipelineRuntime
	reporter repo.TaskReporter
	registry *Registry
}

func NewCoordinator(tasks repo.Tasks, runtime repo.PipelineRuntime, reporter repo.TaskReporter, registry *Registry) (*Coordinator, error) {
	if tasks == nil {
		return nil, fmt.Errorf("%w: tasks", ErrCoordinatorParamMissing)
	}
	if runtime == nil {
		return nil, fmt.Errorf("%w: pipeline runtime", ErrCoordinatorParamMissing)
	}
	if reporter == nil {
		return nil, fmt.Errorf("%w: task reporter", ErrCoordinatorParamMissing)
	}
	if registry == nil {
		return nil, fmt.Errorf("%w: work registry", ErrCoordinatorParamMissing)
	}
	return &Coordinator{tasks: tasks, runtime: runtime, reporter: reporter, registry: registry}, nil
}

// Initialize creates the complete control chain in the root batch. Replayed
// init tasks recover the existing control tasks by logical key.
func (c *Coordinator) Initialize(ctx context.Context, initTask repo.Task, spec PipelineSpec) error {
	control, err := CompileControlTasks(spec, "pipeline:init")
	if err != nil {
		return err
	}
	previousID := initTask.ID
	controlTasks := make([]repo.CreateTaskParams, 0, len(control))
	for _, item := range control {
		payload, err := json.Marshal(item.Stage)
		if err != nil {
			return fmt.Errorf("marshal stage %s: %w", item.Stage.Name, err)
		}
		logicalKey := item.LogicalKey
		previous := previousID
		controlTasks = append(controlTasks, repo.CreateTaskParams{
			BatchID: initTask.BatchID, PreviousTaskID: &previous, LogicalKey: &logicalKey,
			Kind: repo.TaskKindPipelineStage, SourceType: initTask.SourceType, SourceAbbr: initTask.SourceAbbr,
			URL: "pipeline://stage/" + item.Stage.Name, Payload: payload, TraceID: initTask.TraceID,
		})
		previousID = uuid.Nil
	}
	return c.runtime.InitializePipeline(ctx, repo.InitializePipelineParams{
		BatchID: initTask.BatchID, InitTaskID: initTask.ID, NSubtasks: int32(len(control) + 1), Tasks: controlTasks,
	})
}

// RunStage creates a child batch and its fan-out work tasks. A replay of the
// same stage reuses the unique parent_task_id ownership row.
func (c *Coordinator) RunStage(ctx context.Context, stageTask repo.Task, input WorkSetInput) error {
	var stage StageSpec
	if err := json.Unmarshal(stageTask.Payload, &stage); err != nil {
		return fmt.Errorf("decode stage task %s: %w", stageTask.ID, err)
	}
	work, err := c.registry.Build(stage.Name, stage.Config, input)
	if err != nil {
		return err
	}
	childBatchID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("create child batch id: %w", err)
	}
	workTasks := make([]repo.CreateTaskParams, 0, len(work))
	for _, item := range work {
		logicalKey := item.LogicalKey
		workTasks = append(workTasks, repo.CreateTaskParams{
			ParentBatchID: &stageTask.BatchID, ParentTaskID: &stageTask.ID,
			LogicalKey: &logicalKey, Kind: item.Kind, SourceType: item.SourceType, SourceAbbr: item.SourceAbbr,
			URL: item.URL, Payload: item.Payload, Meta: item.Meta, TraceID: stageTask.TraceID,
		})
	}
	if _, err := c.runtime.InitializePipelineStage(ctx, repo.InitializePipelineStageParams{
		ChildBatchID: childBatchID, ParentBatchID: stageTask.BatchID, ParentTaskID: stageTask.ID,
		SourceType: stageTask.SourceType, TraceID: stageTask.TraceID, NSubtasks: int32(len(work)), Tasks: workTasks,
	}); err != nil {
		return fmt.Errorf("initialize pipeline stage %s: %w", stage.Name, err)
	}
	return nil
}

// HandleBatchFinished is idempotent for duplicate completion messages. A
// failed child fails its owner and cancels pending control tasks in the root.
func (c *Coordinator) HandleBatchFinished(ctx context.Context, signal message.PipelineBatchFinishedSignal) error {
	if signal.Succeeded {
		return c.reporter.CompleteTask(ctx, signal.OwnerTaskID)
	}
	if err := c.reporter.FailTask(ctx, signal.OwnerTaskID, 0, "pipeline child batch failed"); err != nil {
		return err
	}
	_, err := c.tasks.CancelPendingTasksByBatchID(ctx, signal.RootBatchID, "upstream pipeline stage failed")
	return err
}
