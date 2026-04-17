package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	collector "github.com/ChiaYuChang/prism/internal/collector"
	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/ChiaYuChang/prism/pkg/archivecodec"
	wm "github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
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
	fetcher          collector.Fetcher
	errorSaver       collector.Saver // optional: nil = raw content lost on Minify failure
	minifier         collector.Minifier
	transformer      collector.Transformer
	parser           collector.Parser
	archivePublisher ArchivePublisher // optional: nil = skip archive
	pipeline         repo.Pipeline
	scheduler        repo.Scheduler
}

func NewHandler(
	logger *slog.Logger,
	tracer trace.Tracer,
	fetcher collector.Fetcher,
	errorSaver collector.Saver,
	minifier collector.Minifier,
	transformer collector.Transformer,
	parser collector.Parser,
	archivePublisher ArchivePublisher,
	pipeline repo.Pipeline,
	scheduler repo.Scheduler,
) (*Handler, error) {
	if logger == nil {
		return nil, fmt.Errorf("%w: logger", ErrParamMissing)
	}
	if tracer == nil {
		return nil, fmt.Errorf("%w: tracer", ErrParamMissing)
	}
	if fetcher == nil {
		return nil, fmt.Errorf("%w: fetcher", ErrParamMissing)
	}
	if minifier == nil {
		return nil, fmt.Errorf("%w: minifier", ErrParamMissing)
	}
	if transformer == nil {
		return nil, fmt.Errorf("%w: transformer", ErrParamMissing)
	}
	if parser == nil {
		return nil, fmt.Errorf("%w: parser", ErrParamMissing)
	}
	if pipeline == nil {
		return nil, fmt.Errorf("%w: pipeline", ErrParamMissing)
	}
	if scheduler == nil {
		return nil, fmt.Errorf("%w: scheduler", ErrParamMissing)
	}
	return &Handler{
		logger:           logger,
		tracer:           tracer,
		fetcher:          fetcher,
		errorSaver:       errorSaver, // nil = raw content not archived on Minify failure
		minifier:         minifier,
		transformer:      transformer,
		parser:           parser,
		archivePublisher: archivePublisher,
		pipeline:         pipeline,
		scheduler:        scheduler,
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
	if sig.Kind != repo.TaskKindPageFetch {
		return true, nil
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
		if failErr := h.scheduler.FailTask(ctx, sig.TaskID); failErr != nil {
			return false, fmt.Errorf("process task %s: %w; mark failed: %w", sig.TaskID, err, failErr)
		}
		return true, err
	}

	if err := h.scheduler.CompleteTask(ctx, sig.TaskID); err != nil {
		return false, fmt.Errorf("complete task %s: %w", sig.TaskID, err)
	}

	logger.InfoContext(ctx, "collector task completed")
	return true, nil
}

func (h *Handler) process(ctx context.Context, logger *slog.Logger, sig message.TaskSignal) error {
	candidateID := extractCandidateID(sig.Meta)

	if candidateID != uuid.Nil {
		if _, err := h.pipeline.GetContentByCandidateID(ctx, candidateID); err == nil {
			logger.InfoContext(ctx, "content already exists by candidate ID, skipping", "candidate_id", candidateID.String(), "url", sig.URL)
			return nil
		}
	}

	// Skip if content already exists for this URL.
	if _, err := h.pipeline.GetContentByURL(ctx, sig.URL); err == nil {
		logger.InfoContext(ctx, "content already exists by url, skipping", "url", sig.URL)
		return nil
	}

	// F: Fetch raw content from the live URL.
	raw, err := h.fetcher.Fetch(ctx, sig.URL)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", sig.URL, err)
	}

	// M: Minify raw HTML.
	// On failure, archive raw content for later replay via the Recoverer.
	minified, err := h.minifier.Minify(ctx, raw)
	if err != nil {
		h.saveOnMinifyError(ctx, sig, raw, err)
		return fmt.Errorf("minify %s: %w", sig.URL, err)
	}

	// T: Transform minified HTML (Stage 2, currently no-op).
	canonical, err := h.transformer.Transform(ctx, minified)
	if err != nil {
		return fmt.Errorf("transform %s: %w", sig.URL, err)
	}

	// P: Parse canonical HTML into structured content.
	art, err := h.parser.Parse(ctx, sig.URL, canonical)
	if err != nil {
		return fmt.Errorf("parse %s: %w", sig.URL, err)
	}

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

// saveOnMinifyError archives raw content when Minify fails so it can be
// replayed later via LocalRecoverer. Non-fatal: logs a warning on failure.
// The recover_key stored in meta is the task's TraceID; the caller uses it
// to locate the archive file via LocalRecoverer.Fetch(ctx, traceID).
func (h *Handler) saveOnMinifyError(ctx context.Context, sig message.TaskSignal, raw string, minifyErr error) {
	if h.errorSaver == nil {
		return
	}
	archive := collector.Archive{
		URL:       sig.URL,
		Payload:   raw,
		TraceID:   sig.TraceID,
		Timestamp: time.Now(),
		Metadata: map[string]any{
			"stage":        "raw",
			"error":        minifyErr.Error(),
			"recover_from": "minify",
			"recover_key":  sig.TraceID,
			"source_abbr":  sig.SourceAbbr,
			"source_type":  sig.SourceType,
			"batch_id":     sig.BatchID.String(),
		},
	}
	if err := h.errorSaver.Save(ctx, archive); err != nil {
		h.logger.WarnContext(ctx, "failed to archive raw content on minify error (content may be lost)",
			slog.String("url", sig.URL),
			slog.String("trace_id", sig.TraceID),
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
