package scout

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/ChiaYuChang/prism/internal/discovery"
	"github.com/ChiaYuChang/prism/internal/model"
	"go.opentelemetry.io/otel/trace"
)

const (
	ScoutSpanNameFormat   = "discovery.scout.%s.%s.discover"
	ScoutRegistrySpanName = "discovery.scout.registry.discover"
)

var ErrNoMatchingScout = errors.New("no matching scout registered for URL")
var ErrParamMissing = errors.New("param missing")
var ErrNoCandidatesFound = errors.New("no candidates found")
var ErrConfigFieldEmpty = errors.New("scout config field is empty")

// Registry routes discovery requests to a source-specific scout by URL host.
type Registry struct {
	logger *slog.Logger
	tracer trace.Tracer
	scouts map[string]discovery.Scout
}

func NewRegistry(logger *slog.Logger, tracer trace.Tracer, scouts map[string]discovery.Scout) (*Registry, error) {
	if logger == nil {
		return nil, fmt.Errorf("%w: logger", ErrParamMissing)
	}
	if tracer == nil {
		return nil, fmt.Errorf("%w: tracer", ErrParamMissing)
	}

	cloned := make(map[string]discovery.Scout, len(scouts))
	for host, scout := range scouts {
		if scout == nil {
			continue
		}
		cloned[strings.ToLower(host)] = scout
	}
	return &Registry{
		logger: logger,
		tracer: tracer,
		scouts: cloned,
	}, nil
}

func (r *Registry) Discover(ctx context.Context, rawURL string) ([]model.Candidates, error) {
	if r == nil {
		return nil, ErrNoMatchingScout
	}

	ctx, span := r.tracer.Start(ctx, ScoutRegistrySpanName)
	defer span.End()

	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}

	host := strings.ToLower(u.Hostname())
	scout, ok := r.scouts[host]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNoMatchingScout, host)
	}

	r.logger.DebugContext(ctx, "routing discovery request",
		slog.String("url", rawURL),
		slog.String("host", host),
	)

	return scout.Discover(ctx, rawURL)
}
