package pipeline

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/ChiaYuChang/prism/internal/repo/mocks"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type captureWorkBuilder struct {
	input  WorkSetInput
	inputs []WorkSetInput
}

func (*captureWorkBuilder) TaskType() string { return "CAPTURE" }

func (b *captureWorkBuilder) Build(_ string, _ map[string]any, input WorkSetInput) ([]WorkTaskSpec, error) {
	b.input = input
	b.inputs = append(b.inputs, input)
	return []WorkTaskSpec{{
		LogicalKey: "capture:one", Kind: repo.TaskKindEmbedCandidate, SourceType: repo.SourceTypeParty,
		SourceAbbr: "dpp", URL: "https://pipeline.test/work", Payload: []byte(`{}`),
	}}, nil
}

func TestHandlerStagesReuseSnapshotAcrossMutationAndReplay(t *testing.T) {
	tasks := mocks.NewMockTasks(t)
	runtime := mocks.NewMockPipelineRuntime(t)
	reporter := mocks.NewMockScheduler(t)
	builder := &captureWorkBuilder{}
	registry, err := NewRegistry(builder)
	require.NoError(t, err)
	coordinator, err := NewCoordinator(tasks, runtime, reporter, registry)
	require.NoError(t, err)
	handler, err := NewHandler(tasks, reporter, runtime, coordinator, 3, "")
	require.NoError(t, err)

	rootID := uuid.New()
	candidateID, contentID := uuid.New(), uuid.New()
	now := time.Now()
	stageTask := func(id uuid.UUID, name string) repo.Task {
		payload, marshalErr := json.Marshal(StageSpec{Name: name, Config: map[string]any{"task_type": "CAPTURE"}})
		require.NoError(t, marshalErr)
		return repo.Task{ID: id, BatchID: rootID, Kind: repo.TaskKindPipelineStage, Status: repo.TaskStatusRunning, Payload: payload}
	}
	task1, task2 := stageTask(uuid.New(), "stage_one"), stageTask(uuid.New(), "stage_two")
	tasks.EXPECT().GetTaskByID(mock.Anything, task1.ID).Return(task1, nil).Times(2)
	tasks.EXPECT().GetTaskByID(mock.Anything, task2.ID).Return(task2, nil).Once()
	runtime.EXPECT().GetPipelineBatch(mock.Anything, rootID).Return(repo.Batch{ID: rootID, PipelineInputSnapshotAt: &now}, nil).Times(3)
	runtime.EXPECT().ListPipelineInputCandidates(mock.Anything, rootID).Return([]repo.PipelineInputMember{{ID: candidateID, SourceAbbr: "dpp"}}, nil).Times(3)
	runtime.EXPECT().ListPipelineInputContents(mock.Anything, rootID).Return([]repo.PipelineInputMember{{ID: contentID, SourceAbbr: "dpp"}}, nil).Times(3)
	runtime.EXPECT().InitializePipelineStage(mock.Anything, mock.Anything).Return(uuid.New(), nil).Times(3)

	for _, task := range []repo.Task{task1, task2, task1} {
		ack, stageErr := handler.HandleTaskSignal(context.Background(), message.TaskSignal{TaskID: task.ID, BatchID: rootID, Kind: repo.TaskKindPipelineStage}, PipelineSpec{})
		require.NoError(t, stageErr)
		require.True(t, ack)
	}
	require.Len(t, builder.inputs, 3)
	for _, input := range builder.inputs[1:] {
		require.ElementsMatch(t, builder.inputs[0].CandidateIDs, input.CandidateIDs)
		require.ElementsMatch(t, builder.inputs[0].ContentIDs, input.ContentIDs)
		require.Equal(t, builder.inputs[0].CandidateSourceAbbr, input.CandidateSourceAbbr)
		require.Equal(t, builder.inputs[0].ContentSourceAbbr, input.ContentSourceAbbr)
	}
}

func TestHandlerStageUsesImmutablePipelineSnapshot(t *testing.T) {
	tasks := mocks.NewMockTasks(t)
	runtime := mocks.NewMockPipelineRuntime(t)
	reporter := mocks.NewMockScheduler(t)
	builder := &captureWorkBuilder{}
	registry, err := NewRegistry(builder)
	require.NoError(t, err)
	coordinator, err := NewCoordinator(tasks, runtime, reporter, registry)
	require.NoError(t, err)
	handler, err := NewHandler(tasks, reporter, runtime, coordinator, 3, "")
	require.NoError(t, err)

	rootID, taskID := uuid.New(), uuid.New()
	candidateID, contentID := uuid.New(), uuid.New()
	now := time.Now()
	payload, err := json.Marshal(StageSpec{Name: "capture", Config: map[string]any{"task_type": "CAPTURE"}})
	require.NoError(t, err)
	task := repo.Task{
		ID: taskID, BatchID: rootID, Kind: repo.TaskKindPipelineStage, Status: repo.TaskStatusRunning,
		SourceType: repo.SourceTypeParty, SourceAbbr: "dpp", TraceID: "trace", Payload: payload,
	}
	tasks.EXPECT().GetTaskByID(mock.Anything, taskID).Return(task, nil).Once()
	runtime.EXPECT().GetPipelineBatch(mock.Anything, rootID).Return(repo.Batch{
		ID: rootID, Purpose: "ANALYZER_PIPELINE_ROOT", PipelineInputSnapshotAt: &now,
	}, nil).Once()
	runtime.EXPECT().ListPipelineInputCandidates(mock.Anything, rootID).Return([]repo.PipelineInputMember{{ID: candidateID, SourceAbbr: "dpp"}}, nil).Once()
	runtime.EXPECT().ListPipelineInputContents(mock.Anything, rootID).Return([]repo.PipelineInputMember{{ID: contentID, SourceAbbr: "dpp"}}, nil).Once()
	runtime.EXPECT().InitializePipelineStage(mock.Anything, mock.MatchedBy(func(arg repo.InitializePipelineStageParams) bool {
		return arg.ParentBatchID == rootID && arg.ParentTaskID == taskID && arg.NSubtasks == 1
	})).Return(uuid.New(), nil).Once()

	ack, err := handler.HandleTaskSignal(context.Background(), message.TaskSignal{TaskID: taskID, BatchID: rootID, Kind: repo.TaskKindPipelineStage}, PipelineSpec{})
	require.NoError(t, err)
	require.True(t, ack)
	require.ElementsMatch(t, []uuid.UUID{candidateID}, builder.input.CandidateIDs)
	require.ElementsMatch(t, []uuid.UUID{contentID}, builder.input.ContentIDs)
	require.Equal(t, "dpp", builder.input.CandidateSourceAbbr[candidateID])
	require.Equal(t, "dpp", builder.input.ContentSourceAbbr[contentID])
}

func TestHandlerStageRejectsMissingSnapshotWithoutLiveFallback(t *testing.T) {
	tasks := mocks.NewMockTasks(t)
	runtime := mocks.NewMockPipelineRuntime(t)
	reporter := mocks.NewMockScheduler(t)
	registry, err := NewRegistry(&captureWorkBuilder{})
	require.NoError(t, err)
	coordinator, err := NewCoordinator(tasks, runtime, reporter, registry)
	require.NoError(t, err)
	handler, err := NewHandler(tasks, reporter, runtime, coordinator, 3, "")
	require.NoError(t, err)

	rootID, taskID := uuid.New(), uuid.New()
	payload := json.RawMessage(`{"name":"capture","config":{"task_type":"CAPTURE"}}`)
	tasks.EXPECT().GetTaskByID(mock.Anything, taskID).Return(repo.Task{
		ID: taskID, BatchID: rootID, Kind: repo.TaskKindPipelineStage, Status: repo.TaskStatusRunning, Payload: payload,
	}, nil).Once()
	runtime.EXPECT().GetPipelineBatch(mock.Anything, rootID).Return(repo.Batch{ID: rootID}, nil).Once()
	reporter.EXPECT().FailTask(mock.Anything, taskID, 3, mock.Anything).Return(nil).Once()
	tasks.EXPECT().GetTaskByID(mock.Anything, taskID).Return(repo.Task{ID: taskID, BatchID: rootID, Status: repo.TaskStatusPending}, nil).Once()

	ack, err := handler.HandleTaskSignal(context.Background(), message.TaskSignal{TaskID: taskID, BatchID: rootID, Kind: repo.TaskKindPipelineStage}, PipelineSpec{})
	require.ErrorIs(t, err, repo.ErrPipelineSnapshotMissing)
	require.True(t, ack)
}

func TestHandlerInitRejectsDefinitionHashMismatch(t *testing.T) {
	handler := &Handler{definitionHash: "expected"}
	err := handler.handleTask(context.Background(), repo.Task{
		Payload: json.RawMessage(`{"pipeline_definition_hash":"different"}`),
	}, repo.TaskKindPipelineInit, PipelineSpec{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "pipeline definition hash mismatch")
}
