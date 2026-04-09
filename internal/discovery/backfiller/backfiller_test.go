package backfiller_test

import (
	"context"
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/discovery"
	"github.com/ChiaYuChang/prism/internal/discovery/backfiller"
	"github.com/ChiaYuChang/prism/internal/discovery/backfiller/mocks"
	discoverymocks "github.com/ChiaYuChang/prism/internal/discovery/mocks"
	discoverysink "github.com/ChiaYuChang/prism/internal/discovery/sink"
	"github.com/ChiaYuChang/prism/internal/model"
	"github.com/ChiaYuChang/prism/pkg/testutils"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

type stubCandidateSink struct {
	handle func(ctx context.Context, req discoverysink.CandidateSinkRequest) error
}

func (s stubCandidateSink) Handle(ctx context.Context, req discoverysink.CandidateSinkRequest) error {
	if s.handle != nil {
		return s.handle(ctx, req)
	}
	return nil
}

func TestBackfillerTimeout(t *testing.T) {
	scout := discoverymocks.NewMockScout(t)
	pager := mocks.NewMockPager(t)
	sink := stubCandidateSink{}

	pageURL := "https://example.com/page-1"
	pager.On("Next", mock.Anything).Return(pageURL, nil)

	scout.On("Discover", mock.Anything, pageURL).
		Run(func(args mock.Arguments) {
			ctx := args.Get(0).(context.Context)
			select {
			case <-time.After(100 * time.Millisecond):
			case <-ctx.Done():
			}
		}).Return(nil, context.DeadlineExceeded)

	runner, err := backfiller.New(
		testutils.Logger(),
		noop.NewTracerProvider().Tracer("test"),
		scout, pager, sink, 3, 10*time.Millisecond)
	require.NoError(t, err)

	_, err = runner.Run(
		context.Background(),
		discovery.BackfillRequest{Until: time.Now()})
	require.Error(t, err)
	require.Contains(t, err.Error(), context.DeadlineExceeded.Error())
}

func TestBackfillerGlobalTimeout(t *testing.T) {
	scout := discoverymocks.NewMockScout(t)
	pager := mocks.NewMockPager(t)
	sink := stubCandidateSink{}

	pageURL := "https://example.com/page-1"
	pager.On("Next", mock.Anything).Return(pageURL, nil)

	scout.On("Discover", mock.Anything, pageURL).
		Run(func(args mock.Arguments) {
			ctx := args.Get(0).(context.Context)
			select {
			case <-time.After(100 * time.Millisecond):
			case <-ctx.Done():
			}
		}).Return(nil, context.DeadlineExceeded)

	runner, err := backfiller.New(
		testutils.Logger(),
		noop.NewTracerProvider().Tracer("test"),
		scout, pager, sink, 3, 0)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()

	_, err = runner.Run(ctx, discovery.BackfillRequest{Until: time.Now()})
	require.Error(t, err)
	require.Contains(t, err.Error(), context.DeadlineExceeded.Error())
}

func TestBackfillerRunBuildsCandidateSinkRequest(t *testing.T) {
	until := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	page1 := "https://example.com/page-1"
	page2 := "https://example.com/page-2"

	scout := discoverymocks.NewMockScout(t)
	pager := mocks.NewMockPager(t)

	pager.On("Next", mock.Anything).Return(page1, nil).Once()
	pager.On("Next", mock.Anything).Return(page2, nil).Once()

	oldest := time.Date(2026, 2, 27, 0, 0, 0, 0, time.UTC)
	c1 := []model.Candidates{
		{
			URL:         "https://example.com/a",
			PublishedAt: time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC),
		},
		{
			URL:         "https://example.com/b",
			PublishedAt: time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC),
		},
	}
	c2 := []model.Candidates{
		{
			URL:         "https://example.com/d",
			PublishedAt: oldest,
		},
		{
			URL:         "https://example.com/c",
			PublishedAt: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	scout.On("Discover", mock.Anything, page1).Return(c1, nil).Once()
	scout.On("Discover", mock.Anything, page2).Return(c2, nil).Once()

	var got []discoverysink.CandidateSinkRequest
	batchID := uuid.MustParse("018fef42-4df1-7e68-98fb-e6d4f6b3d9e1")
	sink := stubCandidateSink{
		handle: func(_ context.Context, req discoverysink.CandidateSinkRequest) error {
			got = append(got, req)
			return nil
		},
	}

	runner, err := backfiller.New(
		testutils.Logger(),
		noop.NewTracerProvider().Tracer("test"),
		scout, pager, sink, 3, 0)
	require.NoError(t, err)

	result, err := runner.Run(
		context.Background(),
		discovery.BackfillRequest{BatchID: batchID, Until: until})
	require.NoError(t, err)
	require.Equal(t, 2, result.PagesVisited)
	require.Equal(t, 4, result.CandidatesSeen)
	require.Equal(t, 3, result.CandidatesProcessed)
	require.NotNil(t, result.OldestPublishedAt)
	require.Equal(t, oldest, result.OldestPublishedAt)

	require.Len(t, got, 2)
	require.Equal(t, page1, got[0].SourceURL)
	require.Equal(t, int32(3), got[0].SourceID)
	require.Equal(t, "PARTY", got[0].SourceType)
	require.Equal(t, "DIRECTORY", got[0].IngestionMethod)
	require.NotNil(t, got[0].BatchID)
	require.Equal(t, batchID, got[0].BatchID)
	require.Len(t, got[0].Candidates, 2)
	require.Equal(t, page2, got[1].SourceURL)
	require.Equal(t, int32(3), got[1].SourceID)
	require.Equal(t, "PARTY", got[1].SourceType)
	require.Equal(t, "DIRECTORY", got[1].IngestionMethod)
	require.NotNil(t, got[1].BatchID)
	require.Equal(t, batchID, got[1].BatchID)
	require.Len(t, got[1].Candidates, 1)
	require.Equal(t, "https://example.com/c", got[1].Candidates[0].URL)
}
