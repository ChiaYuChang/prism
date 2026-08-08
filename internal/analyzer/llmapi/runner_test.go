package llmapi_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/ChiaYuChang/prism/internal/analyzer/llmapi"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace/noop"
)

type testPacket = llmapi.Packet[string, struct{}, string]

type fakeStage struct {
	mu       sync.Mutex
	name     string
	order    []string
	failAt   []string
	errs     []error
	packets  []*testPacket
	contexts []context.Context
	
	attemptCount int
}

func (s *fakeStage) Name() string { return s.name }

func (s *fakeStage) record(ctx context.Context, p *testPacket, step string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.order = append(s.order, step)
	s.packets = append(s.packets, p)
	s.contexts = append(s.contexts, ctx)
	
	if s.attemptCount < len(s.failAt) && s.failAt[s.attemptCount] == step {
		err := s.errs[s.attemptCount]
		s.attemptCount++
		return err
	}
	
	if step == "post_process" {
		p.Output = "success"
		s.attemptCount++
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

func newRunner(t *testing.T, stage *fakeStage, maxAttempt int, exporter *tracetest.InMemoryExporter, output *bytes.Buffer) *llmapi.Runner[string, struct{}, string] {
	t.Helper()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { require.NoError(t, tp.Shutdown(context.Background())) })
	logger := slog.New(slog.NewTextHandler(output, nil))
	runner, err := llmapi.NewRunner(tp.Tracer("test"), logger, maxAttempt, stage)
	require.NoError(t, err)
	return runner
}

func TestNewRunnerValidatesDependencies(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	stage := &fakeStage{name: "test"}

	_, err := llmapi.NewRunner[string, struct{}, string](nil, logger, 1, stage)
	require.ErrorIs(t, err, llmapi.ErrParamMissing)
	_, err = llmapi.NewRunner[string, struct{}, string](tracer, nil, 1, stage)
	require.ErrorIs(t, err, llmapi.ErrParamMissing)
	_, err = llmapi.NewRunner[string, struct{}, string](tracer, logger, 0, stage)
	require.ErrorIs(t, err, llmapi.ErrParamMissing)
	_, err = llmapi.NewRunner[string, struct{}, string](tracer, logger, 1, nil)
	require.ErrorIs(t, err, llmapi.ErrParamMissing)
	_, err = llmapi.NewRunner[string, struct{}, string](tracer, logger, 1, &fakeStage{name: " \t"})
	require.ErrorIs(t, err, llmapi.ErrParamMissing)
	var typedNilStage *fakeStage
	_, err = llmapi.NewRunner[string, struct{}, string](tracer, logger, 1, typedNilStage)
	require.ErrorIs(t, err, llmapi.ErrParamMissing)
	var typedNilTracer *noop.Tracer
	_, err = llmapi.NewRunner[string, struct{}, string](typedNilTracer, logger, 1, stage)
	require.ErrorIs(t, err, llmapi.ErrParamMissing)
}

func TestRunnerDoExecutesLifecycleWithSamePacketAndDerivedContexts(t *testing.T) {
	var logs bytes.Buffer
	exporter := tracetest.NewInMemoryExporter()
	stage := &fakeStage{name: "test"}
	runner := newRunner(t, stage, 1, exporter, &logs)

	out, err := runner.Do(context.Background(), "input")
	require.NoError(t, err)
	require.Equal(t, "success", out)

	stage.mu.Lock()
	require.Equal(t, []string{"pre_process", "api_call", "post_process"}, stage.order)
	require.Len(t, stage.packets, 3)
	
	p := stage.packets[0]
	for _, got := range stage.packets {
		require.Same(t, p, got)
	}
	for _, stepCtx := range stage.contexts {
		require.True(t, trace.SpanContextFromContext(stepCtx).IsValid())
	}
	stage.mu.Unlock()
	
	spans := exporter.GetSpans()
	require.Len(t, spans, 5) // parent + attempt + 3 steps
	var parentSpanID trace.SpanID
	var parentTraceID trace.TraceID
	var attemptSpanID trace.SpanID
	for _, span := range spans {
		if span.Name == "analyzer.llmapi.test" {
			parentSpanID = span.SpanContext.SpanID()
			parentTraceID = span.SpanContext.TraceID()
		} else if span.Name == "attempt" {
			attemptSpanID = span.SpanContext.SpanID()
		}
	}
	require.NotEqual(t, trace.SpanID{}, parentSpanID)
	require.NotEqual(t, trace.SpanID{}, attemptSpanID)
	for _, span := range spans {
		if span.Name == "analyzer.llmapi.test" {
			continue
		} else if span.Name == "attempt" {
			require.Equal(t, parentSpanID, span.Parent.SpanID())
		} else {
			require.Equal(t, attemptSpanID, span.Parent.SpanID())
			require.Equal(t, parentTraceID, span.SpanContext.TraceID())
		}
	}
}

func TestRunnerDoReAttemptsOnReAttemptError(t *testing.T) {
	var logs bytes.Buffer
	exporter := tracetest.NewInMemoryExporter()
	stage := &fakeStage{
		name: "test",
		failAt: []string{"post_process"},
		errs: []error{&llmapi.ReAttemptError{Cause: errors.New("invalid"), Hint: &llmapi.RepairHint{}}},
	}
	runner := newRunner(t, stage, 3, exporter, &logs)

	out, err := runner.Do(context.Background(), "input")
	require.NoError(t, err)
	require.Equal(t, "success", out)

	stage.mu.Lock()
	require.Equal(t, []string{
		"pre_process", "api_call", "post_process",
		"pre_process", "api_call", "post_process",
	}, stage.order)
	require.Len(t, stage.packets, 6)
	
	p1 := stage.packets[0]
	p2 := stage.packets[3]
	require.NotSame(t, p1, p2)
	require.Nil(t, p1.RepairHint)
	require.NotNil(t, p2.RepairHint)
	stage.mu.Unlock()
}

func TestRunnerDoAbortsImmediatelyOnRetryError(t *testing.T) {
	var logs bytes.Buffer
	exporter := tracetest.NewInMemoryExporter()
	stage := &fakeStage{
		name: "test",
		failAt: []string{"api_call"},
		errs: []error{&llmapi.RetryError{Cause: errors.New("rate limited")}},
	}
	runner := newRunner(t, stage, 3, exporter, &logs)

	_, err := runner.Do(context.Background(), "input")
	var retryErr *llmapi.RetryError
	require.ErrorAs(t, err, &retryErr)

	stage.mu.Lock()
	require.Equal(t, []string{"pre_process", "api_call"}, stage.order)
	stage.mu.Unlock()
}

func TestRunnerDoAbortsImmediatelyOnFatalError(t *testing.T) {
	var logs bytes.Buffer
	exporter := tracetest.NewInMemoryExporter()
	stage := &fakeStage{
		name: "test",
		failAt: []string{"post_process"},
		errs: []error{errors.New("fatal")},
	}
	runner := newRunner(t, stage, 3, exporter, &logs)

	_, err := runner.Do(context.Background(), "input")
	require.ErrorContains(t, err, "fatal")

	stage.mu.Lock()
	require.Equal(t, []string{"pre_process", "api_call", "post_process"}, stage.order)
	stage.mu.Unlock()
}

func TestRunnerDoExhaustsMaxAttempts(t *testing.T) {
	var logs bytes.Buffer
	exporter := tracetest.NewInMemoryExporter()
	
	errs := []error{
		&llmapi.ReAttemptError{Cause: errors.New("e1")},
		&llmapi.ReAttemptError{Cause: errors.New("e2")},
		&llmapi.ReAttemptError{Cause: errors.New("e3")},
	}
	stage := &fakeStage{
		name: "test",
		failAt: []string{"post_process", "post_process", "post_process"},
		errs: errs,
	}
	runner := newRunner(t, stage, 3, exporter, &logs)

	_, err := runner.Do(context.Background(), "input")
	require.ErrorContains(t, err, "max semantic attempts exhausted")
	require.ErrorContains(t, err, "e3")

	stage.mu.Lock()
	require.Equal(t, 9, len(stage.order))
	stage.mu.Unlock()
}

func TestRunnerDoRecordsParentAndChildSpanErrors(t *testing.T) {
	var logs bytes.Buffer
	exporter := tracetest.NewInMemoryExporter()
	stage := &fakeStage{
		name: "test", 
		failAt: []string{"api_call"}, 
		errs: []error{context.Canceled},
	}
	runner := newRunner(t, stage, 1, exporter, &logs)

	_, err := runner.Do(context.Background(), "input")
	require.ErrorIs(t, err, context.Canceled)

	spans := exporter.GetSpans()
	require.Len(t, spans, 4)
	for _, span := range spans {
		if span.Name == "analyzer.llmapi.test" || span.Name == "attempt" || span.Name == "api_call" {
			require.Equal(t, codes.Error, span.Status.Code)
			requireExceptionEvent(t, span, "api_call: context canceled")
		}
		require.NotEqual(t, "post_process", span.Name)
	}
}

func TestRunnerDoesNotLogPacketPayloads(t *testing.T) {
	var logs bytes.Buffer
	exporter := tracetest.NewInMemoryExporter()
	stage := &fakeStage{
		name: "test", 
		failAt: []string{"post_process"}, 
		errs: []error{errors.New("stage failure")},
	}
	runner := newRunner(t, stage, 1, exporter, &logs)

	_, err := runner.Do(context.Background(), "secret-input")
	require.Error(t, err)
	require.NotContains(t, logs.String(), "secret-input")
}

func TestRunnerDoIsSafeForConcurrentCalls(t *testing.T) {
	var logs bytes.Buffer
	exporter := tracetest.NewInMemoryExporter()
	stage := &fakeStage{name: "test"}
	runner := newRunner(t, stage, 1, exporter, &logs)

	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := runner.Do(context.Background(), "input")
			errs <- err
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
		// Our runner wraps errors differently now, so we just check Contains instead of Equal
		require.Contains(t, message, wantMessage)
		return
	}
	require.Failf(t, "missing exception event", "span=%s", span.Name)
}
