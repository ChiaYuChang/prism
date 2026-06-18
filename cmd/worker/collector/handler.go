package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	collector "github.com/ChiaYuChang/prism/internal/collector"
	"github.com/ChiaYuChang/prism/internal/collector/archiver"
	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/ChiaYuChang/prism/pkg/archivecodec"
	wm "github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const (
	SpanNameHandleMessage = "worker.collector.handle_message"

	ContentTypePartyRelease = "PARTY_RELEASE"
	ContentTypeArticle      = "ARTICLE"
)

var (
	ErrParamMissing      = errors.New("param missing")
	ErrInvalidTaskSignal = errors.New("invalid task signal")
)

// ArchivePublisher publishes ArchiveSignal messages to the archive topic.
// Implemented by any Watermill publisher.
type ArchivePublisher interface {
	Publish(topic string, messages ...*wm.Message) error
}

type Handler struct {
	logger           *slog.Logger
	tracer           trace.Tracer
	dispatcher       *collector.Dispatcher
	errorSaver       collector.Saver  // optional: nil = raw content lost on Minify failure
	archivePublisher ArchivePublisher // optional: nil = skip archive
	pipeline         repo.Pipeline
	reporter         repo.TaskReporter
	metrics          *metrics
}

type metrics struct {
	tasks        metric.Int64Counter
	taskDuration metric.Float64Histogram
}

func newMetrics(meter metric.Meter) (*metrics, error) {
	tasks, err := meter.Int64Counter(
		"prism.collector.tasks",
		metric.WithDescription("Count of collector task outcomes."),
		metric.WithUnit("{task}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create collector task counter: %w", err)
	}
	taskDuration, err := meter.Float64Histogram(
		"prism.collector.task.duration",
		metric.WithDescription("Collector task handling duration."),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("create collector task duration histogram: %w", err)
	}
	return &metrics{tasks: tasks, taskDuration: taskDuration}, nil
}

func (m *metrics) recordTask(ctx context.Context, sig message.TaskSignal, result string, started time.Time) {
	if m == nil {
		return
	}

	kind := strings.TrimSpace(sig.Kind)
	if kind == "" {
		kind = "unknown"
	}
	stype := strings.TrimSpace(sig.SourceType)
	if stype == "" {
		stype = "unknown"
	}

	attrs := metric.WithAttributes(
		attribute.String("task.kind", kind),
		attribute.String("source.type", stype),
		attribute.String("result", result),
	)
	m.tasks.Add(ctx, 1, attrs)
	m.taskDuration.Record(ctx, time.Since(started).Seconds(), attrs)
}

func NewHandler(
	logger *slog.Logger,
	tracer trace.Tracer,
	dispatcher *collector.Dispatcher,
	errorSaver collector.Saver,
	archivePublisher ArchivePublisher,
	pipeline repo.Pipeline,
	reporter repo.TaskReporter,
	metrics *metrics,
) (*Handler, error) {
	if logger == nil {
		return nil, fmt.Errorf("%w: logger", ErrParamMissing)
	}
	if tracer == nil {
		return nil, fmt.Errorf("%w: tracer", ErrParamMissing)
	}
	if dispatcher == nil {
		return nil, fmt.Errorf("%w: dispatcher", ErrParamMissing)
	}
	if pipeline == nil {
		return nil, fmt.Errorf("%w: pipeline", ErrParamMissing)
	}
	if reporter == nil {
		return nil, fmt.Errorf("%w: reporter", ErrParamMissing)
	}
	return &Handler{
		logger:           logger,
		tracer:           tracer,
		dispatcher:       dispatcher,
		errorSaver:       errorSaver,
		archivePublisher: archivePublisher,
		pipeline:         pipeline,
		reporter:         reporter,
		metrics:          metrics,
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
	if sig.Kind != repo.TaskKindPageFetch {
		h.metrics.recordTask(ctx, sig, "ignored", started)
		return true, nil
	}

	ctx, traceErr := message.ExtractTraceContext(ctx, msg)
	if traceErr != nil {
		h.metrics.recordTask(ctx, sig, "invalid", started)
		return true, fmt.Errorf("extract trace context: %w", traceErr)
	}
	ctx = obs.WithTraceID(ctx, sig.TraceID)
	ctx, span := h.tracer.Start(ctx, SpanNameHandleMessage)
	defer span.End()

	logger := h.logger.With(
		slog.String("task_id", sig.TaskID.String()),
		slog.String("trace_id", sig.TraceID),
		slog.String("source_type", sig.SourceType),
		slog.String("source_abbr", sig.SourceAbbr),
		slog.String("url", sig.URL),
	)

	if err := h.process(ctx, logger, sig); err != nil {
		logger.ErrorContext(ctx, "collector task failed", "error", err)
		if failErr := h.reporter.FailTask(ctx, sig.TaskID); failErr != nil {
			h.metrics.recordTask(ctx, sig, "nacked", started)
			return false, fmt.Errorf("process task %s: %w; mark failed: %w", sig.TaskID, err, failErr)
		}

		h.metrics.recordTask(ctx, sig, collectorResult(err), started)
		return true, err
	}

	if err := h.reporter.CompleteTask(ctx, sig.TaskID); err != nil {
		h.metrics.recordTask(ctx, sig, "nacked", started)
		return false, fmt.Errorf("complete task %s: %w", sig.TaskID, err)
	}

	h.metrics.recordTask(ctx, sig, "ok", started)
	logger.InfoContext(ctx, "collector task completed")
	return true, nil
}

func collectorResult(err error) string {
	for _, stage := range []collector.PipelineStage{
		collector.PipelineStageFetch,
		collector.PipelineStageMinify,
		collector.PipelineStageTransform,
		collector.PipelineStageParse,
	} {
		if errors.Is(err, &collector.StageError{Stage: stage}) {
			return stage.String() + "_failed"
		}
	}
	return "failed"
}

func (h *Handler) process(ctx context.Context, logger *slog.Logger, sig message.TaskSignal) error {
	candidateID := extractCandidateID(sig.Meta)

	// Skip if content already exists for this candidate ID.
	if candidateID != uuid.Nil {
		if _, err := h.pipeline.GetContentByCandidateID(ctx, candidateID); err == nil {
			logger.InfoContext(
				ctx,
				"content already exists by candidate ID, skipping",
				"candidate_id", candidateID.String(),
				"url", sig.URL,
			)
			return nil
		}
	}

	// Skip if content already exists for this URL.
	if _, err := h.pipeline.GetContentByURL(ctx, sig.URL); err == nil {
		logger.InfoContext(
			ctx,
			"content already exists by url, skipping",
			"url", sig.URL,
		)
		return nil
	}

	// Dispatch the URL to the collector.
	result, err := h.dispatcher.Dispatch(ctx, sig.SourceAbbr, sig.URL)
	if err != nil {
		if stageErr, ok := errors.AsType[*collector.StageError](err); ok {
			switch stageErr.Stage {
			case collector.PipelineStageMinify:
				h.saveErrorArchive(ctx, sig, stageErr.Intermediate, stageErr.Err,
					archiver.PayloadKindRaw, collector.PipelineStageMinify)
			case collector.PipelineStageTransform:
				h.saveErrorArchive(ctx, sig, stageErr.Intermediate, stageErr.Err,
					archiver.PayloadKindMinified, collector.PipelineStageTransform)
			case collector.PipelineStageParse:
				h.saveErrorArchive(ctx, sig, stageErr.Intermediate, stageErr.Err,
					archiver.PayloadKindCanonical, collector.PipelineStageParse)
			}
		}
		return fmt.Errorf("dispatch %s: %w", sig.URL, err)
	}
	art := result.Article
	canonical := result.Canonical

	contentType := sourceTypeToContentType(sig.SourceType)
	fetchedAt := time.Now()

	publishedAt := art.PublishedAt
	metadata := map[string]any{}
	if publishedAt.IsZero() {
		publishedAt = fetchedAt
		metadata["published_at_estimated"] = true
	}
	metaBytes, _ := json.Marshal(metadata)

	params := repo.CreateContentParams{
		BatchID:     sig.BatchID,
		Type:        contentType,
		SourceAbbr:  sig.SourceAbbr,
		CandidateID: candidateID,
		URL:         sig.URL,
		Title:       art.Title,
		Content:     art.Content,
		TraceID:     sig.TraceID,
		PublishedAt: publishedAt,
		FetchedAt:   fetchedAt,
		Metadata:    metaBytes,
	}
	if art.Author != "" {
		params.Author = &art.Author
	}

	content, err := h.pipeline.CreateContent(ctx, params)
	if err != nil {
		return fmt.Errorf("create content for %s: %w", sig.URL, err)
	}

	logger.InfoContext(ctx, "content persisted",
		slog.String("url", sig.URL),
		slog.String("content_type", contentType),
		slog.String("candidate_id", candidateID.String()),
		slog.String("content_id", content.ID.String()),
	)

	// S: Publish archive signal after content is persisted.
	// Fire-and-forget: failure is logged but does not affect task completion.
	if h.archivePublisher != nil {
		if err := h.publishArchive(ctx, logger, content.ID, sig, canonical, fetchedAt); err != nil {
			logger.WarnContext(ctx, "failed to publish archive signal (non-fatal)", "error", err)
		}
	}

	return nil
}

func (h *Handler) publishArchive(ctx context.Context, logger *slog.Logger, contentID uuid.UUID, sig message.TaskSignal, canonical string, fetchedAt time.Time) error {
	page, err := archivecodec.GzipBase64.PackString(canonical)
	if err != nil {
		return fmt.Errorf("compress canonical html: %w", err)
	}

	archiveSig := message.ArchiveSignal{
		ContentID: contentID,
		URL:       sig.URL,
		TraceID:   sig.TraceID,
		FetchedAt: fetchedAt,
		Page:      *page,
	}

	payload, err := json.Marshal(archiveSig)
	if err != nil {
		return fmt.Errorf("marshal archive signal: %w", err)
	}

	msgID := uuid.Must(uuid.NewV7()).String()
	wmMsg := wm.NewMessage(msgID, payload)
	wmMsg.Metadata.Set("trace_id", sig.TraceID)
	if err := message.InjectTraceContext(ctx, wmMsg); err != nil {
		return fmt.Errorf("inject archive trace context: %w", err)
	}

	if err := h.archivePublisher.Publish(message.ArchiveTopic, wmMsg); err != nil {
		return fmt.Errorf("publish to %s: %w", message.ArchiveTopic, err)
	}

	logger.DebugContext(ctx, "archive signal published",
		slog.String("content_id", contentID.String()),
		slog.Int("original_size", page.OriginalSize),
	)
	return nil
}

func sourceTypeToContentType(sourceType string) string {
	switch sourceType {
	case repo.SourceTypeParty:
		return ContentTypePartyRelease
	default:
		return ContentTypeArticle
	}
}

// saveErrorArchive archives intermediate content when a pipeline stage fails
// so it can be replayed later via LocalRecoverer. Non-fatal: logs a warning on failure.
func (h *Handler) saveErrorArchive(ctx context.Context, sig message.TaskSignal, payload string, err error, kind archiver.PayloadKind, stage collector.PipelineStage) {
	if h.errorSaver == nil {
		return
	}
	archive := collector.Archive{
		URL:       sig.URL,
		Payload:   payload,
		TraceID:   sig.TraceID,
		Timestamp: time.Now(),
		Metadata: map[string]any{
			"kind":         string(kind),
			"error":        err.Error(),
			"recover_from": stage,
			"recover_key":  sig.TraceID,
			"source_abbr":  sig.SourceAbbr,
			"source_type":  sig.SourceType,
			"batch_id":     sig.BatchID.String(),
		},
	}
	if err := h.errorSaver.Save(ctx, archive); err != nil {
		h.logger.WarnContext(ctx, "failed to archive content on pipeline error (content may be lost)",
			slog.String("url", sig.URL),
			slog.String("trace_id", sig.TraceID),
			slog.String("stage", string(stage)),
			slog.String("kind", string(kind)),
			slog.Any("error", err),
		)
	}
}

func extractCandidateID(meta json.RawMessage) uuid.UUID {
	if len(meta) == 0 {
		return uuid.Nil
	}
	var m map[string]string
	if err := json.Unmarshal(meta, &m); err != nil {
		return uuid.Nil
	}
	if raw, ok := m["candidate_id"]; ok {
		if id, err := uuid.Parse(raw); err == nil {
			return id
		}
	}
	return uuid.Nil
}
