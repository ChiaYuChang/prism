package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/ChiaYuChang/prism/internal/discovery"
	discoverysink "github.com/ChiaYuChang/prism/internal/discovery/sink"
	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/internal/repo"
	wm "github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
)

const (
	SpanNameHandleMessage  = "worker.discovery.handle_message"
	TaskKindDirectoryFetch = "DIRECTORY_FETCH"
	SourceTypeParty        = "PARTY"
)

var (
	ErrParamMissing          = errors.New("param missing")
	ErrInvalidTaskSignal     = errors.New("invalid task signal")
	ErrUnsupportedTaskKind   = errors.New("unsupported task kind")
	ErrUnsupportedSourceType = errors.New("unsupported source type")
	ErrSourceMismatch        = errors.New("source mismatch")
)

type Handler struct {
	logger    *slog.Logger
	tracer    trace.Tracer
	scout     discovery.Scout
	sink      discoverysink.CandidateSink
	scoutRepo repo.Scout
	scheduler repo.Scheduler
}

func NewHandler(
	logger *slog.Logger,
	tracer trace.Tracer,
	scout discovery.Scout,
	sink discoverysink.CandidateSink,
	scoutRepo repo.Scout,
	scheduler repo.Scheduler,
) (*Handler, error) {
	if logger == nil {
		return nil, fmt.Errorf("%w: logger", ErrParamMissing)
	}
	if tracer == nil {
		return nil, fmt.Errorf("%w: tracer", ErrParamMissing)
	}
	if scout == nil {
		return nil, fmt.Errorf("%w: scout", ErrParamMissing)
	}
	if sink == nil {
		return nil, fmt.Errorf("%w: sink", ErrParamMissing)
	}
	if scoutRepo == nil {
		return nil, fmt.Errorf("%w: scout_repository", ErrParamMissing)
	}
	if scheduler == nil {
		return nil, fmt.Errorf("%w: scheduler_repository", ErrParamMissing)
	}

	return &Handler{
		logger:    logger,
		tracer:    tracer,
		scout:     scout,
		sink:      sink,
		scoutRepo: scoutRepo,
		scheduler: scheduler,
	}, nil
}

func (h *Handler) HandleMessage(ctx context.Context, msg *wm.Message) (bool, error) {
	var sig message.TaskSignal
	if err := json.Unmarshal(msg.Payload, &sig); err != nil {
		return true, fmt.Errorf("%w: decode task signal: %w", ErrInvalidTaskSignal, err)
	}
	if sig.TaskID == uuid.Nil {
		return true, fmt.Errorf("%w: task_id is empty", ErrInvalidTaskSignal)
	}

	ctx = obs.WithTraceID(ctx, sig.TraceID)
	ctx, span := h.tracer.Start(ctx, SpanNameHandleMessage)
	defer span.End()

	logger := h.logger.With(
		slog.String("task_id", sig.TaskID.String()),
		slog.String("trace_id", sig.TraceID),
		slog.String("kind", sig.Kind),
		slog.String("source_type", sig.SourceType),
		slog.Int("source_id", int(sig.SourceID)),
		slog.String("url", sig.URL),
	)

	if err := h.process(ctx, sig); err != nil {
		logger.ErrorContext(ctx, "discovery task failed", "error", err)
		if failErr := h.scheduler.FailTask(ctx, sig.TaskID); failErr != nil {
			return false, fmt.Errorf("process task %s: %w; mark failed: %w", sig.TaskID, err, failErr)
		}
		return true, err
	}

	if err := h.scheduler.CompleteTask(ctx, sig.TaskID); err != nil {
		return false, fmt.Errorf("complete task %s: %w", sig.TaskID, err)
	}

	logger.InfoContext(ctx, "discovery task completed")
	return true, nil
}

func (h *Handler) process(ctx context.Context, sig message.TaskSignal) error {
	if sig.Kind != TaskKindDirectoryFetch {
		return fmt.Errorf("%w: %s", ErrUnsupportedTaskKind, sig.Kind)
	}
	if sig.SourceType != SourceTypeParty {
		return fmt.Errorf("%w: %s", ErrUnsupportedSourceType, sig.SourceType)
	}

	source, err := h.scoutRepo.GetSourceByID(ctx, sig.SourceID)
	if err != nil {
		return fmt.Errorf("get source by id %d: %w", sig.SourceID, err)
	}
	if source.Type != sig.SourceType {
		return fmt.Errorf("%w: db source type %s != signal source type %s", ErrSourceMismatch, source.Type, sig.SourceType)
	}
	if err := validateTaskURL(source.BaseURL, sig.URL); err != nil {
		return err
	}

	candidates, err := h.scout.Discover(ctx, sig.URL)
	if err != nil {
		return fmt.Errorf("discover candidates from %s: %w", sig.URL, err)
	}

	batchID := sig.BatchID
	if err := h.sink.Handle(ctx, discoverysink.CandidateSinkRequest{
		SourceURL:       sig.URL,
		SourceID:        sig.SourceID,
		SourceType:      sig.SourceType,
		BatchID:         batchID,
		TraceID:         sig.TraceID,
		IngestionMethod: "DIRECTORY",
		DefaultMetadata: map[string]any{
			"task_id":     sig.TaskID.String(),
			"task_kind":   sig.Kind,
			"source_type": sig.SourceType,
		},
		Candidates: candidates,
	}); err != nil {
		return fmt.Errorf("sink candidates from %s: %w", sig.URL, err)
	}

	return nil
}

func validateTaskURL(baseURL, rawURL string) error {
	base, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("%w: parse source base_url: %w", ErrSourceMismatch, err)
	}
	target, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("%w: parse task url: %w", ErrSourceMismatch, err)
	}

	baseHost := strings.ToLower(base.Hostname())
	targetHost := strings.ToLower(target.Hostname())
	if baseHost == "" || targetHost == "" {
		return fmt.Errorf("%w: empty host in base_url=%q or url=%q", ErrSourceMismatch, baseURL, rawURL)
	}
	if baseHost != targetHost {
		return fmt.Errorf("%w: base host %s != task host %s", ErrSourceMismatch, baseHost, targetHost)
	}
	return nil
}
