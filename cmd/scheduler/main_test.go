package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/ChiaYuChang/prism/internal/infra"
	"github.com/ChiaYuChang/prism/internal/message"
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

type stubTaskPublisher struct {
	publish func(topic string, messages ...*wm.Message) error
}

func (s stubTaskPublisher) Publish(topic string, messages ...*wm.Message) error {
	if s.publish != nil {
		return s.publish(topic, messages...)
	}
	return nil
}

func TestDispatchTasksPublishesTaskSignal(t *testing.T) {
	scheduler := repomocks.NewMockScheduler(t)
	taskID := uuid.Must(uuid.NewV7())
	batchID := uuid.Must(uuid.NewV7())

	tasks := []repo.Task{{
		ID:         taskID,
		BatchID:    batchID,
		Kind:       "DIRECTORY_FETCH",
		SourceType: "PARTY",
		SourceAbbr: "dpp",
		URL:        "https://example.com/listing",
		TraceID:    "trace-123",
	}}

	var gotTopic string
	var gotSig message.TaskSignal
	publisher := stubTaskPublisher{
		publish: func(topic string, messages ...*wm.Message) error {
			require.Len(t, messages, 1)
			gotTopic = topic
			require.Equal(t, "trace-123", messages[0].Metadata.Get("trace_id"))
			require.NoError(t, gotSig.Unmarshal(messages[0].Payload))
			return nil
		},
	}

	svc := newScheduler(
		testSchedulerLogger(),
		noop.NewTracerProvider().Tracer("test"),
		nil,
		infra.NoOpRateLimiter{},
		scheduler,
		publisher,
	)

	err := svc.DispatchTasks(context.Background(), tasks)
	require.NoError(t, err)
	require.Equal(t, message.TaskTopic, gotTopic)
	require.Equal(t, taskID, gotSig.TaskID)
	require.Equal(t, batchID, gotSig.BatchID)
	require.Equal(t, "PARTY", gotSig.SourceType)
	require.Equal(t, "dpp", gotSig.SourceAbbr)
}

func TestDispatchTasksMarksTaskFailedWhenPublishFails(t *testing.T) {
	scheduler := repomocks.NewMockScheduler(t)
	taskID := uuid.Must(uuid.NewV7())
	tasks := []repo.Task{{
		ID:         taskID,
		BatchID:    uuid.Must(uuid.NewV7()),
		Kind:       "DIRECTORY_FETCH",
		SourceType: "PARTY",
		SourceAbbr: "dpp",
		URL:        "https://example.com/listing",
		TraceID:    "trace-123",
	}}
	scheduler.EXPECT().FailTask(mock.Anything, taskID).Return(nil)

	publisher := stubTaskPublisher{
		publish: func(topic string, messages ...*wm.Message) error {
			return errors.New("publish failed")
		},
	}

	svc := newScheduler(
		testSchedulerLogger(),
		noop.NewTracerProvider().Tracer("test"),
		nil,
		infra.NoOpRateLimiter{},
		scheduler,
		publisher,
	)
	err := svc.DispatchTasks(context.Background(), tasks)
	require.NoError(t, err)
}

func TestDispatchTasksRecordsMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, meterProvider.Shutdown(context.Background())) })
	metrics, err := newSchedulerMetrics(meterProvider.Meter("test"))
	require.NoError(t, err)

	scheduler := repomocks.NewMockScheduler(t)
	taskID := uuid.Must(uuid.NewV7())
	tasks := []repo.Task{{
		ID:         taskID,
		BatchID:    uuid.Must(uuid.NewV7()),
		Kind:       repo.TaskKindDirectoryFetch,
		SourceType: repo.SourceTypeParty,
		SourceAbbr: repo.SourceAbbrDPP,
		URL:        "https://example.com/listing",
		TraceID:    "trace-123",
	}}
	publisher := stubTaskPublisher{}

	svc := newScheduler(
		testSchedulerLogger(),
		noop.NewTracerProvider().Tracer("test"),
		metrics,
		infra.NoOpRateLimiter{},
		scheduler,
		publisher,
	)
	err = svc.DispatchTasks(context.Background(), tasks)
	require.NoError(t, err)

	rm := collectMetrics(t, reader)
	require.Equal(t, int64(1), int64CounterValue(t, rm, "prism.scheduler.tasks", "result", "published"))
	require.Equal(t, int64(1), int64CounterTotal(t, rm, "prism.scheduler.tasks"))
	require.Equal(t, uint64(1), histogramCount(t, rm, "prism.scheduler.dispatch.duration"))
}

func TestDispatchTasksRecordsFailureMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, meterProvider.Shutdown(context.Background())) })
	metrics, err := newSchedulerMetrics(meterProvider.Meter("test"))
	require.NoError(t, err)

	scheduler := repomocks.NewMockScheduler(t)
	taskID := uuid.Must(uuid.NewV7())
	scheduler.EXPECT().FailTask(mock.Anything, taskID).Return(nil)
	tasks := []repo.Task{{
		ID:         taskID,
		BatchID:    uuid.Must(uuid.NewV7()),
		Kind:       repo.TaskKindDirectoryFetch,
		SourceType: repo.SourceTypeParty,
		SourceAbbr: repo.SourceAbbrDPP,
		URL:        "https://example.com/listing",
		TraceID:    "trace-123",
	}}
	publisher := stubTaskPublisher{publish: func(topic string, messages ...*wm.Message) error {
		return errors.New("publish failed")
	}}

	svc := newScheduler(
		testSchedulerLogger(),
		noop.NewTracerProvider().Tracer("test"),
		metrics,
		infra.NoOpRateLimiter{},
		scheduler,
		publisher,
	)
	err = svc.DispatchTasks(context.Background(), tasks)
	require.NoError(t, err)

	rm := collectMetrics(t, reader)
	require.Equal(t, int64(1), int64CounterValue(t, rm, "prism.scheduler.tasks", "result", "marked_failed"))
	require.Equal(t, int64(1), int64CounterTotal(t, rm, "prism.scheduler.tasks"))
	require.Equal(t, uint64(1), histogramCount(t, rm, "prism.scheduler.dispatch.duration"))
}

func TestRunTickRecordsMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, meterProvider.Shutdown(context.Background())) })
	metrics, err := newSchedulerMetrics(meterProvider.Meter("test"))
	require.NoError(t, err)

	scheduler := repomocks.NewMockScheduler(t)
	tasks := []repo.Task{
		{
			ID:         uuid.Must(uuid.NewV7()),
			Kind:       repo.TaskKindDirectoryFetch,
			SourceType: repo.SourceTypeParty,
			SourceAbbr: repo.SourceAbbrDPP,
		},
		{
			ID:         uuid.Must(uuid.NewV7()),
			Kind:       repo.TaskKindDirectoryFetch,
			SourceType: repo.SourceTypeParty,
			SourceAbbr: repo.SourceAbbrKMT,
		},
		{
			ID:         uuid.Must(uuid.NewV7()),
			Kind:       repo.TaskKindDirectoryFetch,
			SourceType: repo.SourceTypeParty,
			SourceAbbr: repo.SourceAbbrKMT,
		},
	}
	scheduler.EXPECT().
		ClaimTasks(mock.Anything, int32(5), []string{repo.TaskKindDirectoryFetch}, mock.Anything).
		Return(tasks, nil)

	svc := newScheduler(
		testSchedulerLogger(),
		noop.NewTracerProvider().Tracer("test"),
		metrics,
		infra.NoOpRateLimiter{},
		scheduler,
		stubTaskPublisher{},
	)
	got := svc.RunTick(context.Background(), &Config{
		LockKey:   "test-lock",
		BatchSize: 5,
		Kinds:     []string{repo.TaskKindDirectoryFetch},
	})
	require.Len(t, got, len(tasks))

	rm := collectMetrics(t, reader)
	require.Equal(t, uint64(1), histogramCount(t, rm, "prism.scheduler.tick.duration"))
}

// collectMetrics collects metrics from the reader and returns them as a ResourceMetrics.
func collectMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	return rm
}

// int64CounterValue returns the value of an int64 counter metric with the given name and attributes.
func int64CounterValue(t *testing.T, rm metricdata.ResourceMetrics, name, attrKey, attrValue string) int64 {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
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

// int64CounterTotal returns the total value of an int64 counter metric.
func int64CounterTotal(t *testing.T, rm metricdata.ResourceMetrics, name string) int64 {
	t.Helper()
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
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

// histogramCount returns the count of data points in a histogram metric.
func histogramCount(t *testing.T, rm metricdata.ResourceMetrics, name string) uint64 {
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

func testSchedulerLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
