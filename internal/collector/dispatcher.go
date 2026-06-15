package collector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

var ErrParamMissing = errors.New("param missing")

// Dispatcher orchestrates the F-M-T[]-P pipeline for a single URL, routing to
// a per-source pipeline.Pipeline retrieved from the Registry.
//
// Pipeline stages:
//
//	F (Fetcher)     — retrieve raw content
//	M (Minifier)    — strip noise, reduce size; archive point for S
//	T (Transformers)— post-archive transforms (may be empty)
//	P (Parser)      — extract structured Article
//
// On any stage failure Dispatcher returns a *StageError carrying the
// intermediate value (= the failing stage's input). The caller decides
// whether to archive it, retry, or drop.
type Dispatcher struct {
	logger   *slog.Logger
	tracer   trace.Tracer
	registry *PipelineRegistry
}

func NewDispatcher(
	logger *slog.Logger,
	tracer trace.Tracer,
	registry *PipelineRegistry,
) (*Dispatcher, error) {
	if logger == nil {
		return nil, fmt.Errorf("%w: logger", ErrParamMissing)
	}
	if tracer == nil {
		return nil, fmt.Errorf("%w: tracer", ErrParamMissing)
	}
	if registry == nil {
		return nil, fmt.Errorf("%w: registry", ErrParamMissing)
	}
	return &Dispatcher{
		logger:   logger,
		tracer:   tracer,
		registry: registry,
	}, nil
}

// DispatchResult carries the parsed Article and the canonical content
// (input to P). Canonical is the success-path archive point: publish it
// to the Saver (S leg) after a successful dispatch so the content can
// be replayed from this stage if a downstream fault is discovered later.
type DispatchResult struct {
	Article   *Article
	Canonical string
}

// Dispatch runs F-M-T[]-P for a single URL using the Pipeline registered
// for sourceID (fallback used when none is registered). Stage failures
// return *StageError with the intermediate value attached.
func (d *Dispatcher) Dispatch(ctx context.Context, sourceID, url string) (*DispatchResult, error) {
	ctx, span := d.tracer.Start(ctx, "collector.dispatcher.dispatch")
	defer span.End()

	p := d.registry.For(sourceID)

	raw, err := p.Fetcher.Fetch(ctx, url)
	if err != nil {
		return nil, &StageError{
			Stage:    PipelineStageFetch,
			SubStage: stageName(p.Fetcher),
			Err:      err,
		}
	}

	if strings.TrimSpace(raw) == "" {
		return nil, &StageError{
			Stage:    PipelineStageFetch,
			SubStage: stageName(p.Fetcher),
			Err:      fmt.Errorf("%w: fetched output is empty", ErrInvalidArticle),
		}
	}

	minified, err := p.Minifier.Transform(ctx, raw)
	if err != nil {
		return nil, &StageError{
			Stage:        PipelineStageMinify,
			SubStage:     stageName(p.Minifier),
			Err:          err,
			Intermediate: raw,
		}
	}
	if strings.TrimSpace(minified) == "" {
		return nil, &StageError{
			Stage:        PipelineStageMinify,
			SubStage:     stageName(p.Minifier),
			Err:          fmt.Errorf("%w: minified output is empty", ErrInvalidArticle),
			Intermediate: raw,
		}
	}

	canonical := minified
	for i, t := range p.Transformers {
		out, err := t.Transform(ctx, canonical)
		if err != nil {
			return nil, &StageError{
				Stage:        PipelineStageTransform,
				SubStage:     fmt.Sprintf("[%d] %s", i, stageName(t)),
				Err:          err,
				Intermediate: canonical,
			}
		}
		canonical = out
	}

	article, err := p.Parser.Parse(ctx, url, canonical)
	if err != nil {
		return nil, &StageError{
			Stage:    PipelineStageParse,
			SubStage: stageName(p.Parser),
			Err:      err, Intermediate: canonical,
		}
	}

	article = NormalizeArticle(article)
	if err := ValidateArticle(article); err != nil {
		return nil, &StageError{
			Stage:        PipelineStageParse,
			SubStage:     stageName(p.Parser),
			Err:          err,
			Intermediate: canonical,
		}
	}

	d.logger.DebugContext(
		ctx, "dispatch complete",
		slog.String("url", url),
		slog.String("source_id", sourceID),
		slog.String("title", article.Title),
	)

	return &DispatchResult{
		Article:   article,
		Canonical: canonical,
	}, nil
}
