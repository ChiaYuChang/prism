package parser

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/ChiaYuChang/prism/internal/collector"
	"go.opentelemetry.io/otel/trace"
)

var (
	ErrParamMissing     = errors.New("param missing")
	ErrNoMatchingParser = errors.New("no matching parser registered for host")
)

type Registry struct {
	logger   *slog.Logger
	tracer   trace.Tracer
	parsers  map[string]collector.Parser
	fallback collector.Parser
}

// NewRegistry builds a per-host parser registry. fallback is optional; when
// non-nil, Parse routes host-miss requests to it (with an info log) instead
// of returning ErrNoMatchingParser. Wire fallback from config.BuildRegistry
// — global fallback.enable in parsers.yaml.
func NewRegistry(logger *slog.Logger, tracer trace.Tracer, parsers map[string]collector.Parser, fallback collector.Parser) (*Registry, error) {
	if logger == nil {
		return nil, fmt.Errorf("%w: logger", ErrParamMissing)
	}
	if tracer == nil {
		return nil, fmt.Errorf("%w: tracer", ErrParamMissing)
	}

	cloned := make(map[string]collector.Parser, len(parsers))
	for host, p := range parsers {
		cloned[strings.ToLower(host)] = p
	}

	return &Registry{
		logger:   logger,
		tracer:   tracer,
		parsers:  cloned,
		fallback: fallback,
	}, nil
}

func (r *Registry) Parse(ctx context.Context, rawURL string, data string) (*collector.Article, error) {
	ctx, span := r.tracer.Start(ctx, "collector.parser.registry.parse")
	defer span.End()

	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}

	host := strings.ToLower(u.Hostname())
	if p, ok := r.parsers[host]; ok {
		r.logger.DebugContext(ctx, "using specific parser for host", slog.String("host", host))
		return p.Parse(ctx, rawURL, data)
	}

	if r.fallback != nil {
		r.logger.InfoContext(ctx, "no host-specific parser; using fallback",
			slog.String("host", host), slog.String("url", rawURL))
		return r.fallback.Parse(ctx, rawURL, data)
	}

	return nil, fmt.Errorf("%w: %s", ErrNoMatchingParser, host)
}
