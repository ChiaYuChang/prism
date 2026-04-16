package sink_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/discovery/sink"
	"github.com/ChiaYuChang/prism/internal/model"
	"github.com/ChiaYuChang/prism/internal/repo"
	repomocks "github.com/ChiaYuChang/prism/internal/repo/mocks"
	"github.com/ChiaYuChang/prism/pkg/testutils"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestPersistingCandidateSinkHandleUsesRequestDefaults(t *testing.T) {
	scoutRepo := repomocks.NewMockScout(t)
	tasksRepo := repomocks.NewMockTasks(t)
	s, err := sink.NewPersistingCandidateSink(
		testutils.Logger(),
		noop.NewTracerProvider().Tracer("test"),
		scoutRepo,
		tasksRepo,
	)
	require.NoError(t, err)

	batchID := uuid.MustParse("018fef42-4df1-7e68-98fb-e6d4f6b3d9e1")
	publishedAt := time.Date(2026, 4, 4, 9, 0, 0, 0, time.UTC)

	var got repo.UpsertCandidateParams
	scoutRepo.On("UpsertCandidate", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			got = args.Get(1).(repo.UpsertCandidateParams)
		}).
		Return(repo.Candidate{}, nil).
		Once()

	err = s.Handle(context.Background(), sink.CandidateSinkRequest{
		SourceURL:       "https://example.com/listing",
		SourceAbbr:      "kmt",
		SourceType:      "MEDIA",
		BatchID:         batchID,
		TraceID:         "trace-default",
		IngestionMethod: repo.IngestionMethodDirectory,
		DefaultMetadata: map[string]any{
			"source_url": "https://example.com/listing",
		},
		Candidates: []model.Candidates{
			{
				URL:         "https://example.com/a",
				Title:       "Example",
				Description: "Desc",
				PublishedAt: publishedAt,
				Metadata: map[string]any{
					"item": "candidate",
				},
			},
		},
	})
	require.NoError(t, err)

	require.NotEmpty(t, got.BatchID)
	require.Equal(t, batchID, got.BatchID)
	require.Equal(t, "kmt", got.SourceAbbr)
	require.Equal(t, "https://example.com/a", got.URL)
	require.Equal(t, "Example", got.Title)
	require.NotNil(t, got.Description)
	require.Equal(t, "Desc", *got.Description)
	require.NotNil(t, got.PublishedAt)
	require.Equal(t, publishedAt, *got.PublishedAt)
	require.Equal(t, "trace-default", got.TraceID)
	require.Equal(t, repo.IngestionMethodDirectory, got.IngestionMethod)
	require.Equal(t, model.Candidates{
		URL:         "https://example.com/a",
		Title:       "Example",
		Description: "Desc",
		PublishedAt: publishedAt,
	}.Fingerprint(), got.Fingerprint)

	var metadata map[string]any
	require.NoError(t, json.Unmarshal(got.Metadata, &metadata))
	require.Equal(t, "https://example.com/listing", metadata["source_url"])
	require.Equal(t, "candidate", metadata["item"])
}

func TestPersistingCandidateSinkHandleUsesCandidateOverrides(t *testing.T) {
	scoutRepo := repomocks.NewMockScout(t)
	tasksRepo := repomocks.NewMockTasks(t)
	s, err := sink.NewPersistingCandidateSink(
		testutils.Logger(),
		noop.NewTracerProvider().Tracer("test"),
		scoutRepo,
		tasksRepo,
	)
	require.NoError(t, err)

	candidateBatchID := uuid.MustParse("018fef42-4df1-7e68-98fb-e6d4f6b3d9e2")

	var got repo.UpsertCandidateParams
	scoutRepo.On("UpsertCandidate", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			got = args.Get(1).(repo.UpsertCandidateParams)
		}).
		Return(repo.Candidate{}, nil).
		Once()

	err = s.Handle(context.Background(), sink.CandidateSinkRequest{
		SourceAbbr: "dpp",
		TraceID:    "trace-default",
		Candidates: []model.Candidates{
			{
				BatchID:         candidateBatchID,
				SourceAbbr:      "pts",
				TraceID:         "trace-candidate",
				URL:             "https://example.com/b",
				Title:           "Override",
				IngestionMethod: repo.IngestionMethodSearch,
			},
		},
	})
	require.NoError(t, err)

	require.NotEmpty(t, got.BatchID)
	require.Equal(t, candidateBatchID, got.BatchID)
	require.Equal(t, "pts", got.SourceAbbr)
	require.Equal(t, "trace-candidate", got.TraceID)
	require.Equal(t, repo.IngestionMethodSearch, got.IngestionMethod)
}

func TestPersistingCandidateSinkHandleReturnsErrorWhenSourceIDMissing(t *testing.T) {
	scoutRepo := repomocks.NewMockScout(t)
	tasksRepo := repomocks.NewMockTasks(t)
	s, err := sink.NewPersistingCandidateSink(
		testutils.Logger(),
		noop.NewTracerProvider().Tracer("test"),
		scoutRepo,
		tasksRepo,
	)
	require.NoError(t, err)

	err = s.Handle(context.Background(), sink.CandidateSinkRequest{
		TraceID: "trace-default",
		Candidates: []model.Candidates{
			{URL: "https://example.com/a", Title: "Example"},
		},
	})
	require.ErrorIs(t, err, sink.ErrMissingSourceID)
}

func TestPersistingCandidateSinkHandleReturnsErrorWhenTraceIDMissing(t *testing.T) {
	scoutRepo := repomocks.NewMockScout(t)
	tasksRepo := repomocks.NewMockTasks(t)
	s, err := sink.NewPersistingCandidateSink(
		testutils.Logger(),
		noop.NewTracerProvider().Tracer("test"),
		scoutRepo,
		tasksRepo,
	)
	require.NoError(t, err)

	err = s.Handle(context.Background(), sink.CandidateSinkRequest{
		SourceAbbr: "dpp",
		Candidates: []model.Candidates{
			{URL: "https://example.com/a", Title: "Example"},
		},
	})
	require.ErrorIs(t, err, sink.ErrMissingTraceID)
}

func TestPersistingCandidateSinkCreatesPageFetchTaskForPartySource(t *testing.T) {
	scoutRepo := repomocks.NewMockScout(t)
	tasksRepo := repomocks.NewMockTasks(t)
	s, err := sink.NewPersistingCandidateSink(
		testutils.Logger(),
		noop.NewTracerProvider().Tracer("test"),
		scoutRepo,
		tasksRepo,
	)
	require.NoError(t, err)

	candidateID := uuid.MustParse("018fef42-4df1-7e68-98fb-e6d4f6b3d9e3")
	batchID := uuid.MustParse("018fef42-4df1-7e68-98fb-e6d4f6b3d9e1")

	scoutRepo.On("UpsertCandidate", mock.Anything, mock.Anything).
		Return(repo.Candidate{
			ID:              candidateID,
			BatchID:         batchID,
			SourceAbbr:      "dpp",
			URL:             "https://example.com/a",
			TraceID:         "trace-default",
			IngestionMethod: repo.IngestionMethodDirectory,
		}, nil).
		Once()

	var gotParams repo.CreateTaskParams
	tasksRepo.On("CreateTask", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			gotParams = args.Get(1).(repo.CreateTaskParams)
		}).
		Return(repo.Task{}, nil).
		Once()

	err = s.Handle(context.Background(), sink.CandidateSinkRequest{
		SourceURL:       "https://example.com/listing",
		SourceAbbr:      "dpp",
		SourceType:      "PARTY",
		BatchID:         batchID,
		TraceID:         "trace-default",
		IngestionMethod: repo.IngestionMethodDirectory,
		Candidates: []model.Candidates{
			{
				URL:   "https://example.com/a",
				Title: "Example",
			},
		},
	})
	require.NoError(t, err)
	require.Equal(t, repo.TaskKindPageFetch, gotParams.Kind)
	require.Equal(t, "PARTY", gotParams.SourceType)
	require.Equal(t, "https://example.com/a", gotParams.URL)
	require.Equal(t, "trace-default", gotParams.TraceID)
	require.Equal(t, batchID, gotParams.BatchID)
}
