package message_test

import (
	"context"
	"testing"

	"github.com/ChiaYuChang/prism/internal/message"
	wm "github.com/ThreeDotsLabs/watermill/message"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestWatermillTraceContextPropagation(t *testing.T) {
	previousPropagator := otel.GetTextMapPropagator()
	t.Cleanup(func() { otel.SetTextMapPropagator(previousPropagator) })

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer func() { _ = tp.Shutdown(context.Background()) }()
	tracer := tp.Tracer("test-watermill")

	ctx, parentSpan := tracer.Start(context.Background(), "parent")
	parentSpanID := parentSpan.SpanContext().SpanID()
	parentTraceID := parentSpan.SpanContext().TraceID()
	require.True(t, parentSpan.SpanContext().IsValid())

	msg := wm.NewMessage("test-msg-id", []byte("payload"))
	require.NoError(t, message.InjectTraceContext(ctx, msg))
	require.NotEmpty(t, msg.Metadata.Get("traceparent"))

	extractedCtx, err := message.ExtractTraceContext(context.Background(), msg)
	require.NoError(t, err)
	_, childSpan := tracer.Start(extractedCtx, "child")
	childSpanID := childSpan.SpanContext().SpanID()
	require.True(t, childSpan.SpanContext().IsValid())
	require.Equal(t, parentTraceID, childSpan.SpanContext().TraceID())
	require.NotEqual(t, parentSpanID, childSpanID)

	childSpan.End()
	parentSpan.End()

	spans := exporter.GetSpans()
	require.Len(t, spans, 2)

	spanByName := make(map[string]tracetest.SpanStub, len(spans))
	for _, span := range spans {
		spanByName[span.Name] = span
	}
	parent := spanByName["parent"]
	child := spanByName["child"]

	require.Equal(t, parentTraceID, parent.SpanContext.TraceID())
	require.Equal(t, parentTraceID, child.SpanContext.TraceID())
	require.Equal(t, parentSpanID, child.Parent.SpanID())
}

func TestWatermillTraceContextNilMessage(t *testing.T) {
	require.ErrorIs(t, message.InjectTraceContext(context.Background(), nil), message.ErrNilMessage)

	ctx, err := message.ExtractTraceContext(context.Background(), nil)
	require.ErrorIs(t, err, message.ErrNilMessage)
	require.NotNil(t, ctx)
}
