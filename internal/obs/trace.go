package obs

import (
	"context"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
)

type contextKey string

const (
	// DefaultTraceIDFallback is used when no TraceID is found in the context.
	DefaultTraceIDFallback = "00000000000000000000000000000000"

	traceIDKey contextKey = "prism.trace_id"
	userIDKey  contextKey = "prism.user_id"
)

// WithTraceID returns a new context with the manually injected trace ID.
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDKey, traceID)
}

// ExtractTraceID retrieves the TraceID from context, prioritizing OpenTelemetry
// and falling back to manually injected ID.
func ExtractTraceID(ctx context.Context) string {
	// 1. Try OpenTelemetry
	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.HasTraceID() {
		return spanCtx.TraceID().String()
	}

	// 2. Try manually injected ID
	if v, ok := ctx.Value(traceIDKey).(string); ok {
		return v
	}

	return DefaultTraceIDFallback
}

// WithUserID returns a new context with the user ID.
func WithUserID(ctx context.Context, userID uuid.UUID) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}

// ExtractUserID retrieves the UserID from context.
func ExtractUserID(ctx context.Context) uuid.UUID {
	if v, ok := ctx.Value(userIDKey).(uuid.UUID); ok {
		return v
	}
	return uuid.Nil
}
