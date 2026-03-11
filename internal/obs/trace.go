package obs

import (
	"context"

	"go.opentelemetry.io/otel/trace"
)

const (
	// DefaultTraceIDFallback is used when no TraceID is found in the context.
	DefaultTraceIDFallback = "00000000000000000000000000000000"
)

// ExtractTraceID retrieves the current OpenTelemetry TraceID from the context.
// It returns a hex-encoded string.
func ExtractTraceID(ctx context.Context) string {
	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.HasTraceID() {
		return spanCtx.TraceID().String()
	}
	return DefaultTraceIDFallback
}
