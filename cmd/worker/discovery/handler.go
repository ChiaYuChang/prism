package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/ChiaYuChang/prism/internal/discovery"
	"github.com/ChiaYuChang/prism/internal/discovery/planner"
	discoverysink "github.com/ChiaYuChang/prism/internal/discovery/sink"
	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/model"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/internal/repo"
	wm "github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const (
	SpanNameHandleMessage = "worker.discovery.handle_message"
)

var (
	ErrParamMissing                             = errors.New("param missing")
	ErrInvalidTaskSignal                        = errors.New("invalid task signal")
	ErrUnsupportedTaskKind                      = errors.New("unsupported task kind")
	ErrUnsupportedSourceType                    = errors.New("unsupported source type")
	ErrUnsupportedTaskKindSourceTypeCombination = errors.New("unsupported task kind/source type combination")
	ErrSourceMismatch                           = errors.New("source mismatch")
)

type Handler struct {
	logger    *slog.Logger
	tracer    trace.Tracer
	scout     discovery.Scout
	providers map[string]discovery.SearchClient
	sink      discoverysink.CandidateSink
	scoutRepo repo.Scout
	reporter  repo.TaskReporter
	metrics   *metrics
}

type metrics struct {
	task   *taskMetrics
	search *searchMetrics
}

type taskMetrics struct {
	count    otelmetric.Int64Counter
	duration otelmetric.Float64Histogram
}

type searchMetrics struct {
	requests otelmetric.Int64Counter
	duration otelmetric.Float64Histogram
	results  otelmetric.Int64Counter
}

func newMetrics(meter otelmetric.Meter) (*metrics, error) {
	tasks, err := meter.Int64Counter(
		"prism.discovery.tasks",
		otelmetric.WithDescription("Count of discovery task outcomes."),
		otelmetric.WithUnit("{task}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create discovery task counter: %w", err)
	}
	taskDuration, err := meter.Float64Histogram(
		"prism.discovery.task.duration",
		otelmetric.WithDescription("Discovery task handling duration."),
		otelmetric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("create discovery task duration histogram: %w", err)
	}
	searchRequests, err := meter.Int64Counter(
		"prism.search.requests",
		otelmetric.WithDescription("Count of search provider request outcomes."),
		otelmetric.WithUnit("{request}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create search request counter: %w", err)
	}
	searchRequestDuration, err := meter.Float64Histogram(
		"prism.search.request.duration",
		otelmetric.WithDescription("Search provider request duration."),
		otelmetric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("create search request duration histogram: %w", err)
	}
	searchResults, err := meter.Int64Counter(
		"prism.search.results",
		otelmetric.WithDescription("Count of search provider results returned."),
		otelmetric.WithUnit("{result}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create search result counter: %w", err)
	}

	return &metrics{
		task: &taskMetrics{
			count:    tasks,
			duration: taskDuration,
		},
		search: &searchMetrics{
			requests: searchRequests,
			duration: searchRequestDuration,
			results:  searchResults,
		},
	}, nil
}

func (m *metrics) recordTask(ctx context.Context, sig message.TaskSignal, result string, started time.Time) {
	if m == nil {
		return
	}
	m.task.record(ctx, sig, result, started)
}

func (m *metrics) recordSearch(ctx context.Context, provider, config, result string, duration time.Duration, resultCount int) {
	if m == nil {
		return
	}
	m.search.record(ctx, provider, config, result, duration, resultCount)
}

func (m *taskMetrics) record(ctx context.Context, sig message.TaskSignal, result string, started time.Time) {
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

	attrs := otelmetric.WithAttributes(
		attribute.String("task.kind", kind),
		attribute.String("source.type", stype),
		attribute.String("result", result),
	)
	m.count.Add(ctx, 1, attrs)
	m.duration.Record(ctx, time.Since(started).Seconds(), attrs)
}

func (m *searchMetrics) record(ctx context.Context, provider, config, result string, duration time.Duration, resultCount int) {
	if m == nil {
		return
	}
	attrs := otelmetric.WithAttributes(
		attribute.String("provider", provider),
		attribute.String("config", config),
		attribute.String("result", result),
	)
	m.requests.Add(ctx, 1, attrs)
	m.duration.Record(ctx, duration.Seconds(), attrs)
	if result == "ok" && resultCount > 0 {
		m.results.Add(ctx, int64(resultCount), otelmetric.WithAttributes(
			attribute.String("provider", provider),
			attribute.String("config", config),
		))
	}
}

func NewHandler(
	logger *slog.Logger,
	tracer trace.Tracer,
	scout discovery.Scout,
	searchProviders map[string]discovery.SearchClient,
	sink discoverysink.CandidateSink,
	scoutRepo repo.Scout,
	reporter repo.TaskReporter,
	metrics *metrics,
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
	if reporter == nil {
		return nil, fmt.Errorf("%w: task_reporter", ErrParamMissing)
	}
	if searchProviders == nil {
		searchProviders = map[string]discovery.SearchClient{}
	}
	return &Handler{
		logger:    logger,
		tracer:    tracer,
		scout:     scout,
		providers: searchProviders,
		sink:      sink,
		scoutRepo: scoutRepo,
		reporter:  reporter,
		metrics:   metrics,
	}, nil
}

// HandleMessage handles incoming task signals for discovery tasks.
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
	if !ownsTask(sig) {
		h.metrics.recordTask(ctx, sig, "ignored", started)
		h.logger.WarnContext(ctx,
			fmt.Sprintf("ignoring task %s: kind=%s source_type=%s",
				sig.TaskID, sig.Kind, sig.SourceType))
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
		slog.String("kind", sig.Kind),
		slog.String("source_type", sig.SourceType),
		slog.String("source_abbr", sig.SourceAbbr),
		slog.String("url", sig.URL),
	)

	if err := h.process(ctx, sig); err != nil {
		logger.ErrorContext(ctx, "discovery task failed", "error", err)
		if failErr := h.reporter.FailTask(ctx, sig.TaskID); failErr != nil {
			h.metrics.recordTask(ctx, sig, "nacked", started)
			return false, fmt.Errorf(
				"process task %s: %w; mark failed: %w",
				sig.TaskID, err, failErr)
		}
		h.metrics.recordTask(ctx, sig, "failed", started)
		return true, err
	}

	if err := h.reporter.CompleteTask(ctx, sig.TaskID); err != nil {
		h.metrics.recordTask(ctx, sig, "nacked", started)
		return false, fmt.Errorf("complete task %s: %w", sig.TaskID, err)
	}

	h.metrics.recordTask(ctx, sig, "ok", started)
	logger.InfoContext(ctx, "discovery task completed")
	return true, nil
}

func (h *Handler) process(ctx context.Context, sig message.TaskSignal) error {
	switch {
	case sig.Kind == repo.TaskKindDirectoryFetch &&
		(sig.SourceType == repo.SourceTypeParty || sig.SourceType == repo.SourceTypeMedia):
		return h.handleDirectoryFetch(ctx, sig)
	case sig.Kind == repo.TaskKindKeywordSearch && sig.SourceType == repo.SourceTypeMedia:
		return h.handleKeywordSearch(ctx, sig)
	default:
		return fmt.Errorf("%w: kind=%s source_type=%s",
			ErrUnsupportedTaskKindSourceTypeCombination, sig.Kind, sig.SourceType)
	}
}

func (h *Handler) handleDirectoryFetch(ctx context.Context, sig message.TaskSignal) error {
	source, err := h.scoutRepo.GetSourceByAbbr(ctx, sig.SourceAbbr)
	if err != nil {
		return fmt.Errorf("get source by abbr %s: %w", sig.SourceAbbr, err)
	}
	if source.Type != sig.SourceType {
		return fmt.Errorf("%w: db source type %s != signal source type %s",
			ErrSourceMismatch, source.Type, sig.SourceType)
	}
	if err := validateTaskURL(source.BaseURL, sig.URL); err != nil {
		return err
	}

	candidates, err := h.scout.Discover(ctx, sig.URL)
	if err != nil {
		return fmt.Errorf("discover candidates from %s: %w", sig.URL, err)
	}

	if err := h.sink.Handle(ctx, discoverysink.CandidateSinkRequest{
		SourceURL:       sig.URL,
		SourceAbbr:      sig.SourceAbbr,
		SourceType:      sig.SourceType,
		BatchID:         sig.BatchID,
		TraceID:         sig.TraceID,
		IngestionMethod: repo.IngestionMethodDirectory,
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

func (h *Handler) handleKeywordSearch(ctx context.Context, sig message.TaskSignal) error {
	if len(h.providers) == 0 {
		return fmt.Errorf("%w: no search providers enabled", ErrUnsupportedSourceType)
	}

	var payload planner.MediaTaskPayload
	if err := json.Unmarshal(sig.Payload, &payload); err != nil {
		return fmt.Errorf("decode keyword search payload: %w", err)
	}
	if strings.TrimSpace(payload.Query) == "" {
		return fmt.Errorf("%w: empty query in payload", ErrInvalidTaskSignal)
	}

	var (
		candidates []model.Candidates
		failures   []error
	)
	for provider, client := range h.providers {
		started := time.Now()
		found, err := client.DiscoverNews(ctx, payload.Query, payload.Site)
		duration := time.Since(started)
		providerLabel, configLabel := normalizeSearchProvider(provider)
		if err != nil {
			h.metrics.recordSearch(ctx, providerLabel, configLabel, "failed", duration, 0)
			h.logger.WarnContext(ctx, "search provider failed",
				slog.String("provider", provider),
				slog.String("source_abbr", sig.SourceAbbr),
				slog.String("query", payload.Query),
				slog.Any("error", err),
			)
			failures = append(failures, fmt.Errorf("%s: %w", provider, err))
			continue
		}
		h.metrics.recordSearch(ctx, providerLabel, configLabel, "ok", duration, len(found))
		for i := range found {
			if found[i].Metadata == nil {
				found[i].Metadata = map[string]any{}
			}
			found[i].Metadata["search_provider"] = provider
			found[i].Metadata["query"] = payload.Query
			if payload.Site != "" {
				found[i].Metadata["site_filter"] = payload.Site
			}
		}
		candidates = append(candidates, found...)
	}
	if len(failures) == len(h.providers) {
		return fmt.Errorf("search %q via enabled providers: %w", payload.Query, errors.Join(failures...))
	}

	if err := h.sink.Handle(ctx, discoverysink.CandidateSinkRequest{
		SourceURL:       sig.URL,
		SourceAbbr:      sig.SourceAbbr,
		SourceType:      sig.SourceType,
		BatchID:         sig.BatchID,
		TraceID:         sig.TraceID,
		IngestionMethod: repo.IngestionMethodSearch,
		DefaultMetadata: map[string]any{
			"task_id":     sig.TaskID.String(),
			"task_kind":   sig.Kind,
			"source_type": sig.SourceType,
			"query":       payload.Query,
			"site":        payload.Site,
		},
		Candidates: candidates,
	}); err != nil {
		return fmt.Errorf("sink search candidates for %q: %w", payload.Query, err)
	}

	return nil
}

// normalizeSearchProvider maps raw provider map keys to low-cardinality metric labels.
// The config label is deployment-configured and should remain low-cardinality per deployment.
func normalizeSearchProvider(key string) (provider string, config string) {
	key = strings.TrimSpace(key)
	switch {
	case key == "brave":
		return "search.brave", "default"
	case key == "google-cse":
		return "search.google_cse", "default"
	case strings.HasPrefix(key, "serpapi-google-news-"):
		return "search.serpapi.google_news", searchProviderConfig(key, "serpapi-google-news-")
	case strings.HasPrefix(key, "serpapi-bing-news-"):
		return "search.serpapi.bing_news", searchProviderConfig(key, "serpapi-bing-news-")
	case strings.HasPrefix(key, "serpapi-duckduckgo-news-"):
		return "search.serpapi.duckduckgo_news", searchProviderConfig(key, "serpapi-duckduckgo-news-")
	default:
		return "search.unknown", "unknown"
	}
}

func searchProviderConfig(key, prefix string) string {
	config := strings.TrimSpace(strings.TrimPrefix(key, prefix))
	if config == "" {
		return "unknown"
	}
	return config
}

// ownsTask returns true if the (task kind, source type) combination is owned by this worker, false otherwise.
// valid combinations are:
//
// - TaskKindDirectoryFetch:
//   - SourceTypeParty
//   - SourceTypeMedia
//
// - TaskKindKeywordSearch:
//   - SourceTypeMedia
func ownsTask(sig message.TaskSignal) bool {
	if sig.Kind == repo.TaskKindDirectoryFetch {
		return sig.SourceType == repo.SourceTypeParty || sig.SourceType == repo.SourceTypeMedia
	}
	return sig.Kind == repo.TaskKindKeywordSearch && sig.SourceType == repo.SourceTypeMedia
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
