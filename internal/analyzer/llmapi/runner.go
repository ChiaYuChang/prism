package llmapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var (
	// ErrParamMissing indicates a missing Runner constructor dependency.
	ErrParamMissing = errors.New("param missing")
	// ErrNilPacket indicates that Runner.Do received no execution Packet.
	ErrNilPacket = errors.New("packet is nil")
)

// Runner executes a Stage in the fixed PreProcess, APICall, PostProcess order.
// Runner is safe for concurrent use when its Stage and dependencies are safe.
type Runner[I, S, O any] struct {
	tracer     trace.Tracer
	logger     *slog.Logger
	stage      Stage[I, S, O]
	stageName  string
	maxAttempt int
}

// NewRunner creates a Runner for stage with the supplied observability dependencies.
func NewRunner[I, S, O any](tracer trace.Tracer, logger *slog.Logger, maxAttempt int, stage Stage[I, S, O]) (*Runner[I, S, O], error) {
	if isNil(tracer) {
		return nil, fmt.Errorf("%w: tracer", ErrParamMissing)
	}
	if logger == nil {
		return nil, fmt.Errorf("%w: logger", ErrParamMissing)
	}
	if maxAttempt < 1 {
		return nil, fmt.Errorf("%w: maxAttempt must be >= 1", ErrParamMissing)
	}
	if isNil(stage) {
		return nil, fmt.Errorf("%w: stage", ErrParamMissing)
	}
	stageName := stage.Name()
	if strings.TrimSpace(stageName) == "" {
		return nil, fmt.Errorf("%w: stage name", ErrParamMissing)
	}

	return &Runner[I, S, O]{
		tracer: tracer,
		logger: logger.With(
			slog.String("component", "analyzer.llmapi.runner"),
			slog.String("stage", stageName),
		),
		stage:      stage,
		stageName:  stageName,
		maxAttempt: maxAttempt,
	}, nil
}

// Do executes the Stage lifecycle and returns the output or the first unrecoverable failure.
func (r *Runner[I, S, O]) Do(ctx context.Context, input I) (output O, retErr error) {
	ctx, span := r.tracer.Start(
		ctx,
		"analyzer.llmapi."+r.stageName,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("analyzer.stage", r.stageName),
		),
	)

	defer func() {
		if retErr != nil {
			span.RecordError(retErr)
			span.SetStatus(codes.Error, retErr.Error())
			r.logger.ErrorContext(
				ctx,
				"LLM stage failed",
				slog.Any("error", retErr),
			)
		}
		span.End()
	}()

	var repairHint *RepairHint
	var lastReAttemptErr error

	for attempt := 1; attempt <= r.maxAttempt; attempt++ {
		p := NewPacket[I, S, O](input, repairHint)

		err := r.doAttempt(ctx, p, attempt)
		if err == nil {
			return p.Output, nil
		}

		if reAttemptErr, ok := errors.AsType[*ReAttemptError](err); ok {
			repairHint = reAttemptErr.Hint
			lastReAttemptErr = err
			continue
		}

		return output, err
	}

	return output, fmt.Errorf(
		"max semantic attempts exhausted: %w",
		lastReAttemptErr,
	)
}

func (r *Runner[I, S, O]) doAttempt(ctx context.Context, p *Packet[I, S, O], attempt int) (err error) {
	ctx, span := r.tracer.Start(
		ctx,
		"attempt",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("analyzer.stage", r.stageName),
			attribute.Int("analyzer.attempt", attempt),
		),
	)
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	if err = r.step(ctx, StepPreProcess, func(stepCtx context.Context) error {
		return r.stage.PreProcess(stepCtx, p)
	}); err != nil {
		return err
	}
	if err = r.step(ctx, StepAPICall, func(stepCtx context.Context) error {
		return r.stage.APICall(stepCtx, p)
	}); err != nil {
		return err
	}
	if err = r.step(ctx, StepPostProcess, func(stepCtx context.Context) error {
		return r.stage.PostProcess(stepCtx, p)
	}); err != nil {
		return err
	}

	return nil
}

func (r *Runner[I, S, O]) step(ctx context.Context, step Step, fn func(context.Context) error) (err error) {
	ctx, span := r.tracer.Start(
		ctx,
		string(step),
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("analyzer.stage", r.stageName),
			attribute.String("analyzer.step", string(step)),
		),
	)
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	if err = fn(ctx); err != nil {
		return fmt.Errorf("%s: %w", step, err)
	}
	return nil
}

func isNil(value any) bool {
	if value == nil {
		return true
	}

	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}
