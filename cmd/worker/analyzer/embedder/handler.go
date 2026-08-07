package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ChiaYuChang/prism/internal/analyzer/embedder"
	"github.com/ChiaYuChang/prism/internal/llm"
	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/internal/repo"
	wm "github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const (
	HandleMessageSpanName = "worker.embedder.handle_message"
)

var (
	ErrParamMissing      = errors.New("param missing")
	ErrInvalidTaskSignal = errors.New("invalid embedding task signal")
)

type TaskReader interface {
	IsTaskRunning(ctx context.Context, id uuid.UUID) (bool, error)
}

type Store struct {
	Scout      repo.Scout
	Pipeline   repo.Pipeline
	Embeddings repo.Embeddings
	Reporter   repo.TaskReporter
	Tasks      repo.Tasks
}

type EmbedderConfig struct {
	Embedder  llm.Embedder
	ModelID   int16
	ModelName string
	Dimension int
	RetryMax  int
}

type HandlerConfig struct {
	Logger   *slog.Logger
	Tracer   trace.Tracer
	Embedder EmbedderConfig
	Store    Store
	Metrics  *metrics
}

type Handler struct {
	Logger     *slog.Logger
	Tracer     trace.Tracer
	Service    *embedder.Service
	Reporter   repo.TaskReporter
	TaskReader TaskReader
	Metrics    *metrics
	RetryMax   int
}

type metrics struct {
	Tasks            metric.Int64Counter
	TaskDuration     metric.Float64Histogram
	ValidationErrors metric.Int64Counter
}

func newMetrics(meter metric.Meter) (*metrics, error) {
	tasks, err := meter.Int64Counter(
		"prism.embedder.tasks",
		metric.WithDescription("Count of embedder task outcomes."),
		metric.WithUnit("{task}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create embedder task counter: %w", err)
	}

	taskDuration, err := meter.Float64Histogram(
		"prism.embedder.task.duration",
		metric.WithDescription("Embedder task handling duration."),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("create embedder task duration histogram: %w", err)
	}

	validationErrors, err := meter.Int64Counter(
		"prism.embedder.validation.errors",
		metric.WithDescription("Count of invalid embedding responses."),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create embedder validation counter: %w", err)
	}

	return &metrics{
		Tasks: tasks, TaskDuration: taskDuration, ValidationErrors: validationErrors,
	}, nil
}

func (m *metrics) recordTask(ctx context.Context, sig message.TaskSignal, result string, started time.Time) {
	if m == nil {
		return
	}
	attrs := metric.WithAttributes(
		attribute.String("task.kind", strings.TrimSpace(sig.Kind)),
		attribute.String("source.type", strings.TrimSpace(sig.SourceType)),
		attribute.String("result", result),
	)
	m.Tasks.Add(ctx, 1, attrs)
	m.TaskDuration.Record(ctx, time.Since(started).Seconds(), attrs)
}

func NewHandler(cfg HandlerConfig) (*Handler, error) {
	if cfg.Logger == nil {
		return nil, fmt.Errorf("%w: logger", ErrParamMissing)
	}
	if cfg.Tracer == nil {
		return nil, fmt.Errorf("%w: tracer", ErrParamMissing)
	}
	if cfg.Embedder.Embedder == nil {
		return nil, fmt.Errorf("%w: embedder", ErrParamMissing)
	}
	if cfg.Store.Scout == nil {
		return nil, fmt.Errorf("%w: scout", ErrParamMissing)
	}
	if cfg.Store.Pipeline == nil {
		return nil, fmt.Errorf("%w: pipeline", ErrParamMissing)
	}
	if cfg.Store.Embeddings == nil {
		return nil, fmt.Errorf("%w: embeddings", ErrParamMissing)
	}
	if cfg.Store.Reporter == nil {
		return nil, fmt.Errorf("%w: reporter", ErrParamMissing)
	}
	if cfg.Embedder.ModelID < 1 {
		return nil, fmt.Errorf("%w: model_id", ErrParamMissing)
	}
	if strings.TrimSpace(cfg.Embedder.ModelName) == "" {
		return nil, fmt.Errorf("%w: model_name", ErrParamMissing)
	}
	if cfg.Embedder.Dimension < 1 {
		return nil, fmt.Errorf("%w: dimension", ErrParamMissing)
	}
	if cfg.Embedder.RetryMax < 1 {
		return nil, fmt.Errorf("%w: retry_max", ErrParamMissing)
	}

	service, err := embedder.NewService(
		cfg.Logger, cfg.Tracer, cfg.Embedder.Embedder,
		cfg.Store.Scout, cfg.Store.Pipeline, cfg.Store.Embeddings,
		cfg.Embedder.ModelID, cfg.Embedder.ModelName, cfg.Embedder.Dimension,
	)
	if err != nil {
		return nil, fmt.Errorf("create embedder service: %w", err)
	}

	var tr TaskReader
	if cfg.Store.Tasks != nil {
		tr = cfg.Store.Tasks
	}

	return &Handler{
		Logger:     cfg.Logger,
		Tracer:     cfg.Tracer,
		Service:    service,
		Reporter:   cfg.Store.Reporter,
		TaskReader: tr,
		Metrics:    cfg.Metrics,
		RetryMax:   cfg.Embedder.RetryMax,
	}, nil
}

func (h *Handler) HandleMessage(ctx context.Context, msg *wm.Message) (bool, error) {
	started := time.Now()
	var sig message.TaskSignal
	if err := json.Unmarshal(msg.Payload, &sig); err != nil {
		h.Metrics.recordTask(ctx, sig, "invalid", started)
		return true, fmt.Errorf("%w: decode task signal: %w", ErrInvalidTaskSignal, err)
	}
	if sig.TaskID == uuid.Nil {
		h.Metrics.recordTask(ctx, sig, "invalid", started)
		return true, fmt.Errorf("%w: task_id is empty", ErrInvalidTaskSignal)
	}
	if sig.Kind != repo.TaskKindEmbedCandidate && sig.Kind != repo.TaskKindEmbedContent {
		h.Metrics.recordTask(ctx, sig, "ignored", started)
		return true, nil
	}
	ctx, err := message.ExtractTraceContext(ctx, msg)
	if err != nil {
		h.Metrics.recordTask(ctx, sig, "invalid", started)
		return true, fmt.Errorf("extract trace context: %w", err)
	}
	ctx = obs.WithTraceID(ctx, sig.TraceID)
	ctx, span := h.Tracer.Start(ctx, HandleMessageSpanName)
	defer span.End()

	if h.TaskReader != nil {
		running, err := h.TaskReader.IsTaskRunning(ctx, sig.TaskID)
		if err != nil {
			h.Metrics.recordTask(ctx, sig, "nacked", started)
			return false, fmt.Errorf("get task %s before processing: %w", sig.TaskID, err)
		}
		if !running {
			h.Metrics.recordTask(ctx, sig, "ignored", started)
			return true, nil
		}
	}

	if err := h.process(ctx, sig); err != nil {
		if failErr := h.Reporter.FailTask(ctx, sig.TaskID, h.RetryMax, err.Error()); failErr != nil {
			h.Metrics.recordTask(ctx, sig, "nacked", started)
			return false, fmt.Errorf("process task %s: %w; mark failed: %w", sig.TaskID, err, failErr)
		}
		h.Metrics.recordTask(ctx, sig, "failed", started)
		return true, err
	}
	if err := h.Reporter.CompleteTask(ctx, sig.TaskID); err != nil {
		h.Metrics.recordTask(ctx, sig, "nacked", started)
		return false, fmt.Errorf("complete task %s: %w", sig.TaskID, err)
	}
	h.Metrics.recordTask(ctx, sig, "ok", started)
	return true, nil
}

func (h *Handler) process(ctx context.Context, sig message.TaskSignal) error {
	targetID, err := targetID(sig.Meta, sig.Kind)
	if err != nil {
		return err
	}
	switch sig.Kind {
	case repo.TaskKindEmbedCandidate:
		var meta struct {
			Snapshot *repo.Candidate `json:"snapshot"`
		}
		if err := json.Unmarshal(sig.Meta, &meta); err != nil {
			return fmt.Errorf("%w: decode candidate snapshot: %w", ErrInvalidTaskSignal, err)
		}
		if meta.Snapshot != nil {
			if meta.Snapshot.ID != targetID {
				return fmt.Errorf("%w: candidate snapshot ID does not match candidate_id", ErrInvalidTaskSignal)
			}
			return h.Service.EmbedCandidateSnapshot(ctx, *meta.Snapshot, sig.TraceID)
		}
		return h.Service.EmbedCandidate(ctx, targetID, sig.TraceID)
	case repo.TaskKindEmbedContent:
		var meta struct {
			Snapshot *repo.Content `json:"snapshot"`
		}
		if err := json.Unmarshal(sig.Meta, &meta); err != nil {
			return fmt.Errorf("%w: decode content snapshot: %w", ErrInvalidTaskSignal, err)
		}
		if meta.Snapshot != nil {
			if meta.Snapshot.ID != targetID {
				return fmt.Errorf("%w: content snapshot ID does not match content_id", ErrInvalidTaskSignal)
			}
			return h.Service.EmbedContentSnapshot(ctx, *meta.Snapshot, sig.TraceID)
		}
		return h.Service.EmbedContent(ctx, targetID, sig.TraceID)
	default:
		return nil
	}
}

func targetID(meta json.RawMessage, kind string) (uuid.UUID, error) {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(meta, &values); err != nil {
		return uuid.Nil, fmt.Errorf("%w: decode %s metadata: %w", ErrInvalidTaskSignal, kind, err)
	}
	key := "candidate_id"
	if kind == repo.TaskKindEmbedContent {
		key = "content_id"
	}
	var rawID string
	if err := json.Unmarshal(values[key], &rawID); err != nil {
		return uuid.Nil, fmt.Errorf("%w: %s is missing or invalid", ErrInvalidTaskSignal, key)
	}
	id, err := uuid.Parse(rawID)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%w: %s is missing or invalid", ErrInvalidTaskSignal, key)
	}
	return id, nil
}
