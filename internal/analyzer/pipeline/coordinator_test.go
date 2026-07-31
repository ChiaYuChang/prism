package pipeline

import (
	"context"
	"testing"

	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/ChiaYuChang/prism/internal/repo/mocks"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func messageSignal(rootID, ownerID uuid.UUID, succeeded bool) message.PipelineBatchFinishedSignal {
	return message.PipelineBatchFinishedSignal{RootBatchID: rootID, OwnerTaskID: ownerID, Succeeded: succeeded}
}

func TestCoordinatorInitializesIdempotentControlChain(t *testing.T) {
	tasks := mocks.NewMockTasks(t)
	runtime := mocks.NewMockPipelineRuntime(t)
	reporter := mocks.NewMockTaskReporter(t)
	registry, err := NewRegistry(EmbedCandidateBuilder{})
	require.NoError(t, err)
	coordinator, err := NewCoordinator(tasks, runtime, reporter, registry)
	require.NoError(t, err)

	rootBatchID, initID, stageID := uuid.New(), uuid.New(), uuid.New()
	traceID := "trace"
	tasks.EXPECT().CreateTask(mock.Anything, mock.MatchedBy(func(arg repo.CreateTaskParams) bool {
		return arg.BatchID == rootBatchID && arg.Kind == repo.TaskKindPipelineStage && arg.SourceAbbr == "dpp"
	})).Return(repo.Task{ID: stageID, BatchID: rootBatchID}, nil)
	runtime.EXPECT().SetNSubtasks(mock.Anything, rootBatchID, int32(2)).Return(repo.Batch{ID: rootBatchID}, nil)
	reporter.EXPECT().CompleteTask(mock.Anything, initID).Return(nil)

	err = coordinator.Initialize(context.Background(), repo.Task{
		ID: initID, BatchID: rootBatchID, SourceType: repo.SourceTypeParty, SourceAbbr: "dpp", TraceID: traceID,
	}, PipelineSpec{Pipeline: []StageSpec{{Name: "embed", Config: map[string]any{"task_type": repo.TaskKindEmbedCandidate, "fan_out": "candidate_ids"}}}})
	require.NoError(t, err)
}

func TestCoordinatorCancelsRootAfterFailedChild(t *testing.T) {
	tasks := mocks.NewMockTasks(t)
	runtime := mocks.NewMockPipelineRuntime(t)
	reporter := mocks.NewMockTaskReporter(t)
	registry, err := NewRegistry(EmbedCandidateBuilder{})
	require.NoError(t, err)
	coordinator, err := NewCoordinator(tasks, runtime, reporter, registry)
	require.NoError(t, err)
	ownerID, rootID := uuid.New(), uuid.New()
	reporter.EXPECT().FailTask(mock.Anything, ownerID, 0, "pipeline child batch failed").Return(nil)
	tasks.EXPECT().CancelPendingTasksByBatchID(mock.Anything, rootID, "upstream pipeline stage failed").Return(int64(1), nil)

	err = coordinator.HandleBatchFinished(context.Background(), messageSignal(rootID, ownerID, false))
	require.NoError(t, err)
}
