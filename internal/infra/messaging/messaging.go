package messaging

import (
	"context"
	"errors"
	"fmt"
	"time"

	prismmessage "github.com/ChiaYuChang/prism/internal/message"
	wm "github.com/ThreeDotsLabs/watermill/message"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// ErrParamMissing indicates that a required instrumentation dependency was not supplied.
var ErrParamMissing = errors.New("param missing")

// Config configures Watermill messaging instrumentation.
type Config struct {
	Tracer         trace.Tracer
	Meter          metric.Meter
	System         string
	Consumer       string
	AckWaitTimeout time.Duration
}

// Instrumentation decorates Watermill publishers and subscribers with traces and metrics.
type Instrumentation struct {
	tracer         trace.Tracer
	metrics        *metrics
	system         string
	consumer       string
	ackWaitTimeout time.Duration
}

// New creates Watermill instrumentation from an initialized telemetry provider.
func New(config Config) (*Instrumentation, error) {
	if config.Tracer == nil {
		return nil, fmt.Errorf("%w: tracer", ErrParamMissing)
	}
	if config.Meter == nil {
		return nil, fmt.Errorf("%w: meter", ErrParamMissing)
	}
	if config.System == "" {
		config.System = "unknown"
	}
	if config.AckWaitTimeout <= 0 {
		config.AckWaitTimeout = 30 * time.Second
	}

	metrics, err := newMetrics(config.Meter)
	if err != nil {
		return nil, err
	}
	return &Instrumentation{
		tracer:         config.Tracer,
		metrics:        metrics,
		system:         config.System,
		consumer:       config.Consumer,
		ackWaitTimeout: config.AckWaitTimeout,
	}, nil
}

// Publisher wraps a Watermill publisher with producer tracing and metrics.
func (i *Instrumentation) Publisher(next wm.Publisher) (wm.Publisher, error) {
	if next == nil {
		return nil, fmt.Errorf("%w: publisher", ErrParamMissing)
	}
	return &publisher{next: next, instrumentation: i}, nil
}

// Subscriber wraps a Watermill subscriber with consumer tracing and metrics.
func (i *Instrumentation) Subscriber(next wm.Subscriber) (wm.Subscriber, error) {
	if next == nil {
		return nil, fmt.Errorf("%w: subscriber", ErrParamMissing)
	}
	return &subscriber{next: next, instrumentation: i}, nil
}

func messageContext(msg *wm.Message) context.Context {
	ctx := msg.Context()
	if trace.SpanContextFromContext(ctx).IsValid() {
		return ctx
	}
	ctx, _ = prismmessage.ExtractTraceContext(ctx, msg)
	return ctx
}

func (i *Instrumentation) spanAttributes(destination, operation string) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("messaging.system", i.system),
		attribute.String("messaging.destination.name", destination),
		attribute.String("messaging.operation.type", operation),
	}
	if i.consumer != "" {
		attrs = append(attrs, attribute.String("messaging.consumer.group.name", i.consumer))
	}
	return attrs
}
