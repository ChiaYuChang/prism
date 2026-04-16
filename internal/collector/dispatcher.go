package collector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ChiaYuChang/prism/internal/obs"
	"go.opentelemetry.io/otel/trace"
)

var ErrParamMissing = errors.New("param missing")

// Dispatcher orchestrates the F-M-T-P pipeline for a single URL.
//
// Pipeline stages:
//
//	F (Fetcher)     — retrieve raw content
//	M (Minifier)    — strip noise, reduce size; archive point for S
//	T (Transformer) — semantic transforms (Stage 2, currently no-op)
//	P (Parser)      — extract structured Article
//
// The Save leg (S) is intentionally excluded from Dispatcher:
//   - On success, the caller archives DispatchResult.Minified via Saver
//   - On Minify failure, Dispatcher archives raw via the optional errorSaver,
//     preserving the original content for replay after a bug fix
type Dispatcher struct {
	logger      *slog.Logger
	tracer      trace.Tracer
	fetcher     Fetcher
	minifier    Minifier
	transformer Transformer
	parser      Parser
	errorSaver  Saver // optional: saves raw on Minify failure for later replay
}

func NewDispatcher(
	logger *slog.Logger,
	tracer trace.Tracer,
	fetcher Fetcher,
	minifier Minifier,
	transformer Transformer,
	parser Parser,
	errorSaver Saver,
) (*Dispatcher, error) {
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
	// errorSaver is optional
	return &Dispatcher{
		logger:      logger,
		tracer:      tracer,
		fetcher:     fetcher,
		minifier:    minifier,
		transformer: transformer,
		parser:      parser,
		errorSaver:  errorSaver,
	}, nil
}

// DispatchResult carries the parsed Article and the minified content.
// Minified is the archive point: pass it to the Saver (S leg) after a
// successful dispatch so the content can be replayed from this stage
// if the Parser or DB encounters a fault later.
type DispatchResult struct {
	Article  *Article
	Minified string
}

// Dispatch runs F-M-T-P for a single URL.
// On Minify failure the raw content is forwarded to errorSaver (if set)
// so the original page is preserved for replay after the bug is fixed.
func (d *Dispatcher) Dispatch(ctx context.Context, url string) (*DispatchResult, error) {
	ctx, span := d.tracer.Start(ctx, "collector.dispatcher.dispatch")
	defer span.End()

	// F: Fetch original raw content.
	raw, err := d.fetcher.Fetch(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}

	// M: Minify — strip noise and reduce size.
	// On failure, archive raw for later replay before returning the error.
	minified, err := d.minifier.Minify(ctx, raw)
	if err != nil {
		d.saveOnError(ctx, url, raw, err)
		return nil, fmt.Errorf("minify: %w", err)
	}

	// T: Transform — semantic Stage 2 (currently no-op for HTML).
	canonical, err := d.transformer.Transform(ctx, minified)
	if err != nil {
		return nil, fmt.Errorf("transform: %w", err)
	}

	// P: Parse canonical content into structured Article.
	article, err := d.parser.Parse(ctx, url, canonical)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	d.logger.DebugContext(ctx, "dispatch complete",
		slog.String("url", url),
		slog.String("title", article.Title),
	)

	return &DispatchResult{
		Article:  article,
		Minified: minified,
	}, nil
}

// saveOnError archives raw content when Minify fails.
// Failure is non-fatal: a warning is logged but the original Minify error is
// returned to the caller unchanged.
func (d *Dispatcher) saveOnError(ctx context.Context, url, raw string, minifyErr error) {
	if d.errorSaver == nil {
		return
	}

	archive := Archive{
		URL:       url,
		Payload:   raw,
		TraceID:   obs.ExtractTraceID(ctx),
		Timestamp: time.Now(),
		Metadata: map[string]any{
			"stage":        "raw",
			"error":        minifyErr.Error(),
			"recover_from": "minify",
		},
	}

	if err := d.errorSaver.Save(ctx, archive); err != nil {
		d.logger.WarnContext(ctx, "failed to archive raw content on minify error (content may be lost)",
			slog.String("url", url),
			slog.Any("error", err),
		)
	}
}
