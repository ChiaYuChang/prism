package backfiller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ChiaYuChang/prism/internal/discovery"
	discoverysink "github.com/ChiaYuChang/prism/internal/discovery/sink"
	"github.com/ChiaYuChang/prism/internal/model"
	"github.com/ChiaYuChang/prism/internal/obs"
	f "github.com/ChiaYuChang/prism/pkg/functional"
	"go.opentelemetry.io/otel/trace"
)

const (
	SpanNameBackfillerRun = "discovery.backfiller.run"
)

var (
	ErrZeroUntil      = errors.New("until is zero")
	ErrNotImplemented = errors.New("not yet implemented")
	ErrParamMissing   = errors.New("param missing")
)

// A Pager is an interface that provides a way to get the next page of a
// discovery source.
type Pager interface {
	Next(ctx context.Context) (string, error)
}

// Backfiller orchestrates historical data ingestion by replaying older listing pages.
// It iterates through past directory pages via a Pager, executes discovery using a Scout,
// filters the discovered briefs against a lower-bound date, and pushes them into
// a CandidateSink for persistence into the 'candidates' table.
// This fulfills the discovery strategy's goal to keep the pipeline recall-oriented and
// ensure candidates are separate from full contents until implicitly promoted.
type Backfiller struct {
	logger   *slog.Logger
	tracer   trace.Tracer
	scout    discovery.Scout
	pager    Pager
	sink     discoverysink.CandidateSink
	sourceID int32
	timeout  time.Duration
}

// New creates a new Backfiller instance, binding it to a specific Scout, Pager, and
// CandidateSink. It requires an explicit sourceID which maps to the 'sources.id' in
// the database.
func New(logger *slog.Logger, tracer trace.Tracer,
	scout discovery.Scout, pager Pager, sink discoverysink.CandidateSink,
	sourceID int32, timeout time.Duration) (*Backfiller, error) {
	if logger == nil {
		return nil, fmt.Errorf("%w: logger", ErrParamMissing)
	}
	if tracer == nil {
		return nil, fmt.Errorf("%w: tracer", ErrParamMissing)
	}
	if scout == nil {
		return nil, fmt.Errorf("%w: scout", ErrParamMissing)
	}
	if pager == nil {
		return nil, fmt.Errorf("%w: pager", ErrParamMissing)
	}
	if sink == nil {
		return nil, fmt.Errorf("%w: sink", ErrParamMissing)
	}
	if sourceID == 0 {
		return nil, fmt.Errorf("%w: source_id", ErrParamMissing)
	}
	return &Backfiller{
		logger:   logger,
		tracer:   tracer,
		scout:    scout,
		pager:    pager,
		sink:     sink,
		sourceID: sourceID,
		timeout:  timeout,
	}, nil
}

// Run executes the synchronous backfill process according to req parameters.
// It pages through using the provided Pager, invokes the Scout, and passes retrieved
// candidate briefs to the CandidateSink. The BatchID assigned to req groups the ingestion.
func (r *Backfiller) Run(ctx context.Context, req discovery.BackfillRequest) (discovery.BackfillResult, error) {
	ctx, span := r.tracer.Start(ctx, SpanNameBackfillerRun)
	defer span.End()
	traceID := obs.ExtractTraceID(ctx)

	var result discovery.BackfillResult
	if req.Until.IsZero() {
		return result, ErrZeroUntil
	}

	r.logger.InfoContext(ctx, "backfill started",
		slog.String("trace_id", traceID),
		slog.Int64("source_id", int64(r.sourceID)),
		slog.Time("until", req.Until),
		slog.Int("max_pages", req.MaxPages),
	)

	oldest := time.Time{}
	for page := 1; ; page++ {
		if req.MaxPages > 0 && page > req.MaxPages {
			r.logger.InfoContext(ctx,
				"reached max pages limit",
				slog.String("trace_id", traceID),
				slog.Int("page", page))
			break
		}

		pageCtx := ctx
		if r.timeout > 0 {
			var cancel context.CancelFunc
			pageCtx, cancel = context.WithTimeout(ctx, r.timeout)
			defer cancel()
		}

		currentURL, err := r.pager.Next(pageCtx)
		if err != nil {
			return result, fmt.Errorf("resolve page %d url: %w", page, err)
		}
		if currentURL == "" {
			break
		}

		candidates, err := r.scout.Discover(pageCtx, currentURL)
		if err != nil {
			return result, fmt.Errorf("discover page %d (%s): %w", page, currentURL, err)
		}

		result.PagesVisited++
		result.CandidatesSeen += len(candidates)

		filtered := f.Filter(candidates, func(c model.Candidates) bool {
			if oldest.IsZero() || oldest.After(c.PublishedAt) {
				oldest = c.PublishedAt
			}
			return !c.PublishedAt.Before(req.Until)
		})

		if len(filtered) > 0 {
			result.OldestPublishedAt = oldest
			if err := r.sink.Handle(pageCtx, discoverysink.CandidateSinkRequest{
				SourceURL:       currentURL,
				SourceID:        r.sourceID,
				SourceType:      "PARTY",
				BatchID:         req.BatchID,
				TraceID:         traceID,
				IngestionMethod: "DIRECTORY",
				Candidates:      filtered,
			}); err != nil {
				return result, fmt.Errorf("handle candidates from %s: %w", currentURL, err)
			}
			result.CandidatesProcessed += len(filtered)
		}

		r.logger.DebugContext(ctx, "processed backfill page",
			slog.String("trace_id", traceID),
			slog.Int("page", page),
			slog.String("url", currentURL),
			slog.Int("seen", len(candidates)),
			slog.Int("filtered", len(filtered)),
		)

		if oldest.Before(req.Until) {
			r.logger.InfoContext(ctx, "reached until time limit",
				slog.String("trace_id", traceID),
				slog.Time("oldest", oldest),
				slog.Time("until", req.Until),
			)
			break
		}
		if len(candidates) == 0 {
			break
		}
	}

	r.logger.InfoContext(ctx, "backfill completed",
		slog.String("trace_id", traceID),
		slog.Int("pages_visited", result.PagesVisited),
		slog.Int("candidates_processed", result.CandidatesProcessed),
	)

	return result, nil
}
