package message

import (
	"context"
	"errors"

	wm "github.com/ThreeDotsLabs/watermill/message"
	"go.opentelemetry.io/otel"
)

// ErrNilMessage indicates a nil Watermill message was passed to a trace helper.
var ErrNilMessage = errors.New("message is nil")

// otelMetadataCarrier adapts Watermill's Metadata map to OTel's TextMapCarrier.
type otelMetadataCarrier struct {
	wm.Metadata
}

// Keys lists the keys in the carrier.
func (c otelMetadataCarrier) Keys() []string {
	keys := make([]string, 0, len(c.Metadata))
	for k := range c.Metadata {
		keys = append(keys, k)
	}
	return keys
}

// InjectTraceContext injects the OpenTelemetry trace context from ctx into the
// Watermill message metadata.
func InjectTraceContext(ctx context.Context, msg *wm.Message) error {
	if msg == nil {
		return ErrNilMessage
	}
	if msg.Metadata == nil {
		msg.Metadata = make(wm.Metadata)
	}
	otel.GetTextMapPropagator().
		Inject(ctx, otelMetadataCarrier{msg.Metadata})
	return nil
}

// ExtractTraceContext extracts the OpenTelemetry trace context from the Watermill
// message metadata and returns a new context.
func ExtractTraceContext(ctx context.Context, msg *wm.Message) (context.Context, error) {
	if msg == nil {
		return ctx, ErrNilMessage
	}
	if msg.Metadata == nil {
		return ctx, nil
	}
	return otel.GetTextMapPropagator().
		Extract(ctx, otelMetadataCarrier{msg.Metadata}), nil
}
