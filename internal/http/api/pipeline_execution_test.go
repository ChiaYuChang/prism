package api_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	pipeline "github.com/ChiaYuChang/prism/internal/analyzer/pipeline"
	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/ChiaYuChang/prism/internal/repo/mocks"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestCreateAdminPipelineTaskExecutesWithConfiguredWorker(t *testing.T) {
	srv, _ := newTestServer(t)
	apiRuntime := mocks.NewMockPipelineRuntime(t)
	srv.PipelineRuntime = apiRuntime
	inputID := uuid.New()
	var createdParams repo.CreateTaskParams
	var createdTask repo.Task
	apiRuntime.EXPECT().CreatePipelineRoot(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, arg repo.CreateTaskParams) (repo.Task, error) {
		createdParams = arg
		createdTask = repo.Task{
			ID: uuid.New(), BatchID: arg.BatchID, Kind: arg.Kind, SourceType: arg.SourceType,
			SourceAbbr: arg.SourceAbbr, TraceID: arg.TraceID, Payload: arg.Payload, Status: repo.TaskStatusRunning,
		}
		return createdTask, nil
	}).Once()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/pipelines", bytes.NewBufferString(
		`{"input_batch_id":"`+inputID.String()+`","source_type":"PARTY","source_abbr":"dpp","trace_id":"api-trace"}`,
	))
	rec := httptest.NewRecorder()
	srv.CreateAdminPipeline(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code)
	require.NotEmpty(t, createdParams.PipelineDefinitionHash)

	var metadata map[string]string
	require.NoError(t, json.Unmarshal(createdParams.Payload, &metadata))
	require.Equal(t, "configs/llm_pipeline.yaml", metadata["pipeline_file"])
	require.Equal(t, createdParams.PipelineDefinitionHash, metadata["pipeline_definition_hash"])

	definitionPath := pipelineDefinitionPath(t)
	definition, err := os.ReadFile(definitionPath)
	require.NoError(t, err)
	definitionHash := fmt.Sprintf("%x", sha256.Sum256(definition))
	require.Equal(t, definitionHash, createdParams.PipelineDefinitionHash)

	tasks := mocks.NewMockTasks(t)
	workerRuntime := mocks.NewMockPipelineRuntime(t)
	reporter := mocks.NewMockScheduler(t)
	registry, err := pipeline.NewRegistry(pipeline.EmbedCandidateBuilder{}, pipeline.EmbedContentBuilder{})
	require.NoError(t, err)
	coordinator, err := pipeline.NewCoordinator(tasks, workerRuntime, reporter, registry)
	require.NoError(t, err)
	handler, err := pipeline.NewHandler(tasks, reporter, workerRuntime, coordinator, 3, definitionHash)
	require.NoError(t, err)
	task := repo.Task{
		ID: createdTask.ID, BatchID: createdParams.BatchID, Kind: repo.TaskKindPipelineInit, Status: repo.TaskStatusRunning,
		SourceType: repo.SourceTypeParty, SourceAbbr: "dpp", TraceID: "worker-trace", Payload: createdParams.Payload,
	}
	tasks.EXPECT().GetTaskByID(mock.Anything, task.ID).Return(task, nil).Once()
	workerRuntime.EXPECT().InitializePipeline(mock.Anything, mock.MatchedBy(func(arg repo.InitializePipelineParams) bool {
		return arg.BatchID == createdParams.BatchID && arg.NSubtasks == 4 && len(arg.Tasks) == 3
	})).Return(nil).Once()
	spec, err := pipeline.LoadFile(definitionPath)
	require.NoError(t, err)

	ack, err := handler.HandleTaskSignal(context.Background(), message.TaskSignal{
		TaskID: task.ID, BatchID: task.BatchID, Kind: repo.TaskKindPipelineInit,
	}, spec)
	require.NoError(t, err)
	require.True(t, ack)
}

func pipelineDefinitionPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Join(filepath.Dir(file), "../../../configs/llm_pipeline.yaml")
}
