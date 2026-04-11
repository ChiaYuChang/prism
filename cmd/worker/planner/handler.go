package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"

	"github.com/ChiaYuChang/prism/internal/discovery"
	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/repo"
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
	scout   repo.Scout
}

func NewHandler(logger *slog.Logger, tracer trace.Tracer, planner discovery.Planner, scout repo.Scout) (*Handler, error) {
	if logger == nil {
		return nil, fmt.Errorf("%w: logger", ErrParamMissing)
	}
	if tracer == nil {
		return nil, fmt.Errorf("%w: tracer", ErrParamMissing)
	}
	if planner == nil {
		return nil, fmt.Errorf("%w: planner", ErrParamMissing)
	}
	if scout == nil {
		return nil, fmt.Errorf("%w: scout_repository", ErrParamMissing)
	}
	return &Handler{logger: logger, tracer: tracer, planner: planner, scout: scout}, nil
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

func (h *Handler) loadTargets(ctx context.Context) ([]discovery.PlannerTarget, error) {
	sources, err := h.scout.ListSourcesByType(ctx, "MEDIA")
	if err != nil {
		return nil, fmt.Errorf("list media sources: %w", err)
	}

	targets := make([]discovery.PlannerTarget, 0, len(sources))
	for _, source := range sources {
		site := ""
		if u, err := url.Parse(source.BaseURL); err == nil {
			site = u.Hostname()
		}
		targets = append(targets, discovery.PlannerTarget{
			SourceID: source.ID,
			URL:      source.BaseURL,
			Site:     site,
		})
	}
	if len(targets) == 0 {
		return nil, ErrNoPlannerTargets
	}
	return targets, nil
}
