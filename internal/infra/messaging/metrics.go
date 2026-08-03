package messaging

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type metrics struct {
	publishMessages metric.Int64Counter
	publishErrors   metric.Int64Counter
	publishDuration metric.Float64Histogram
	publishBytes    metric.Int64Histogram
	processMessages metric.Int64Counter
	processErrors   metric.Int64Counter
	processDuration metric.Float64Histogram
	processBytes    metric.Int64Histogram
	inflight        metric.Int64UpDownCounter
	subscribeErrors metric.Int64Counter
}

func newMetrics(meter metric.Meter) (*metrics, error) {
	publishMessages, err := meter.Int64Counter(
		"prism.messaging.publish.messages",
		metric.WithDescription("Count of messages submitted to a Watermill publisher."),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create messaging publish counter: %w", err)
	}
	publishErrors, err := meter.Int64Counter(
		"prism.messaging.publish.errors",
		metric.WithDescription("Count of Watermill publish failures."),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create messaging publish error counter: %w", err)
	}
	publishDuration, err := meter.Float64Histogram(
		"prism.messaging.publish.duration",
		metric.WithDescription("Duration of Watermill publish calls."),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("create messaging publish duration: %w", err)
	}
	publishBytes, err := meter.Int64Histogram(
		"prism.messaging.publish.bytes",
		metric.WithDescription("Payload bytes submitted to a Watermill publisher."),
		metric.WithUnit("By"),
	)
	if err != nil {
		return nil, fmt.Errorf("create messaging publish bytes: %w", err)
	}
	processMessages, err := meter.Int64Counter(
		"prism.messaging.process.messages",
		metric.WithDescription("Count of Watermill message processing outcomes."),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create messaging process counter: %w", err)
	}
	processErrors, err := meter.Int64Counter(
		"prism.messaging.process.errors",
		metric.WithDescription("Count of Watermill message processing failures."),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create messaging process error counter: %w", err)
	}
	processDuration, err := meter.Float64Histogram(
		"prism.messaging.process.duration",
		metric.WithDescription("Duration from Watermill delivery until ACK, NACK, or timeout."),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("create messaging process duration: %w", err)
	}
	processBytes, err := meter.Int64Histogram(
		"prism.messaging.process.bytes",
		metric.WithDescription("Payload bytes delivered by Watermill."),
		metric.WithUnit("By"),
	)
	if err != nil {
		return nil, fmt.Errorf("create messaging process bytes: %w", err)
	}
	inflight, err := meter.Int64UpDownCounter(
		"prism.messaging.inflight",
		metric.WithDescription("Number of Watermill messages awaiting an outcome."),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create messaging inflight gauge: %w", err)
	}
	subscribeErrors, err := meter.Int64Counter(
		"prism.messaging.subscribe.errors",
		metric.WithDescription("Count of Watermill subscription failures."),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create messaging subscribe error counter: %w", err)
	}

	return &metrics{
		publishMessages: publishMessages,
		publishErrors:   publishErrors,
		publishDuration: publishDuration,
		publishBytes:    publishBytes,
		processMessages: processMessages,
		processErrors:   processErrors,
		processDuration: processDuration,
		processBytes:    processBytes,
		inflight:        inflight,
		subscribeErrors: subscribeErrors,
	}, nil
}

func (m *metrics) attributes(system, destination, operation, outcome, consumer string) metric.MeasurementOption {
	attrs := []attribute.KeyValue{
		attribute.String("messaging.system", system),
		attribute.String("messaging.destination.name", destination),
		attribute.String("messaging.operation.type", operation),
	}
	if outcome != "" {
		attrs = append(attrs, attribute.String("outcome", outcome))
	}
	if consumer != "" {
		attrs = append(attrs, attribute.String("messaging.consumer.group.name", consumer))
	}
	return metric.WithAttributes(attrs...)
}

func (m *metrics) recordPublish(ctx context.Context, system, destination string, count, bytes int, started time.Time, err error) {
	outcome := "ok"
	if err != nil {
		outcome = "error"
	}
	attrs := m.attributes(system, destination, "publish", outcome, "")
	m.publishMessages.Add(ctx, int64(count), attrs)
	m.publishDuration.Record(ctx, time.Since(started).Seconds(), attrs)
	m.publishBytes.Record(ctx, int64(bytes), attrs)
	if err != nil {
		m.publishErrors.Add(ctx, 1, attrs)
	}
}

func (m *metrics) recordSubscribeError(ctx context.Context, system, destination, consumer string) {
	m.subscribeErrors.Add(
		ctx,
		1,
		m.attributes(system, destination, "subscribe", "error", consumer),
	)
}
