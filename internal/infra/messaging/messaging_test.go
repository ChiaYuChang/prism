package messaging

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	prismmessage "github.com/ChiaYuChang/prism/internal/message"
	wm "github.com/ThreeDotsLabs/watermill/message"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestPublisherCreatesProducerSpanAndMetrics(t *testing.T) {
	inst, reader, exporter := newTestInstrumentation(t, time.Second)
	fake := &fakePublisher{}
	publisher, err := inst.Publisher(fake)
	require.NoError(t, err)

	tracer := inst.tracer
	parentCtx, parent := tracer.Start(context.Background(), "parent")
	msg := wm.NewMessage("message-1", []byte("payload"))
	msg.SetContext(parentCtx)

	require.NoError(t, publisher.Publish("prism_task", msg))
	parent.End()

	require.Len(t, fake.messages, 1)
	require.NotEmpty(t, fake.messages[0].Metadata.Get("traceparent"))
	spans := exporter.GetSpans()
	producer := spanByName(t, spans, "publish prism_task")
	require.Equal(t, parent.SpanContext().SpanID(), producer.Parent.SpanID())
	require.Equal(t, trace.SpanKindProducer, producer.SpanKind)

	rm := collectMetrics(t, reader)
	require.Equal(t, int64(1), counterValue(t, rm, "prism.messaging.publish.messages", map[string]string{
		"messaging.system":           "nats",
		"messaging.destination.name": "prism_task",
		"messaging.operation.type":   "publish",
		"outcome":                    "ok",
	}))
	require.Equal(t, uint64(1), histogramCount(t, rm, "prism.messaging.publish.duration", map[string]string{
		"messaging.destination.name": "prism_task",
		"outcome":                    "ok",
	}))
}

func TestSubscriberCreatesConsumerSpanUntilAck(t *testing.T) {
	inst, reader, exporter := newTestInstrumentation(t, time.Second)
	fake := newFakeSubscriber()
	subscriber, err := inst.Subscriber(fake)
	require.NoError(t, err)

	parentCtx, parent := inst.tracer.Start(context.Background(), "producer")
	msg := wm.NewMessage("message-2", []byte("payload"))
	require.NoError(t, injectTestContext(parentCtx, msg))
	parent.End()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	messages, err := subscriber.Subscribe(ctx, "prism_task")
	require.NoError(t, err)
	fake.messages <- msg

	received := <-messages
	require.Equal(t, msg, received)
	require.True(t, trace.SpanContextFromContext(received.Context()).IsValid())
	received.Ack()

	require.Eventually(t, func() bool {
		return hasSpan(exporter.GetSpans(), "process prism_task")
	}, time.Second, time.Millisecond)
	process := spanByName(t, exporter.GetSpans(), "process prism_task")
	require.Equal(t, parent.SpanContext().SpanID(), process.Parent.SpanID())
	require.Equal(t, trace.SpanKindConsumer, process.SpanKind)

	rm := collectMetrics(t, reader)
	require.Equal(t, int64(1), counterValue(t, rm, "prism.messaging.process.messages", map[string]string{
		"messaging.destination.name": "prism_task",
		"outcome":                    "ack",
	}))
	require.Equal(t, uint64(1), histogramCount(t, rm, "prism.messaging.process.duration", map[string]string{
		"messaging.destination.name": "prism_task",
		"outcome":                    "ack",
	}))
}

func TestPublisherReturnsUnderlyingError(t *testing.T) {
	inst, _, _ := newTestInstrumentation(t, time.Second)
	wantErr := errors.New("publish failed")
	publisher, err := inst.Publisher(&fakePublisher{err: wantErr})
	require.NoError(t, err)

	err = publisher.Publish("prism_task", wm.NewMessage("message-3", nil))
	require.ErrorIs(t, err, wantErr)
}

func TestSubscriberRecordsNackAndAckTimeout(t *testing.T) {
	inst, reader, _ := newTestInstrumentation(t, 10*time.Millisecond)
	fake := newFakeSubscriber()
	subscriber, err := inst.Subscriber(fake)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	messages, err := subscriber.Subscribe(ctx, "prism_task")
	require.NoError(t, err)

	fake.messages <- wm.NewMessage("message-nack", nil)
	(<-messages).Nack()
	fake.messages <- wm.NewMessage("message-timeout", nil)
	<-messages

	require.Eventually(t, func() bool {
		rm := collectMetrics(t, reader)
		return counterValue(t, rm, "prism.messaging.process.messages", map[string]string{
			"messaging.destination.name": "prism_task",
			"outcome":                    "nack",
		}) == 1 && counterValue(t, rm, "prism.messaging.process.messages", map[string]string{
			"messaging.destination.name": "prism_task",
			"outcome":                    "ack_timeout",
		}) == 1
	}, time.Second, time.Millisecond)
}

func newTestInstrumentation(t *testing.T, ackWait time.Duration) (*Instrumentation, *sdkmetric.ManualReader, *tracetest.InMemoryExporter) {
	t.Helper()
	previousPropagator := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	t.Cleanup(func() { otel.SetTextMapPropagator(previousPropagator) })
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	traceExporter := tracetest.NewInMemoryExporter()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(traceExporter)))
	t.Cleanup(func() {
		_ = meterProvider.Shutdown(context.Background())
		_ = tracerProvider.Shutdown(context.Background())
	})

	inst, err := New(Config{
		Tracer:         tracerProvider.Tracer("test.messaging"),
		Meter:          meterProvider.Meter("test.messaging"),
		System:         "nats",
		Consumer:       "test-consumer",
		AckWaitTimeout: ackWait,
	})
	require.NoError(t, err)
	return inst, reader, traceExporter
}

func injectTestContext(ctx context.Context, msg *wm.Message) error {
	return prismmessage.InjectTraceContext(ctx, msg)
}

func collectMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	return rm
}

func counterValue(t *testing.T, rm metricdata.ResourceMetrics, name string, attrs map[string]string) int64 {
	t.Helper()
	var total int64
	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != name {
				continue
			}
			sum, ok := metric.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, point := range sum.DataPoints {
				if attributesMatch(point.Attributes, attrs) {
					total += point.Value
				}
			}
		}
	}
	return total
}

func histogramCount(t *testing.T, rm metricdata.ResourceMetrics, name string, attrs map[string]string) uint64 {
	t.Helper()
	var total uint64
	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != name {
				continue
			}
			histogram, ok := metric.Data.(metricdata.Histogram[float64])
			require.True(t, ok)
			for _, point := range histogram.DataPoints {
				if attributesMatch(point.Attributes, attrs) {
					total += point.Count
				}
			}
		}
	}
	return total
}

func attributesMatch(set attribute.Set, attrs map[string]string) bool {
	for key, want := range attrs {
		got, found := set.Value(attribute.Key(key))
		if !found || got.AsString() != want {
			return false
		}
	}
	return true
}

func spanByName(t *testing.T, spans []tracetest.SpanStub, name string) tracetest.SpanStub {
	t.Helper()
	for _, span := range spans {
		if span.Name == name {
			return span
		}
	}
	t.Fatalf("span %q not found", name)
	return tracetest.SpanStub{}
}

func hasSpan(spans []tracetest.SpanStub, name string) bool {
	for _, span := range spans {
		if span.Name == name {
			return true
		}
	}
	return false
}

type fakePublisher struct {
	mu       sync.Mutex
	messages []*wm.Message
	err      error
}

func (f *fakePublisher) Publish(_ string, messages ...*wm.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = append(f.messages, messages...)
	return f.err
}

func (f *fakePublisher) Close() error { return nil }

type fakeSubscriber struct {
	messages chan *wm.Message
	once     sync.Once
}

func newFakeSubscriber() *fakeSubscriber {
	return &fakeSubscriber{messages: make(chan *wm.Message, 1)}
}

func (f *fakeSubscriber) Subscribe(context.Context, string) (<-chan *wm.Message, error) {
	return f.messages, nil
}

func (f *fakeSubscriber) Close() error {
	f.once.Do(func() { close(f.messages) })
	return nil
}
