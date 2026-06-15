package config

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/ChiaYuChang/prism/internal/collector"
	"github.com/ChiaYuChang/prism/internal/collector/parser"
	"github.com/ChiaYuChang/prism/internal/collector/parser/html"
	"github.com/ChiaYuChang/prism/internal/collector/parser/jsonld"
	"go.opentelemetry.io/otel/trace"
)

// ErrFallbackEnabledNoFactory is returned when parsers.yaml sets
// fallback.enable=true but the caller did not supply an llmFactory. Caught
// at startup so a misconfigured deploy fails loudly instead of silently
// disabling fallback at runtime.
var ErrFallbackEnabledNoFactory = errors.New("fallback enabled but no llm factory supplied")

// LLMFactory builds the fallback collector.Parser used when no host-specific
// entry matches. Returning a func keeps this package free of an llm import;
// the call site (cmd/worker/collector / cmd/recover / parse-probe) wires
// the actual provider. May be nil when fallback is disabled in config.
type LLMFactory func() (collector.Parser, error)

// BuildRegistry assembles a parser.Registry from the supplied config. When
// cfg.Fallback.Enable is true and llmFactory is non-nil, the returned
// registry routes host-miss requests to the factory-built fallback parser
// instead of returning ErrNoMatchingParser.
func BuildRegistry(cfg Config, logger *slog.Logger, tracer trace.Tracer, llmFactory LLMFactory) (*parser.Registry, error) {
	parsers := make(map[string]collector.Parser)

	for host, pCfg := range cfg.Parsers {
		if pCfg.Enabled != nil && !*pCfg.Enabled {
			continue
		}

		if pCfg.HTML == nil {
			return nil, fmt.Errorf("%w: missing html rules for host %s", collector.ErrUnsupportedFallbackType, host)
		}

		var hParser collector.Parser
		var jParser collector.Parser

		hParser = html.New(*pCfg.HTML, pCfg.DateLayouts)

		if pCfg.JSONLD {
			jParser = jsonld.New()
			// JSON-LD is prioritized over HTML by placing it first in the list
			cp, err := parser.NewCompositeParser(logger, jParser, hParser)
			if err != nil {
				return nil, fmt.Errorf("build composite parser for %s: %w", host, err)
			}
			parsers[host] = cp
		} else {
			parsers[host] = hParser
		}
	}

	var fallback collector.Parser
	if cfg.Fallback.Enable {
		if llmFactory == nil {
			return nil, ErrFallbackEnabledNoFactory
		}
		fb, err := llmFactory()
		if err != nil {
			return nil, fmt.Errorf("build fallback parser: %w", err)
		}
		fallback = fb
	}

	return parser.NewRegistry(logger, tracer, parsers, fallback)
}
