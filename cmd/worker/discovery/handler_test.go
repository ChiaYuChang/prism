package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/ChiaYuChang/prism/internal/discovery"
	discoverymocks "github.com/ChiaYuChang/prism/internal/discovery/mocks"
	"github.com/ChiaYuChang/prism/internal/discovery/planner"
	discoverysink "github.com/ChiaYuChang/prism/internal/discovery/sink"
	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/model"
	"github.com/ChiaYuChang/prism/internal/repo"
	repomocks "github.com/ChiaYuChang/prism/internal/repo/mocks"
	wm "github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestHandlerHandleMessageCompletesTask(t *testing.T) {
	taskID := uuid.Must(uuid.NewV7())
	batchID := uuid.Must(uuid.NewV7())

	scout := discoverymocks.NewMockScout(t)
	scoutRepo := repomocks.NewMockScout(t)
	scheduler := repomocks.NewMockScheduler(t)
	sink := &stubCandidateSink{}

	h, err := NewHandler(testLogger(), noop.NewTracerProvider().Tracer("test"), scout, nil, sink, scoutRepo, scheduler)
	require.NoError(t, err)

	source := repo.Source{
		Abbr:    "dpp",
		Type:    repo.SourceTypeParty,
		BaseURL: "https://www.dpp.org.tw",
	}
	candidates := []model.Candidates{{Title: "A", URL: "https://www.dpp.org.tw/article/1"}}

	scoutRepo.EXPECT().GetSourceByAbbr(mock.Anything, "dpp").Return(source, nil)
	scout.EXPECT().Discover(mock.Anything, "https://www.dpp.org.tw/media/00").Return(candidates, nil)
	scheduler.EXPECT().CompleteTask(mock.Anything, taskID).Return(nil)

	payload, err := (&message.TaskSignal{
		TaskID:     taskID,
		BatchID:    batchID,
		Kind:       repo.TaskKindDirectoryFetch,
		SourceType: repo.SourceTypeParty,
		SourceAbbr: "dpp",
		URL:        "https://www.dpp.org.tw/media/00",
		TraceID:    "trace-123",
	}).Marshal()
	require.NoError(t, err)

	ack, err := h.HandleMessage(context.Background(), wm.NewMessage("id", payload))
	require.NoError(t, err)
	require.True(t, ack)
	require.NotNil(t, sink.last)
	require.Equal(t, "dpp", sink.last.SourceAbbr)
	require.Equal(t, repo.SourceTypeParty, sink.last.SourceType)
	require.Equal(t, "DIRECTORY", sink.last.IngestionMethod)
	require.Equal(t, batchID, sink.last.BatchID)
}

func TestHandlerHandleMessageIgnoresUnsupportedTask(t *testing.T) {
	taskID := uuid.Must(uuid.NewV7())

	scout := discoverymocks.NewMockScout(t)
	scoutRepo := repomocks.NewMockScout(t)
	scheduler := repomocks.NewMockScheduler(t)
	sink := &stubCandidateSink{}

	h, err := NewHandler(
		testLogger(),
		noop.NewTracerProvider().Tracer("test"),
		scout,
		nil,
		sink,
		scoutRepo,
		scheduler)
	require.NoError(t, err)

	payload, err := (&message.TaskSignal{
		TaskID:     taskID,
		BatchID:    uuid.Must(uuid.NewV7()),
		Kind:       "PAGE_FETCH",
		SourceType: repo.SourceTypeParty,
		SourceAbbr: "dpp",
		URL:        "https://www.dpp.org.tw/media/00",
		TraceID:    "trace-123",
	}).Marshal()
	require.NoError(t, err)

	ack, err := h.HandleMessage(context.Background(), wm.NewMessage("id", payload))
	require.NoError(t, err)
	require.True(t, ack)
	require.Nil(t, sink.last)
}

func TestHandlerHandleMessageNacksWhenCompleteFails(t *testing.T) {
	taskID := uuid.Must(uuid.NewV7())
	batchID := uuid.Must(uuid.NewV7())

	scout := discoverymocks.NewMockScout(t)
	scoutRepo := repomocks.NewMockScout(t)
	scheduler := repomocks.NewMockScheduler(t)
	sink := &stubCandidateSink{}

	h, err := NewHandler(
		testLogger(),
		noop.NewTracerProvider().Tracer("test"),
		scout,
		nil,
		sink,
		scoutRepo,
		scheduler,
	)
	require.NoError(t, err)

	source := repo.Source{
		Abbr:    "dpp",
		Type:    repo.SourceTypeParty,
		BaseURL: "https://www.dpp.org.tw",
	}

	scoutRepo.EXPECT().
		GetSourceByAbbr(mock.Anything, "dpp").
		Return(source, nil)

	scout.EXPECT().
		Discover(mock.Anything, "https://www.dpp.org.tw/media/00").
		Return([]model.Candidates{}, nil)

	scheduler.EXPECT().
		CompleteTask(mock.Anything, taskID).
		Return(errors.New("db down"))

	payload, err := (&message.TaskSignal{
		TaskID:     taskID,
		BatchID:    batchID,
		Kind:       repo.TaskKindDirectoryFetch,
		SourceType: repo.SourceTypeParty,
		SourceAbbr: "dpp",
		URL:        "https://www.dpp.org.tw/media/00",
		TraceID:    "trace-123",
	}).Marshal()
	require.NoError(t, err)

	ack, err := h.HandleMessage(context.Background(), wm.NewMessage("id", payload))
	require.Error(t, err)
	require.False(t, ack)
}

type stubCandidateSink struct {
	last *discoverysink.CandidateSinkRequest
	err  error
}

func (s *stubCandidateSink) Handle(_ context.Context, req discoverysink.CandidateSinkRequest) error {
	s.last = &req
	return s.err
}

func TestHandlerHandleMessageKeywordSearch(t *testing.T) {
	taskID := uuid.Must(uuid.NewV7())
	batchID := uuid.Must(uuid.NewV7())

	scout := discoverymocks.NewMockScout(t)
	searchClient := discoverymocks.NewMockSearchClient(t)
	scoutRepo := repomocks.NewMockScout(t)
	scheduler := repomocks.NewMockScheduler(t)
	sink := &stubCandidateSink{}

	searchProviders := map[string]discovery.SearchClient{
		"brave": searchClient,
	}

	h, err := NewHandler(
		testLogger(),
		noop.NewTracerProvider().Tracer("test"),
		scout,
		searchProviders,
		sink,
		scoutRepo,
		scheduler,
	)
	require.NoError(t, err)

	searchClient.EXPECT().
		DiscoverNews(mock.Anything, "台灣半導體政策", "cna.com.tw").
		Return([]model.Candidates{
			{Title: "TSMC expands", URL: "https://example.com/tsmc"},
			{Title: "Chip policy", URL: "https://example.com/chip"},
		}, nil)
	scheduler.EXPECT().CompleteTask(mock.Anything, taskID).Return(nil)

	payloadBytes, err := json.Marshal(planner.MediaTaskPayload{
		Query: "台灣半導體政策",
		Site:  "cna.com.tw",
	})
	require.NoError(t, err)

	sigPayload, err := (&message.TaskSignal{
		TaskID:     taskID,
		BatchID:    batchID,
		Kind:       repo.TaskKindKeywordSearch,
		SourceType: repo.SourceTypeMedia,
		SourceAbbr: "yahoo",
		URL:        "https://tw.news.yahoo.com",
		Payload:    payloadBytes,
		TraceID:    "trace-kw-123",
	}).Marshal()
	require.NoError(t, err)

	ack, err := h.HandleMessage(context.Background(), wm.NewMessage("id", sigPayload))
	require.NoError(t, err)
	require.True(t, ack)
	require.NotNil(t, sink.last)
	require.Equal(t, "yahoo", sink.last.SourceAbbr)
	require.Equal(t, repo.SourceTypeMedia, sink.last.SourceType)
	require.Equal(t, "SEARCH", sink.last.IngestionMethod)
	require.Equal(t, batchID, sink.last.BatchID)
	require.Len(t, sink.last.Candidates, 2)
	require.Equal(t, "TSMC expands", sink.last.Candidates[0].Title)
	require.Equal(t, "brave", sink.last.Candidates[0].Metadata["search_provider"])
}

func TestHandlerHandleMessageKeywordSearchNoProviders(t *testing.T) {
	taskID := uuid.Must(uuid.NewV7())

	scout := discoverymocks.NewMockScout(t)
	scoutRepo := repomocks.NewMockScout(t)
	scheduler := repomocks.NewMockScheduler(t)
	sink := &stubCandidateSink{}

	h, err := NewHandler(
		testLogger(),
		noop.NewTracerProvider().Tracer("test"),
		scout,
		nil,
		sink,
		scoutRepo,
		scheduler,
	)
	require.NoError(t, err)

	payloadBytes, err := json.Marshal(planner.MediaTaskPayload{Query: "test"})
	require.NoError(t, err)

	sigPayload, err := (&message.TaskSignal{
		TaskID:     taskID,
		BatchID:    uuid.Must(uuid.NewV7()),
		Kind:       repo.TaskKindKeywordSearch,
		SourceType: repo.SourceTypeMedia,
		SourceAbbr: "unknown-source",
		URL:        "https://example.com",
		Payload:    payloadBytes,
		TraceID:    "trace-kw-404",
	}).Marshal()
	require.NoError(t, err)

	scheduler.EXPECT().
		FailTask(mock.Anything, taskID).
		Return(nil)

	ack, err := h.HandleMessage(context.Background(), wm.NewMessage("id", sigPayload))
	require.Error(t, err)
	require.True(t, ack)
}

func TestHandlerHandleMessageKeywordSearchCompletesWhenOneProviderSucceeds(t *testing.T) {
	taskID := uuid.Must(uuid.NewV7())
	batchID := uuid.Must(uuid.NewV7())

	scout := discoverymocks.NewMockScout(t)
	braveClient := discoverymocks.NewMockSearchClient(t)
	googleClient := discoverymocks.NewMockSearchClient(t)
	scoutRepo := repomocks.NewMockScout(t)
	scheduler := repomocks.NewMockScheduler(t)
	sink := &stubCandidateSink{}

	h, err := NewHandler(
		testLogger(),
		noop.NewTracerProvider().Tracer("test"),
		scout,
		map[string]discovery.SearchClient{
			"brave":      braveClient,
			"google-cse": googleClient,
		},
		sink,
		scoutRepo,
		scheduler,
	)
	require.NoError(t, err)

	braveClient.EXPECT().
		DiscoverNews(mock.Anything, "台灣半導體政策", "tw.news.yahoo.com").
		Return(nil, errors.New("rate limited"))

	googleClient.EXPECT().
		DiscoverNews(mock.Anything, "台灣半導體政策", "tw.news.yahoo.com").
		Return([]model.Candidates{
			{Title: "Yahoo article", URL: "https://tw.news.yahoo.com/a"},
		}, nil)

	scheduler.EXPECT().
		CompleteTask(mock.Anything, taskID).
		Return(nil)

	payloadBytes, err := json.Marshal(planner.MediaTaskPayload{Query: "台灣半導體政策", Site: "tw.news.yahoo.com"})
	require.NoError(t, err)
	sigPayload, err := (&message.TaskSignal{
		TaskID:     taskID,
		BatchID:    batchID,
		Kind:       repo.TaskKindKeywordSearch,
		SourceType: repo.SourceTypeMedia,
		SourceAbbr: "yahoo",
		URL:        "https://tw.news.yahoo.com",
		Payload:    payloadBytes,
		TraceID:    "trace-kw-123",
	}).Marshal()
	require.NoError(t, err)

	ack, err := h.HandleMessage(context.Background(), wm.NewMessage("id", sigPayload))
	require.NoError(t, err)
	require.True(t, ack)
	require.NotNil(t, sink.last)
	require.Equal(t, "yahoo", sink.last.SourceAbbr)
	require.Len(t, sink.last.Candidates, 1)
	require.Equal(t, "google-cse", sink.last.Candidates[0].Metadata["search_provider"])
}

func TestHandlerHandleMessageDirectoryFetchMedia(t *testing.T) {
	taskID := uuid.Must(uuid.NewV7())
	batchID := uuid.Must(uuid.NewV7())

	scout := discoverymocks.NewMockScout(t)
	scoutRepo := repomocks.NewMockScout(t)
	scheduler := repomocks.NewMockScheduler(t)
	sink := &stubCandidateSink{}

	h, err := NewHandler(testLogger(), noop.NewTracerProvider().Tracer("test"), scout, nil, sink, scoutRepo, scheduler)
	require.NoError(t, err)

	source := repo.Source{Abbr: "cna", Type: repo.SourceTypeMedia, BaseURL: "https://www.cna.com.tw"}
	scoutRepo.EXPECT().GetSourceByAbbr(mock.Anything, "cna").Return(source, nil)
	scout.EXPECT().Discover(mock.Anything, "https://www.cna.com.tw/rss/aipl.xml").Return([]model.Candidates{
		{Title: "CNA article", URL: "https://www.cna.com.tw/news/aipl/1.aspx"},
	}, nil)
	scheduler.EXPECT().CompleteTask(mock.Anything, taskID).Return(nil)

	payload, err := (&message.TaskSignal{
		TaskID:     taskID,
		BatchID:    batchID,
		Kind:       repo.TaskKindDirectoryFetch,
		SourceType: repo.SourceTypeMedia,
		SourceAbbr: "cna",
		URL:        "https://www.cna.com.tw/rss/aipl.xml",
		TraceID:    "trace-media-dir",
	}).Marshal()
	require.NoError(t, err)

	ack, err := h.HandleMessage(context.Background(), wm.NewMessage("id", payload))
	require.NoError(t, err)
	require.True(t, ack)
	require.NotNil(t, sink.last)
	require.Equal(t, "cna", sink.last.SourceAbbr)
	require.Equal(t, repo.SourceTypeMedia, sink.last.SourceType)
	require.Equal(t, repo.IngestionMethodDirectory, sink.last.IngestionMethod)
}

func TestHandlerHandleMessageRecordsMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, meterProvider.Shutdown(context.Background())) })
	metrics, err := newMetrics(meterProvider.Meter("test"))
	require.NoError(t, err)

	scout := discoverymocks.NewMockScout(t)
	scoutRepo := repomocks.NewMockScout(t)
	scheduler := repomocks.NewMockScheduler(t)
	sink := &stubCandidateSink{}

	h, err := NewHandler(testLogger(), noop.NewTracerProvider().Tracer("test"), scout, nil, sink, scoutRepo, scheduler, metrics)
	require.NoError(t, err)

	okTaskID := uuid.Must(uuid.NewV7())
	failTaskID := uuid.Must(uuid.NewV7())
	failedErr := errors.New("site down")
	source := repo.Source{Abbr: "dpp", Type: repo.SourceTypeParty, BaseURL: "https://www.dpp.org.tw"}
	scoutRepo.EXPECT().GetSourceByAbbr(mock.Anything, "dpp").Return(source, nil).Twice()
	scout.EXPECT().Discover(mock.Anything, "https://www.dpp.org.tw/media/ok").Return([]model.Candidates{}, nil)
	scout.EXPECT().Discover(mock.Anything, "https://www.dpp.org.tw/media/fail").Return(nil, failedErr)
	scheduler.EXPECT().CompleteTask(mock.Anything, okTaskID).Return(nil)
	scheduler.EXPECT().FailTask(mock.Anything, failTaskID).Return(nil)

	tcs := []struct {
		name        string
		taskID      uuid.UUID
		kind        string
		sourceType  string
		rawURL      string
		expectedAck bool
		expectedErr error
	}{
		{
			name:        "ok",
			taskID:      okTaskID,
			kind:        repo.TaskKindDirectoryFetch,
			sourceType:  repo.SourceTypeParty,
			rawURL:      "https://www.dpp.org.tw/media/ok",
			expectedAck: true,
			expectedErr: nil,
		},
		{
			name:        "ignored",
			taskID:      uuid.Must(uuid.NewV7()),
			kind:        repo.TaskKindPageFetch,
			sourceType:  repo.SourceTypeParty,
			rawURL:      "https://www.dpp.org.tw/media/ignored",
			expectedAck: true,
			expectedErr: nil,
		},
		{
			name:        "failed",
			taskID:      failTaskID,
			kind:        repo.TaskKindDirectoryFetch,
			sourceType:  repo.SourceTypeParty,
			rawURL:      "https://www.dpp.org.tw/media/fail",
			expectedAck: true,
			expectedErr: failedErr,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			payload := discoveryTaskPayload(t, tc.taskID, tc.kind, tc.sourceType, tc.rawURL)
			ack, err := h.HandleMessage(context.Background(), wm.NewMessage(tc.name, payload))
			require.Equal(t, tc.expectedAck, ack)
			if tc.expectedErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tc.expectedErr)
			} else {
				require.NoError(t, err)
			}
		})
	}

	rm := collectDiscoveryMetrics(t, reader)
	for _, tc := range tcs {
		require.Equal(t, int64(1), discoveryCounterValue(t, rm, "result", tc.name))
	}
	require.Equal(t, int64(len(tcs)), discoveryCounterTotal(t, rm))
	require.Equal(t, uint64(len(tcs)), discoveryHistogramCount(t, rm, "prism.discovery.task.duration"))
}

// discoveryTaskPayload returns a marshaled TaskSignal payload for testing.
func discoveryTaskPayload(t *testing.T, taskID uuid.UUID, kind, sourceType, rawURL string) []byte {
	t.Helper()
	payload, err := (&message.TaskSignal{
		TaskID:     taskID,
		BatchID:    uuid.Must(uuid.NewV7()),
		Kind:       kind,
		SourceType: sourceType,
		SourceAbbr: "dpp",
		URL:        rawURL,
		TraceID:    "trace-metrics",
	}).Marshal()
	require.NoError(t, err)
	return payload
}

// collectDiscoveryMetrics collects discovery task metrics from the reader.
func collectDiscoveryMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	return rm
}

// discoveryCounterValue returns the value of the discovery task counter metric with the given attributes.
func discoveryCounterValue(t *testing.T, rm metricdata.ResourceMetrics, attrKey, attrValue string) int64 {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "prism.discovery.tasks" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, dp := range sum.DataPoints {
				value, found := dp.Attributes.Value(attribute.Key(attrKey))
				if found && value.AsString() == attrValue {
					return dp.Value
				}
			}
		}
	}
	return 0
}

// discoveryCounterTotal returns the total value of the discovery task counter metric.
func discoveryCounterTotal(t *testing.T, rm metricdata.ResourceMetrics) int64 {
	t.Helper()
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "prism.discovery.tasks" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, dp := range sum.DataPoints {
				total += dp.Value
			}
		}
	}
	return total
}

// discoveryHistogramCount returns the count of the discovery task histogram metric with the given name.
func discoveryHistogramCount(t *testing.T, rm metricdata.ResourceMetrics, name string) uint64 {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			histogram, ok := m.Data.(metricdata.Histogram[float64])
			require.True(t, ok)
			var count uint64
			for _, dp := range histogram.DataPoints {
				count += dp.Count
			}
			return count
		}
	}
	return 0
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
