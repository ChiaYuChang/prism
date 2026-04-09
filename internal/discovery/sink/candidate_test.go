package sink

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/message"
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
	sink, err := NewPersistingCandidateSink(
		testutils.Logger(),
		noop.NewTracerProvider().Tracer("test"),
		scoutRepo,
		nil,
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

	err = sink.Handle(context.Background(), CandidateSinkRequest{
		SourceURL:       "https://example.com/listing",
		SourceID:        3,
		SourceType:      "MEDIA",
		BatchID:         batchID,
		TraceID:         "trace-default",
		IngestionMethod: "DIRECTORY",
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
	require.Equal(t, int32(3), got.SourceID)
	require.Equal(t, "https://example.com/a", got.URL)
	require.Equal(t, "Example", got.Title)
	require.NotNil(t, got.Description)
	require.Equal(t, "Desc", *got.Description)
	require.NotNil(t, got.PublishedAt)
	require.Equal(t, publishedAt, *got.PublishedAt)
	require.Equal(t, "trace-default", got.TraceID)
	require.Equal(t, "DIRECTORY", got.IngestionMethod)
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
	sink, err := NewPersistingCandidateSink(
		testutils.Logger(),
		noop.NewTracerProvider().Tracer("test"),
		scoutRepo,
		nil,
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

	err = sink.Handle(context.Background(), CandidateSinkRequest{
		SourceID: 1,
		TraceID:  "trace-default",
		Candidates: []model.Candidates{
			{
				BatchID:         candidateBatchID,
				SourceID:        7,
				TraceID:         "trace-candidate",
				URL:             "https://example.com/b",
				Title:           "Override",
				IngestionMethod: "SEARCH",
			},
		},
	})
	require.NoError(t, err)

	require.NotEmpty(t, got.BatchID)
	require.Equal(t, candidateBatchID, got.BatchID)
	require.Equal(t, int32(7), got.SourceID)
	require.Equal(t, "trace-candidate", got.TraceID)
	require.Equal(t, "SEARCH", got.IngestionMethod)
}

func TestPersistingCandidateSinkHandleReturnsErrorWhenSourceIDMissing(t *testing.T) {
	scoutRepo := repomocks.NewMockScout(t)
	sink, err := NewPersistingCandidateSink(
		testutils.Logger(),
		noop.NewTracerProvider().Tracer("test"),
		scoutRepo,
		nil,
	)
	require.NoError(t, err)

	err = sink.Handle(context.Background(), CandidateSinkRequest{
		TraceID: "trace-default",
		Candidates: []model.Candidates{
			{URL: "https://example.com/a", Title: "Example"},
		},
	})
	require.ErrorIs(t, err, ErrMissingSourceID)
}

func TestPersistingCandidateSinkHandleReturnsErrorWhenTraceIDMissing(t *testing.T) {
	scoutRepo := repomocks.NewMockScout(t)
	sink, err := NewPersistingCandidateSink(
		testutils.Logger(),
		noop.NewTracerProvider().Tracer("test"),
		scoutRepo,
		nil,
	)
	require.NoError(t, err)

	err = sink.Handle(context.Background(), CandidateSinkRequest{
		SourceID: 1,
		Candidates: []model.Candidates{
			{URL: "https://example.com/a", Title: "Example"},
		},
	})
	require.ErrorIs(t, err, ErrMissingTraceID)
}

func TestPersistingCandidateSinkPublishesPageFetchForPartySource(t *testing.T) {
	scoutRepo := repomocks.NewMockScout(t)
	publisher := &stubPageFetchPublisher{}
	sink, err := NewPersistingCandidateSink(
		testutils.Logger(),
		noop.NewTracerProvider().Tracer("test"),
		scoutRepo,
		publisher,
	)
	require.NoError(t, err)

	candidateID := uuid.MustParse("018fef42-4df1-7e68-98fb-e6d4f6b3d9e3")
	batchID := uuid.MustParse("018fef42-4df1-7e68-98fb-e6d4f6b3d9e1")

	scoutRepo.On("UpsertCandidate", mock.Anything, mock.Anything).
		Return(repo.Candidate{
			ID:              candidateID,
			BatchID:         batchID,
			SourceID:        3,
			URL:             "https://example.com/a",
			TraceID:         "trace-default",
			IngestionMethod: "DIRECTORY",
		}, nil).
		Once()

	err = sink.Handle(context.Background(), CandidateSinkRequest{
		SourceURL:       "https://example.com/listing",
		SourceID:        3,
		SourceType:      "PARTY",
		BatchID:         batchID,
		TraceID:         "trace-default",
		IngestionMethod: "DIRECTORY",
		Candidates: []model.Candidates{
			{
				URL:   "https://example.com/a",
				Title: "Example",
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, publisher.signals, 1)
	require.Equal(t, candidateID, publisher.signals[0].CandidateID)
	require.Equal(t, "PARTY", publisher.signals[0].SourceType)
	require.Equal(t, "https://example.com/a", publisher.signals[0].URL)
}

type stubPageFetchPublisher struct {
	signals []*message.PageFetchSignal
	err     error
}

func (s *stubPageFetchPublisher) PublishPageFetch(_ context.Context, sig *message.PageFetchSignal) error {
	s.signals = append(s.signals, sig)
	return s.err
}
