package backfiller_test

import (
	"context"
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/discovery"
	"github.com/ChiaYuChang/prism/internal/discovery/backfiller"
	"github.com/ChiaYuChang/prism/internal/discovery/backfiller/mocks"
	discoverymocks "github.com/ChiaYuChang/prism/internal/discovery/mocks"
	"github.com/ChiaYuChang/prism/internal/model"
	"github.com/ChiaYuChang/prism/pkg/testutils"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestBackfillerTimeout(t *testing.T) {
	scout := discoverymocks.NewMockScout(t)
	pager := mocks.NewMockPager(t)
	sink := mocks.NewMockSink(t)

	pageURL := "https://example.com/page-1"
	pager.On("Next", mock.Anything).Return(pageURL, nil)

	// Scout will take 100ms
	scout.On("Discover", mock.Anything, pageURL).
		Run(func(args mock.Arguments) {
			ctx := args.Get(0).(context.Context)
			select {
			case <-time.After(100 * time.Millisecond):
			case <-ctx.Done():
			}
		}).Return(nil, context.DeadlineExceeded)

	// Set timeout shorter than scout delay
	runner, err := backfiller.New(
		testutils.Logger(),
		noop.NewTracerProvider().Tracer("test"),
		scout, pager, sink, 10*time.Millisecond)
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
	sink := mocks.NewMockSink(t)

	pageURL := "https://example.com/page-1"
	pager.On("Next", mock.Anything).Return(pageURL, nil)

	// Scout will take 100ms
	scout.On("Discover", mock.Anything, pageURL).
		Run(func(args mock.Arguments) {
			ctx := args.Get(0).(context.Context)
			select {
			case <-time.After(100 * time.Millisecond):
			case <-ctx.Done():
			}
		}).Return(nil, context.DeadlineExceeded)

	// Backfiller has no internal timeout
	runner, err := backfiller.New(
		testutils.Logger(),
		noop.NewTracerProvider().Tracer("test"),
		scout, pager, sink, 0)
	require.NoError(t, err)

	// Context has 15ms timeout
	ctx, cancel := context.WithTimeout(
		context.Background(),
		15*time.Millisecond)
	defer cancel()

	_, err = runner.Run(ctx, discovery.BackfillRequest{Until: time.Now()})
	require.Error(t, err)
	require.Contains(t, err.Error(), context.DeadlineExceeded.Error())
}

func TestRunnerRunStopsWhenOldestBeforeUntil(t *testing.T) {
	until := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	page1 := "https://example.com/page-1"
	page2 := "https://example.com/page-2"

	scout := discoverymocks.NewMockScout(t)
	pager := mocks.NewMockPager(t)
	sink := mocks.NewMockSink(t)

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

	sink.On("Handle", mock.Anything, page1,
		mock.MatchedBy(func(in []model.Candidates) bool {
			return len(in) == 2
		})).Return(nil).Once()

	sink.On("Handle", mock.Anything, page2,
		mock.MatchedBy(func(in []model.Candidates) bool {
			return len(in) == 1 && in[0].URL == "https://example.com/c"
		})).Return(nil).Once()

	runner, err := backfiller.New(
		testutils.Logger(),
		noop.NewTracerProvider().Tracer("test"),
		scout, pager, sink, 0)
	require.NoError(t, err)

	result, err := runner.Run(
		context.Background(),
		discovery.BackfillRequest{Until: until})
	require.NoError(t, err)
	require.Equal(t, 2, result.PagesVisited)
	require.Equal(t, 4, result.CandidatesSeen)
	require.Equal(t, 3, result.CandidatesProcessed)
	require.NotNil(t, result.OldestPublishedAt)
	require.Equal(t, oldest, result.OldestPublishedAt)
}
