package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ChiaYuChang/prism/internal/llm"
	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/internal/repo"
	wm "github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

var (
	ErrParamMissing      = errors.New("param missing")
	ErrInvalidTaskSignal = errors.New("invalid embedding task signal")
	ErrInvalidEmbedding  = errors.New("invalid embedding response")
)

type taskReader interface {
	IsTaskRunning(ctx context.Context, id uuid.UUID) (bool, error)
}

type Handler struct {
	logger     *slog.Logger
	tracer     trace.Tracer
	embedder   llm.Embedder
	scout      repo.Scout
	pipeline   repo.Pipeline
	embeddings repo.Embeddings
	reporter   repo.TaskReporter
	taskReader taskReader
	metrics    *metrics
	modelID    int16
	model      string
	dimension  int
	retryMax   int
}

type metrics struct {
	tasks            metric.Int64Counter
	taskDuration     metric.Float64Histogram
	inputs           metric.Int64Counter
	inputBytes       metric.Int64Counter
	vectors          metric.Int64Counter
	validationErrors metric.Int64Counter
}

func newMetrics(meter metric.Meter) (*metrics, error) {
	tasks, err := meter.Int64Counter("prism.embedder.tasks", metric.WithDescription("Count of embedder task outcomes."), metric.WithUnit("{task}"))
	if err != nil {
		return nil, fmt.Errorf("create embedder task counter: %w", err)
	}
	taskDuration, err := meter.Float64Histogram("prism.embedder.task.duration", metric.WithDescription("Embedder task handling duration."), metric.WithUnit("s"))
	if err != nil {
		return nil, fmt.Errorf("create embedder task duration histogram: %w", err)
	}
	inputs, err := meter.Int64Counter("prism.embedder.inputs", metric.WithDescription("Count of embedding inputs sent."), metric.WithUnit("{input}"))
	if err != nil {
		return nil, fmt.Errorf("create embedder input counter: %w", err)
	}
	inputBytes, err := meter.Int64Counter("prism.embedder.input.bytes", metric.WithDescription("Bytes sent to the embedding provider."), metric.WithUnit("By"))
	if err != nil {
		return nil, fmt.Errorf("create embedder input bytes counter: %w", err)
	}
	vectors, err := meter.Int64Counter("prism.embedder.vectors", metric.WithDescription("Count of vectors persisted."), metric.WithUnit("{vector}"))
	if err != nil {
		return nil, fmt.Errorf("create embedder vector counter: %w", err)
	}
	validationErrors, err := meter.Int64Counter("prism.embedder.validation.errors", metric.WithDescription("Count of invalid embedding responses."), metric.WithUnit("{error}"))
	if err != nil {
		return nil, fmt.Errorf("create embedder validation counter: %w", err)
	}
	return &metrics{
		tasks: tasks, taskDuration: taskDuration, inputs: inputs, inputBytes: inputBytes,
		vectors: vectors, validationErrors: validationErrors,
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
	m.tasks.Add(ctx, 1, attrs)
	m.taskDuration.Record(ctx, time.Since(started).Seconds(), attrs)
}

func NewHandler(logger *slog.Logger, tracer trace.Tracer, embedder llm.Embedder, scout repo.Scout, pipeline repo.Pipeline, embeddings repo.Embeddings, reporter repo.TaskReporter, metrics *metrics, modelID int16, model string, dimension int, retryMax int) (*Handler, error) {
	if logger == nil {
		return nil, fmt.Errorf("%w: logger", ErrParamMissing)
	}
	if tracer == nil {
		return nil, fmt.Errorf("%w: tracer", ErrParamMissing)
	}
	if embedder == nil {
		return nil, fmt.Errorf("%w: embedder", ErrParamMissing)
	}
	if scout == nil {
		return nil, fmt.Errorf("%w: scout", ErrParamMissing)
	}
	if pipeline == nil {
		return nil, fmt.Errorf("%w: pipeline", ErrParamMissing)
	}
	if embeddings == nil {
		return nil, fmt.Errorf("%w: embeddings", ErrParamMissing)
	}
	if reporter == nil {
		return nil, fmt.Errorf("%w: reporter", ErrParamMissing)
	}
	if modelID < 1 {
		return nil, fmt.Errorf("%w: model_id", ErrParamMissing)
	}
	if strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("%w: model", ErrParamMissing)
	}
	if dimension < 1 {
		return nil, fmt.Errorf("%w: dimension", ErrParamMissing)
	}
	if retryMax < 1 {
		return nil, fmt.Errorf("%w: retry_max", ErrParamMissing)
	}
	return &Handler{
		logger: logger, tracer: tracer, embedder: embedder, scout: scout, pipeline: pipeline,
		embeddings: embeddings, reporter: reporter, metrics: metrics, modelID: modelID,
		model: model, dimension: dimension, retryMax: retryMax,
	}, nil
}

func (h *Handler) HandleMessage(ctx context.Context, msg *wm.Message) (bool, error) {
	started := time.Now()
	var sig message.TaskSignal
	if err := json.Unmarshal(msg.Payload, &sig); err != nil {
		h.metrics.recordTask(ctx, sig, "invalid", started)
		return true, fmt.Errorf("%w: decode task signal: %w", ErrInvalidTaskSignal, err)
	}
	if sig.TaskID == uuid.Nil {
		h.metrics.recordTask(ctx, sig, "invalid", started)
		return true, fmt.Errorf("%w: task_id is empty", ErrInvalidTaskSignal)
	}
	if sig.Kind != repo.TaskKindEmbedCandidate && sig.Kind != repo.TaskKindEmbedContent {
		h.metrics.recordTask(ctx, sig, "ignored", started)
		return true, nil
	}
	ctx, err := message.ExtractTraceContext(ctx, msg)
	if err != nil {
		h.metrics.recordTask(ctx, sig, "invalid", started)
		return true, fmt.Errorf("extract trace context: %w", err)
	}
	ctx = obs.WithTraceID(ctx, sig.TraceID)
	ctx, span := h.tracer.Start(ctx, "worker.embedder.handle_message")
	defer span.End()

	if h.taskReader != nil {
		running, err := h.taskReader.IsTaskRunning(ctx, sig.TaskID)
		if err != nil {
			h.metrics.recordTask(ctx, sig, "nacked", started)
			return false, fmt.Errorf("get task %s before processing: %w", sig.TaskID, err)
		}
		if !running {
			h.metrics.recordTask(ctx, sig, "ignored", started)
			return true, nil
		}
	}

	if err := h.process(ctx, sig); err != nil {
		if failErr := h.reporter.FailTask(ctx, sig.TaskID, h.retryMax, err.Error()); failErr != nil {
			h.metrics.recordTask(ctx, sig, "nacked", started)
			return false, fmt.Errorf("process task %s: %w; mark failed: %w", sig.TaskID, err, failErr)
		}
		h.metrics.recordTask(ctx, sig, "failed", started)
		return true, err
	}
	if err := h.reporter.CompleteTask(ctx, sig.TaskID); err != nil {
		h.metrics.recordTask(ctx, sig, "nacked", started)
		return false, fmt.Errorf("complete task %s: %w", sig.TaskID, err)
	}
	h.metrics.recordTask(ctx, sig, "ok", started)
	return true, nil
}

func (h *Handler) process(ctx context.Context, sig message.TaskSignal) error {
	targetID, err := targetID(sig.Meta, sig.Kind)
	if err != nil {
		return err
	}
	switch sig.Kind {
	case repo.TaskKindEmbedCandidate:
		candidate, err := h.scout.GetCandidateByID(ctx, targetID)
		if err != nil {
			return fmt.Errorf("get candidate %s: %w", targetID, err)
		}
		return h.embedCandidate(ctx, candidate, sig.TraceID)
	case repo.TaskKindEmbedContent:
		content, err := h.pipeline.GetContentByID(ctx, targetID)
		if err != nil {
			return fmt.Errorf("get content %s: %w", targetID, err)
		}
		if content.DeletedAt != nil {
			return nil
		}
		return h.embedContent(ctx, content, sig.TraceID)
	default:
		return nil
	}
}

type embeddingInput struct {
	category string
	text     string
}

func (h *Handler) embedCandidate(ctx context.Context, candidate repo.Candidate, traceID string) error {
	inputs := make([]embeddingInput, 0, 2)
	if text := strings.TrimSpace(candidate.Title); text != "" {
		inputs = append(inputs, embeddingInput{category: repo.EmbeddingCategoryTitle, text: text})
	}
	if candidate.Description != nil {
		if text := strings.TrimSpace(*candidate.Description); text != "" {
			inputs = append(inputs, embeddingInput{category: repo.EmbeddingCategoryBrief, text: text})
		}
	}
	if len(inputs) == 0 {
		return fmt.Errorf("candidate %s has no embeddable text", candidate.ID)
	}
	return h.embedInputs(ctx, candidate.ID, inputs, traceID, func(input embeddingInput, vector []float32, hash string) error {
		_, err := h.embeddings.UpsertCandidateEmbedding(ctx, repo.CreateCandidateEmbeddingParams{
			CandidateID: candidate.ID, ModelID: h.modelID, Category: input.category,
			InputHash: hash, Vector: vector, TraceID: traceID,
		})
		return err
	})
}

func (h *Handler) embedContent(ctx context.Context, content repo.Content, traceID string) error {
	text := canonicalDocumentText(content)
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("content %s has no embeddable text", content.ID)
	}
	return h.embedInputs(ctx, content.ID, []embeddingInput{{text: text}}, traceID, func(input embeddingInput, vector []float32, hash string) error {
		_, err := h.embeddings.UpsertContentEmbedding(ctx, repo.CreateContentEmbeddingParams{
			ContentID: content.ID, ModelID: h.modelID, InputHash: hash, Vector: vector, TraceID: traceID,
		})
		return err
	})
}

func (h *Handler) embedInputs(ctx context.Context, targetID uuid.UUID, inputs []embeddingInput, traceID string, persist func(embeddingInput, []float32, string) error) error {
	pending := make([]embeddingInput, 0, len(inputs))
	hashes := make(map[string]string, len(inputs))
	for _, input := range inputs {
		hash := hashInput(input.text)
		hashes[input.category] = hash
		var existing string
		var err error
		if input.category == repo.EmbeddingCategoryTitle || input.category == repo.EmbeddingCategoryBrief {
			existing, err = h.embeddings.GetCandidateEmbeddingInputHash(ctx, targetID, h.modelID, input.category)
		} else {
			existing, err = h.embeddings.GetContentEmbeddingInputHash(ctx, targetID, h.modelID)
		}
		if err == nil && existing == hash {
			continue
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("get existing embedding hash: %w", err)
		}
		pending = append(pending, input)
	}
	if len(pending) == 0 {
		return nil
	}
	texts := make([]string, len(pending))
	for i := range pending {
		texts[i] = pending[i].text
	}
	if h.metrics != nil {
		h.metrics.inputs.Add(ctx, int64(len(texts)))
		var bytes int64
		for _, text := range texts {
			bytes += int64(len(text))
		}
		h.metrics.inputBytes.Add(ctx, bytes)
	}
	response, err := h.embedder.Embed(ctx, &llm.EmbedRequest{Model: h.model, Input: texts, Dimensions: h.dimension, Meta: map[string]string{"trace_id": traceID}})
	if err != nil {
		return fmt.Errorf("embed %d inputs: %w", len(texts), err)
	}
	if response == nil || len(response.Vectors) != len(pending) {
		if h.metrics != nil {
			h.metrics.validationErrors.Add(ctx, 1)
		}
		return fmt.Errorf("%w: got %d vectors for %d inputs", ErrInvalidEmbedding, vectorCount(response), len(pending))
	}
	for i, vector := range response.Vectors {
		if len(vector) != h.dimension {
			if h.metrics != nil {
				h.metrics.validationErrors.Add(ctx, 1)
			}
			return fmt.Errorf("%w: vector %d has dimension %d, want %d", ErrInvalidEmbedding, i, len(vector), h.dimension)
		}
		if err := persist(pending[i], vector, hashes[pending[i].category]); err != nil {
			return fmt.Errorf("persist vector %d: %w", i, err)
		}
		if h.metrics != nil {
			h.metrics.vectors.Add(ctx, 1)
		}
	}
	return nil
}

func targetID(meta json.RawMessage, kind string) (uuid.UUID, error) {
	var values map[string]string
	if err := json.Unmarshal(meta, &values); err != nil {
		return uuid.Nil, fmt.Errorf("%w: decode %s metadata: %w", ErrInvalidTaskSignal, kind, err)
	}
	key := "candidate_id"
	if kind == repo.TaskKindEmbedContent {
		key = "content_id"
	}
	id, err := uuid.Parse(values[key])
	if err != nil || id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%w: %s is missing or invalid", ErrInvalidTaskSignal, key)
	}
	return id, nil
}

func canonicalDocumentText(content repo.Content) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Title: %s\n", strings.TrimSpace(content.Title))
	fmt.Fprintf(&b, "Type: %s\n", strings.TrimSpace(content.Type))
	fmt.Fprintf(&b, "Source: %s\n", strings.TrimSpace(content.SourceAbbr))
	if !content.PublishedAt.IsZero() {
		fmt.Fprintf(&b, "Published: %s\n", content.PublishedAt.UTC().Format(time.RFC3339))
	}
	if content.Author != nil && strings.TrimSpace(*content.Author) != "" {
		fmt.Fprintf(&b, "Author: %s\n", strings.TrimSpace(*content.Author))
	}
	b.WriteString("\n")
	b.WriteString(strings.TrimSpace(content.Content))
	return b.String()
}

func hashInput(input string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(input)))
}

func vectorCount(response *llm.EmbedResponse) int {
	if response == nil {
		return 0
	}
	return len(response.Vectors)
}
