package planner

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/ChiaYuChang/prism/internal/discovery"
	discoverymocks "github.com/ChiaYuChang/prism/internal/discovery/mocks"
	"github.com/ChiaYuChang/prism/internal/model"
	"github.com/ChiaYuChang/prism/internal/repo"
	repomocks "github.com/ChiaYuChang/prism/internal/repo/mocks"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestPlannerPlanCreatesMediaTasksFromUniquePhrases(t *testing.T) {
	extractor := discoverymocks.NewMockExtractor(t)
	tasks := repomocks.NewMockTasks(t)
	pipeline := repomocks.NewMockPipeline(t)

	batchID := uuid.Must(uuid.NewV7())
	content1ID := uuid.Must(uuid.NewV7())
	content2ID := uuid.Must(uuid.NewV7())

	p, err := New(testPlannerLogger(), noop.NewTracerProvider().Tracer("test"), extractor, tasks, pipeline)
	require.NoError(t, err)

	pipeline.EXPECT().ListContentsByBatchID(mock.Anything, batchID).Return([]repo.Content{
		{ID: content1ID, Title: "A", Content: "Body A"},
		{ID: content2ID, Title: "B", Content: "Body B"},
	}, nil)
	extractor.EXPECT().Extract(mock.Anything, &model.ExtractionInput{Title: "A", Body: "Body A"}).Return(&model.ExtractionOutput{
		Phrases: []string{"topic one", " topic two "},
	}, nil)
	extractor.EXPECT().Extract(mock.Anything, &model.ExtractionInput{Title: "B", Body: "Body B"}).Return(&model.ExtractionOutput{
		Phrases: []string{"topic two", ""},
	}, nil)

	var created []repo.CreateTaskParams
	tasks.EXPECT().CreateTask(mock.Anything, mock.Anything).RunAndReturn(
		func(_ context.Context, arg repo.CreateTaskParams) (repo.Task, error) {
			created = append(created, arg)
			return repo.Task{ID: uuid.Must(uuid.NewV7())}, nil
		},
	).Times(4)

	result, err := p.Plan(context.Background(), discovery.PlannerRequest{
		BatchID: batchID,
		TraceID: "trace-123",
		Targets: []discovery.PlannerTarget{
			{SourceID: 10, URL: "https://example.com/search"},
			{SourceID: 20, URL: "https://example.org/search", Site: "news.example.org"},
		},
	})
	require.NoError(t, err)
	require.Equal(t, 2, result.SeedContents)
	require.Equal(t, 2, result.Extractions)
	require.Equal(t, 2, result.UniquePhrases)
	require.Equal(t, 4, result.TasksCreated)
	require.Len(t, created, 4)
	for _, arg := range created {
		require.Equal(t, batchID, arg.BatchID)
		require.Equal(t, TaskKindDirectoryFetch, arg.Kind)
		require.Equal(t, SourceTypeMedia, arg.SourceType)
		require.Equal(t, "trace-123", arg.TraceID)
		require.NotEmpty(t, arg.Payload)
	}
}

func TestPlannerPlanReturnsNoSeedContents(t *testing.T) {
	extractor := discoverymocks.NewMockExtractor(t)
	tasks := repomocks.NewMockTasks(t)
	pipeline := repomocks.NewMockPipeline(t)
	batchID := uuid.Must(uuid.NewV7())

	p, err := New(testPlannerLogger(), noop.NewTracerProvider().Tracer("test"), extractor, tasks, pipeline)
	require.NoError(t, err)

	pipeline.EXPECT().ListContentsByBatchID(mock.Anything, batchID).Return(nil, nil)

	_, err = p.Plan(context.Background(), discovery.PlannerRequest{
		BatchID: batchID,
		TraceID: "trace-123",
		Targets: []discovery.PlannerTarget{{SourceID: 1, URL: "https://example.com/search"}},
	})
	require.ErrorIs(t, err, ErrNoSeedContents)
}

func testPlannerLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
