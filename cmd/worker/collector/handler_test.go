package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	collector "github.com/ChiaYuChang/prism/internal/collector"
	"github.com/ChiaYuChang/prism/internal/collector/archiver"
	"github.com/ChiaYuChang/prism/internal/collector/mocks"
	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/repo"
	repomocks "github.com/ChiaYuChang/prism/internal/repo/mocks"
	wm "github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/trace/noop"
)

var errContentNotFound = errors.New("content not found")

func TestHandlerProcess_ArchivesStageErrorWithRecoverablePayloadKind(t *testing.T) {
	const (
		url       = "https://example.test/article"
		raw       = "raw-html"
		minified  = "minified-html"
		canonical = "canonical-html"
	)

	tests := []struct {
		name        string
		setup       func(t *testing.T) collector.Pipeline
		wantPayload string
		wantKind    archiver.PayloadKind
		wantStage   collector.PipelineStage
	}{
		{
			name: "minify failure archives raw payload",
			setup: func(t *testing.T) collector.Pipeline {
				fetcher := mocks.NewMockFetcher(t)
				minifier := mocks.NewMockTransformer(t)
				parser := mocks.NewMockParser(t)

				fetcher.EXPECT().Fetch(mock.Anything, url).Return(raw, nil).Once()
				minifier.EXPECT().Transform(mock.Anything, raw).Return("", errors.New("minify failed")).Once()

				return collector.Pipeline{Fetcher: fetcher, Minifier: minifier, Parser: parser}
			},
			wantPayload: raw,
			wantKind:    archiver.PayloadKindRaw,
			wantStage:   collector.PipelineStageMinify,
		},
		{
			name: "transform failure archives minified payload",
			setup: func(t *testing.T) collector.Pipeline {
				fetcher := mocks.NewMockFetcher(t)
				minifier := mocks.NewMockTransformer(t)
				transformer := mocks.NewMockTransformer(t)
				parser := mocks.NewMockParser(t)

				fetcher.EXPECT().Fetch(mock.Anything, url).Return(raw, nil).Once()
				minifier.EXPECT().Transform(mock.Anything, raw).Return(minified, nil).Once()
				transformer.EXPECT().Transform(mock.Anything, minified).Return("", errors.New("transform failed")).Once()

				return collector.Pipeline{Fetcher: fetcher, Minifier: minifier, Transformers: []collector.Transformer{transformer}, Parser: parser}
			},
			wantPayload: minified,
			wantKind:    archiver.PayloadKindMinified,
			wantStage:   collector.PipelineStageTransform,
		},
		{
			name: "parse failure archives canonical payload",
			setup: func(t *testing.T) collector.Pipeline {
				fetcher := mocks.NewMockFetcher(t)
				minifier := mocks.NewMockTransformer(t)
				transformer := mocks.NewMockTransformer(t)
				parser := mocks.NewMockParser(t)

				fetcher.EXPECT().Fetch(mock.Anything, url).Return(raw, nil).Once()
				minifier.EXPECT().Transform(mock.Anything, raw).Return(minified, nil).Once()
				transformer.EXPECT().Transform(mock.Anything, minified).Return(canonical, nil).Once()
				parser.EXPECT().Parse(mock.Anything, url, canonical).Return(nil, errors.New("parse failed")).Once()

				return collector.Pipeline{Fetcher: fetcher, Minifier: minifier, Transformers: []collector.Transformer{transformer}, Parser: parser}
			},
			wantPayload: canonical,
			wantKind:    archiver.PayloadKindCanonical,
			wantStage:   collector.PipelineStageParse,
		},
		{
			name: "invalid parsed article archives canonical payload",
			setup: func(t *testing.T) collector.Pipeline {
				fetcher := mocks.NewMockFetcher(t)
				minifier := mocks.NewMockTransformer(t)
				transformer := mocks.NewMockTransformer(t)
				parser := mocks.NewMockParser(t)

				fetcher.EXPECT().Fetch(mock.Anything, url).Return(raw, nil).Once()
				minifier.EXPECT().Transform(mock.Anything, raw).Return(minified, nil).Once()
				transformer.EXPECT().Transform(mock.Anything, minified).Return(canonical, nil).Once()
				parser.EXPECT().Parse(mock.Anything, url, canonical).Return(&collector.Article{URL: url, Title: ""}, nil).Once()

				return collector.Pipeline{Fetcher: fetcher, Minifier: minifier, Transformers: []collector.Transformer{transformer}, Parser: parser}
			},
			wantPayload: canonical,
			wantKind:    archiver.PayloadKindCanonical,
			wantStage:   collector.PipelineStageParse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			saver := &capturingSaver{}
			h := newTestHandler(t, tt.setup(t), saver)
			batchID := uuid.New()
			sig := message.TaskSignal{
				TaskID:     uuid.New(),
				BatchID:    batchID,
				TraceID:    "trace-123",
				Kind:       repo.TaskKindPageFetch,
				SourceType: repo.SourceTypeParty,
				SourceAbbr: "dpp",
				URL:        url,
			}

			err := h.process(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)), sig)
			require.Error(t, err)
			require.Len(t, saver.records, 1)

			record := saver.records[0]
			assert.Equal(t, url, record.URL)
			assert.Equal(t, tt.wantPayload, record.Payload)
			assert.Equal(t, sig.TraceID, record.TraceID)
			assert.Equal(t, string(tt.wantKind), record.Metadata["kind"])
			assert.Equal(t, tt.wantStage, record.Metadata["recover_from"])
			assert.Equal(t, sig.TraceID, record.Metadata["recover_key"])
			assert.Equal(t, sig.SourceAbbr, record.Metadata["source_abbr"])
			assert.Equal(t, sig.SourceType, record.Metadata["source_type"])
			assert.Equal(t, batchID.String(), record.Metadata["batch_id"])
			assert.NotEmpty(t, record.Metadata["error"])
		})
	}
}

func TestHandlerHandleMessageRecordsMetrics(t *testing.T) {
	tests := []struct {
		name       string
		payload    func(t *testing.T, taskID uuid.UUID) []byte
		pipeline   func(t *testing.T) collector.Pipeline
		reporter   metricsReporter
		wantAck    bool
		wantErr    bool
		wantResult string
	}{
		{
			name: "ok",
			payload: func(t *testing.T, taskID uuid.UUID) []byte {
				return collectorTaskPayload(t, taskID, repo.TaskKindPageFetch, repo.SourceTypeParty)
			},
			pipeline: func(t *testing.T) collector.Pipeline {
				fetcher := mocks.NewMockFetcher(t)
				minifier := mocks.NewMockTransformer(t)
				transformer := mocks.NewMockTransformer(t)
				parser := mocks.NewMockParser(t)
				fetcher.EXPECT().Fetch(mock.Anything, "https://example.test/article").Return("raw", nil).Once()
				minifier.EXPECT().Transform(mock.Anything, "raw").Return("minified", nil).Once()
				transformer.EXPECT().Transform(mock.Anything, "minified").Return("canonical", nil).Once()
				parser.EXPECT().Parse(mock.Anything, "https://example.test/article", "canonical").Return(&collector.Article{Title: "Title", Content: "Body"}, nil).Once()
				return collector.Pipeline{Fetcher: fetcher, Minifier: minifier, Transformers: []collector.Transformer{transformer}, Parser: parser}
			},
			reporter:   metricsReporter{},
			wantAck:    true,
			wantResult: "ok",
		},
		{
			name: "ignored",
			payload: func(t *testing.T, taskID uuid.UUID) []byte {
				return collectorTaskPayload(t, taskID, repo.TaskKindDirectoryFetch, repo.SourceTypeParty)
			},
			pipeline:   func(t *testing.T) collector.Pipeline { return collector.Pipeline{} },
			reporter:   metricsReporter{},
			wantAck:    true,
			wantResult: "ignored",
		},
		{
			name: "fetch_failed",
			payload: func(t *testing.T, taskID uuid.UUID) []byte {
				return collectorTaskPayload(t, taskID, repo.TaskKindPageFetch, repo.SourceTypeParty)
			},
			pipeline: func(t *testing.T) collector.Pipeline {
				fetcher := mocks.NewMockFetcher(t)
				minifier := mocks.NewMockTransformer(t)
				parser := mocks.NewMockParser(t)
				fetcher.EXPECT().Fetch(mock.Anything, "https://example.test/article").Return("", errors.New("fetch failed")).Once()
				return collector.Pipeline{Fetcher: fetcher, Minifier: minifier, Parser: parser}
			},
			reporter:   metricsReporter{},
			wantAck:    true,
			wantErr:    true,
			wantResult: "fetch_failed",
		},
		{
			name: "nacked",
			payload: func(t *testing.T, taskID uuid.UUID) []byte {
				return collectorTaskPayload(t, taskID, repo.TaskKindPageFetch, repo.SourceTypeParty)
			},
			pipeline: func(t *testing.T) collector.Pipeline {
				fetcher := mocks.NewMockFetcher(t)
				minifier := mocks.NewMockTransformer(t)
				transformer := mocks.NewMockTransformer(t)
				parser := mocks.NewMockParser(t)
				fetcher.EXPECT().Fetch(mock.Anything, "https://example.test/article").Return("raw", nil).Once()
				minifier.EXPECT().Transform(mock.Anything, "raw").Return("minified", nil).Once()
				transformer.EXPECT().Transform(mock.Anything, "minified").Return("canonical", nil).Once()
				parser.EXPECT().Parse(mock.Anything, "https://example.test/article", "canonical").Return(&collector.Article{Title: "Title", Content: "Body"}, nil).Once()
				return collector.Pipeline{Fetcher: fetcher, Minifier: minifier, Transformers: []collector.Transformer{transformer}, Parser: parser}
			},
			reporter:   metricsReporter{completeErr: errors.New("db down")},
			wantAck:    false,
			wantErr:    true,
			wantResult: "nacked",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := sdkmetric.NewManualReader()
			meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			t.Cleanup(func() { require.NoError(t, meterProvider.Shutdown(context.Background())) })
			metrics, err := newMetrics(meterProvider.Meter("test"))
			require.NoError(t, err)

			h := newTestHandlerWithReporter(t, tt.pipeline(t), nil, tt.reporter, metrics)
			taskID := uuid.Must(uuid.NewV7())
			ack, err := h.HandleMessage(context.Background(), wm.NewMessage(tt.name, tt.payload(t, taskID)))
			require.Equal(t, tt.wantAck, ack)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			rm := collectCollectorMetrics(t, reader)
			require.Equal(t, int64(1), collectorCounterValue(t, rm, "result", tt.wantResult))
			require.Equal(t, int64(1), collectorCounterTotal(t, rm))
			require.Equal(t, uint64(1), collectorHistogramCount(t, rm, "prism.collector.task.duration"))
		})
	}
}

func collectorTaskPayload(t *testing.T, taskID uuid.UUID, kind, sourceType string) []byte {
	t.Helper()
	payload, err := (&message.TaskSignal{
		TaskID:     taskID,
		BatchID:    uuid.Must(uuid.NewV7()),
		TraceID:    "trace-metrics",
		Kind:       kind,
		SourceType: sourceType,
		SourceAbbr: "dpp",
		URL:        "https://example.test/article",
	}).Marshal()
	require.NoError(t, err)
	return payload
}

func newTestHandler(t *testing.T, p collector.Pipeline, saver collector.Saver) *Handler {
	t.Helper()
	return newTestHandlerWithReporter(t, p, saver, stubReporter{}, nil)
}

func newTestHandlerWithReporter(t *testing.T, p collector.Pipeline,
	saver collector.Saver, reporter repo.TaskReporter, metrics *metrics) *Handler {
	t.Helper()

	dispatcher, err := collector.NewDispatcher(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		noop.NewTracerProvider().Tracer("test"),
		collector.NewPipelineRegistry(p),
	)
	require.NoError(t, err)
	pipeline := repomocks.NewMockPipeline(t)
	pipeline.EXPECT().GetContentByCandidateID(mock.Anything, mock.Anything).Return(repo.Content{}, errContentNotFound).Maybe()
	pipeline.EXPECT().GetContentByURL(mock.Anything, mock.Anything).Return(repo.Content{}, errContentNotFound).Maybe()
	pipeline.EXPECT().CreateContent(mock.Anything, mock.Anything).Return(repo.Content{ID: uuid.Must(uuid.NewV7())}, nil).Maybe()

	h, err := NewHandler(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		noop.NewTracerProvider().Tracer("test"),
		dispatcher,
		saver,
		nil,
		pipeline,
		reporter,
		metrics,
	)
	require.NoError(t, err)
	return h
}

func collectCollectorMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	return rm
}

func collectorCounterValue(t *testing.T, rm metricdata.ResourceMetrics, attrKey, attrValue string) int64 {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "prism.collector.tasks" {
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

func collectorCounterTotal(t *testing.T, rm metricdata.ResourceMetrics) int64 {
	t.Helper()
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "prism.collector.tasks" {
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

func collectorHistogramCount(t *testing.T, rm metricdata.ResourceMetrics, name string) uint64 {
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

type capturingSaver struct {
	records []collector.Archive
}

func (s *capturingSaver) Save(_ context.Context, record collector.Archive) error {
	s.records = append(s.records, record)
	return nil
}

type stubReporter struct{}

func (stubReporter) CompleteTask(context.Context, uuid.UUID) error { return nil }

func (stubReporter) FailTask(context.Context, uuid.UUID) error { return nil }

type metricsReporter struct {
	completeErr error
	failErr     error
}

func (r metricsReporter) CompleteTask(context.Context, uuid.UUID) error { return r.completeErr }

func (r metricsReporter) FailTask(context.Context, uuid.UUID) error { return r.failErr }
