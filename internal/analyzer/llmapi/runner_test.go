package llmapi_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/ChiaYuChang/prism/internal/analyzer/llmapi"
	"github.com/ChiaYuChang/prism/internal/llm"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

type testPacket = llmapi.Packet[string, struct{}, string]

type fakeStage struct {
	mu       sync.Mutex
	name     string
	order    []string
	failAt   string
	err      error
	packets  []*testPacket
	contexts []context.Context
}

func (s *fakeStage) Name() string { return s.name }

func (s *fakeStage) record(ctx context.Context, p *testPacket, step string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.order = append(s.order, step)
	s.packets = append(s.packets, p)
	s.contexts = append(s.contexts, ctx)
	if s.failAt == step {
		return s.err
	}
	return nil
}

func (s *fakeStage) PreProcess(ctx context.Context, p *testPacket) error {
	return s.record(ctx, p, "pre_process")
}

func (s *fakeStage) APICall(ctx context.Context, p *testPacket) error {
	return s.record(ctx, p, "api_call")
}

func (s *fakeStage) PostProcess(ctx context.Context, p *testPacket) error {
	return s.record(ctx, p, "post_process")
}

func newRunner(t *testing.T, stage *fakeStage, exporter *tracetest.InMemoryExporter, output *bytes.Buffer) *llmapi.Runner[string, struct{}, string] {
	t.Helper()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { require.NoError(t, tp.Shutdown(context.Background())) })
	logger := slog.New(slog.NewTextHandler(output, nil))
	runner, err := llmapi.NewRunner(tp.Tracer("test"), logger, stage)
	require.NoError(t, err)
	return runner
}

func TestNewRunnerValidatesDependencies(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	stage := &fakeStage{name: "test"}

	_, err := llmapi.NewRunner[string, struct{}, string](nil, logger, stage)
	require.ErrorIs(t, err, llmapi.ErrParamMissing)
	_, err = llmapi.NewRunner[string, struct{}, string](tracer, nil, stage)
	require.ErrorIs(t, err, llmapi.ErrParamMissing)
	_, err = llmapi.NewRunner[string, struct{}, string](tracer, logger, nil)
	require.ErrorIs(t, err, llmapi.ErrParamMissing)
	_, err = llmapi.NewRunner[string, struct{}, string](tracer, logger, &fakeStage{name: " \t"})
	require.ErrorIs(t, err, llmapi.ErrParamMissing)
	var typedNilStage *fakeStage
	_, err = llmapi.NewRunner[string, struct{}, string](tracer, logger, typedNilStage)
	require.ErrorIs(t, err, llmapi.ErrParamMissing)
	var typedNilTracer *noop.Tracer
	_, err = llmapi.NewRunner[string, struct{}, string](typedNilTracer, logger, stage)
	require.ErrorIs(t, err, llmapi.ErrParamMissing)
}

func TestRunnerDoRejectsNilPacket(t *testing.T) {
	var logs bytes.Buffer
	exporter := tracetest.NewInMemoryExporter()
	runner := newRunner(t, &fakeStage{name: "test"}, exporter, &logs)

	err := runner.Do(context.Background(), nil)
	require.ErrorIs(t, err, llmapi.ErrNilPacket)
	require.Contains(t, logs.String(), "component=analyzer.llmapi.runner")
	require.Contains(t, logs.String(), "stage=test")
}

func TestRunnerDoExecutesLifecycleWithSamePacketAndDerivedContexts(t *testing.T) {
	var logs bytes.Buffer
	exporter := tracetest.NewInMemoryExporter()
	stage := &fakeStage{name: "test"}
	runner := newRunner(t, stage, exporter, &logs)
	p := llmapi.NewPacket[string, struct{}, string]("input")

	require.NoError(t, runner.Do(context.Background(), p))

	stage.mu.Lock()
	require.Equal(t, []string{"pre_process", "api_call", "post_process"}, stage.order)
	require.Len(t, stage.packets, 3)
	for _, got := range stage.packets {
		require.Same(t, p, got)
	}
	for _, stepCtx := range stage.contexts {
		require.True(t, trace.SpanContextFromContext(stepCtx).IsValid())
	}
	stage.mu.Unlock()
	spans := exporter.GetSpans()
	require.Len(t, spans, 4)
	var parentSpanID trace.SpanID
	var parentTraceID trace.TraceID
	spanByName := make(map[string]tracetest.SpanStub, len(spans))
	for _, span := range spans {
		spanByName[span.Name] = span
		if span.Name == "analyzer.llmapi.test" {
			parentSpanID = span.SpanContext.SpanID()
			parentTraceID = span.SpanContext.TraceID()
			requireAttribute(t, span, "analyzer.stage", "test")
		}
	}
	require.NotEqual(t, trace.SpanID{}, parentSpanID)
	for _, span := range spans {
		if span.Name == "analyzer.llmapi.test" {
			continue
		}
		require.Equal(t, parentSpanID, span.Parent.SpanID())
		require.Equal(t, parentTraceID, span.SpanContext.TraceID())
		requireAttribute(t, span, "analyzer.stage", "test")
		requireAttribute(t, span, "analyzer.step", span.Name)
	}
	stage.mu.Lock()
	for i, step := range stage.order {
		require.Equal(t, spanByName[step].SpanContext.SpanID(), trace.SpanContextFromContext(stage.contexts[i]).SpanID())
		require.Equal(t, parentTraceID, trace.SpanContextFromContext(stage.contexts[i]).TraceID())
	}
	stage.mu.Unlock()
	require.Empty(t, logs.String())
}

func TestRunnerDoShortCircuitsAndPreservesCause(t *testing.T) {
	for _, tc := range []struct {
		name   string
		failAt string
		order  []string
	}{
		{name: "pre process", failAt: "pre_process", order: []string{"pre_process"}},
		{name: "api call", failAt: "api_call", order: []string{"pre_process", "api_call"}},
		{name: "post process", failAt: "post_process", order: []string{"pre_process", "api_call", "post_process"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cause := errors.New("stage failure")
			var logs bytes.Buffer
			exporter := tracetest.NewInMemoryExporter()
			stage := &fakeStage{name: "test", failAt: tc.failAt, err: cause}
			runner := newRunner(t, stage, exporter, &logs)

			err := runner.Do(context.Background(), llmapi.NewPacket[string, struct{}, string]("input"))
			require.ErrorIs(t, err, cause)
			require.Equal(t, tc.order, stage.order)
			require.Equal(t, 1, strings.Count(logs.String(), "LLM stage failed"))
			require.Contains(t, logs.String(), "component=analyzer.llmapi.runner")
			require.Contains(t, logs.String(), "stage=test")
			require.Contains(t, logs.String(), "error=\""+tc.failAt+": stage failure\"")
			spans := exporter.GetSpans()
			require.Len(t, spans, len(tc.order)+1)
			expectedNames := append([]string{"analyzer.llmapi.test"}, tc.order...)
			actualNames := make([]string, 0, len(spans))
			for _, span := range spans {
				actualNames = append(actualNames, span.Name)
				if tc.failAt != "post_process" {
					require.NotEqual(t, "post_process", span.Name)
				}
			}
			require.ElementsMatch(t, expectedNames, actualNames)
		})
	}
}

func TestRunnerDoRecordsParentAndChildSpanErrors(t *testing.T) {
	var logs bytes.Buffer
	exporter := tracetest.NewInMemoryExporter()
	stage := &fakeStage{name: "test", failAt: "api_call", err: context.Canceled}
	runner := newRunner(t, stage, exporter, &logs)

	err := runner.Do(context.Background(), llmapi.NewPacket[string, struct{}, string]("input"))
	require.ErrorIs(t, err, context.Canceled)

	spans := exporter.GetSpans()
	require.Len(t, spans, 3)
	for _, span := range spans {
		if span.Name == "analyzer.llmapi.test" || span.Name == "api_call" {
			require.Equal(t, codes.Error, span.Status.Code)
			requireExceptionEvent(t, span, "api_call: context canceled")
		}
		require.NotEqual(t, "post_process", span.Name)
	}
}

func TestRunnerDoNilPacketRecordsOneErroredParentWithoutChildren(t *testing.T) {
	var logs bytes.Buffer
	exporter := tracetest.NewInMemoryExporter()
	runner := newRunner(t, &fakeStage{name: "test"}, exporter, &logs)

	require.ErrorIs(t, runner.Do(context.Background(), nil), llmapi.ErrNilPacket)
	require.Equal(t, 1, strings.Count(logs.String(), "LLM stage failed"))
	require.NotContains(t, logs.String(), "input")

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	require.Equal(t, "analyzer.llmapi.test", spans[0].Name)
	require.Equal(t, codes.Error, spans[0].Status.Code)
	requireExceptionEvent(t, spans[0], "packet is nil")
}

func TestRunnerDoesNotLogPacketPayloads(t *testing.T) {
	var logs bytes.Buffer
	exporter := tracetest.NewInMemoryExporter()
	stage := &fakeStage{name: "test", failAt: "post_process", err: errors.New("stage failure")}
	runner := newRunner(t, stage, exporter, &logs)
	p := llmapi.NewPacket[string, struct{}, string]("secret-input")
	p.Request = &llm.GenerateRequest{Prompt: "secret-request"}
	p.Response = &llm.GenerateResponse{Text: "secret-response"}

	require.Error(t, runner.Do(context.Background(), p))
	require.NotContains(t, logs.String(), "secret-input")
	require.NotContains(t, logs.String(), "secret-request")
	require.NotContains(t, logs.String(), "secret-response")
}

func TestRunnerDoIsSafeForConcurrentCalls(t *testing.T) {
	var logs bytes.Buffer
	exporter := tracetest.NewInMemoryExporter()
	stage := &fakeStage{name: "test"}
	runner := newRunner(t, stage, exporter, &logs)

	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- runner.Do(context.Background(), llmapi.NewPacket[string, struct{}, string]("input"))
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	require.Len(t, stage.order, 60)
}

func requireAttribute(t *testing.T, span tracetest.SpanStub, key, want string) {
	t.Helper()
	for _, attr := range span.Attributes {
		if string(attr.Key) == key {
			require.Equal(t, want, attr.Value.AsString())
			return
		}
	}
	require.Failf(t, "missing span attribute", "key=%s span=%s", key, span.Name)
}

func requireExceptionEvent(t *testing.T, span tracetest.SpanStub, wantMessage string) {
	t.Helper()
	for _, event := range span.Events {
		if event.Name != "exception" {
			continue
		}
		hasType := false
		hasMessage := false
		message := ""
		for _, attr := range event.Attributes {
			switch string(attr.Key) {
			case "exception.type":
				hasType = true
			case "exception.message":
				hasMessage = true
				message = attr.Value.AsString()
			}
		}
		require.True(t, hasType)
		require.True(t, hasMessage)
		require.Equal(t, wantMessage, message)
		return
	}
	require.Failf(t, "missing exception event", "span=%s", span.Name)
}
