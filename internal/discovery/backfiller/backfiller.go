package backfiller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ChiaYuChang/prism/internal/discovery"
	"github.com/ChiaYuChang/prism/internal/model"
	f "github.com/ChiaYuChang/prism/pkg/functional"
	"go.opentelemetry.io/otel/trace"
)

var (
	ErrZeroUntil      = errors.New("until is zero")
	ErrNotImplemented = errors.New("not yet implemented")
	ErrParamMissing   = errors.New("param missing")
)

type Pager interface {
	Next(ctx context.Context) (string, error)
}

type Sink interface {
	Handle(ctx context.Context, sourceURL string, candidates []model.Candidates) error
}

type SinkFunc func(ctx context.Context, sourceURL string, candidates []model.Candidates) error

func (f SinkFunc) Handle(ctx context.Context, sourceURL string, candidates []model.Candidates) error {
	return f(ctx, sourceURL, candidates)
}

type Backfiller struct {
	logger  *slog.Logger
	tracer  trace.Tracer
	scout   discovery.Scout
	pager   Pager
	sink    Sink
	timeout time.Duration
}

func New(logger *slog.Logger, tracer trace.Tracer, scout discovery.Scout, pager Pager, sink Sink, timeout time.Duration) (*Backfiller, error) {
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
	return &Backfiller{
		logger:  logger,
		tracer:  tracer,
		scout:   scout,
		pager:   pager,
		sink:    sink,
		timeout: timeout,
	}, nil
}

func (r *Backfiller) Run(ctx context.Context, req discovery.BackfillRequest) (discovery.BackfillResult, error) {
	ctx, span := r.tracer.Start(ctx, "discovery.backfiller.run")
	defer span.End()

	var result discovery.BackfillResult
	if req.Until.IsZero() {
		return result, ErrZeroUntil
	}

	r.logger.InfoContext(ctx, "backfill started",
		slog.Time("until", req.Until),
		slog.Int("max_pages", req.MaxPages),
	)

	oldest := time.Time{}
	for page := 1; ; page++ {
		if req.MaxPages > 0 && page > req.MaxPages {
			r.logger.InfoContext(ctx, "reached max pages limit", slog.Int("page", page))
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
			if err := r.sink.Handle(pageCtx, currentURL, filtered); err != nil {
				return result, fmt.Errorf("handle candidates from %s: %w", currentURL, err)
			}
			result.CandidatesProcessed += len(filtered)
		}

		r.logger.DebugContext(ctx, "processed backfill page",
			slog.Int("page", page),
			slog.String("url", currentURL),
			slog.Int("seen", len(candidates)),
			slog.Int("filtered", len(filtered)),
		)

		if oldest.Before(req.Until) {
			r.logger.InfoContext(ctx, "reached until time limit",
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
		slog.Int("pages_visited", result.PagesVisited),
		slog.Int("candidates_processed", result.CandidatesProcessed),
	)

	return result, nil
}
