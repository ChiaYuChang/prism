package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/ChiaYuChang/prism/internal/discovery"
	"github.com/ChiaYuChang/prism/internal/message"
	wm "github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
)

var (
	ErrParamMissing     = errors.New("param missing")
	ErrInvalidSignal    = errors.New("invalid batch completed signal")
	ErrNoPlannerTargets = errors.New("planner target is missing")
)

type Handler struct {
	logger  *slog.Logger
	tracer  trace.Tracer
	planner discovery.Planner
	targets []discovery.PlannerTarget
}

func NewHandler(logger *slog.Logger, tracer trace.Tracer, planner discovery.Planner, targets []discovery.PlannerTarget) (*Handler, error) {
	if logger == nil {
		return nil, fmt.Errorf("%w: logger", ErrParamMissing)
	}
	if tracer == nil {
		return nil, fmt.Errorf("%w: tracer", ErrParamMissing)
	}
	if planner == nil {
		return nil, fmt.Errorf("%w: planner", ErrParamMissing)
	}
	if len(targets) == 0 {
		return nil, ErrNoPlannerTargets
	}
	return &Handler{logger: logger, tracer: tracer, planner: planner, targets: targets}, nil
}

func (h *Handler) HandleMessage(ctx context.Context, msg *wm.Message) (bool, error) {
	var sig message.BatchCompletedSignal
	if err := sig.Unmarshal(msg.Payload); err != nil {
		return true, fmt.Errorf("%w: %w", ErrInvalidSignal, err)
	}
	if sig.BatchID == uuid.Nil {
		return true, fmt.Errorf("%w: batch_id is empty", ErrInvalidSignal)
	}

	ctx, span := h.tracer.Start(ctx, "worker.planner.handle_message")
	defer span.End()

	targets, err := h.loadTargets(ctx)
	if err != nil {
		return true, err
	}

	result, err := h.planner.Plan(ctx, discovery.PlannerRequest{
		BatchID: sig.BatchID,
		TraceID: sig.TraceID,
		Targets: targets,
	})
	if err != nil {
		return true, err
	}

	h.logger.InfoContext(ctx, "planner completed",
		slog.String("msg_id", msg.UUID),
		slog.String("batch_id", sig.BatchID.String()),
		slog.String("trace_id", sig.TraceID),
		slog.Int("targets", len(targets)),
		slog.Int("seed_contents", result.SeedContents),
		slog.Int("extractions", result.Extractions),
		slog.Int("unique_phrases", result.UniquePhrases),
		slog.Int("tasks_created", result.TasksCreated),
	)
	return true, nil
}

func (h *Handler) loadTargets(_ context.Context) ([]discovery.PlannerTarget, error) {
	if len(h.targets) == 0 {
		return nil, ErrNoPlannerTargets
	}
	return h.targets, nil
}
