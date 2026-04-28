package config

import (
	"fmt"
	"log/slog"

	"github.com/ChiaYuChang/prism/internal/collector"
	"github.com/ChiaYuChang/prism/internal/collector/parser"
	"github.com/ChiaYuChang/prism/internal/collector/parser/html"
	"github.com/ChiaYuChang/prism/internal/collector/parser/jsonld"
	"go.opentelemetry.io/otel/trace"
)

func BuildRegistry(cfg Config, logger *slog.Logger, tracer trace.Tracer) (*parser.Registry, error) {
	parsers := make(map[string]collector.Parser)

	for host, pCfg := range cfg.Parsers {
		if pCfg.Enabled != nil && !*pCfg.Enabled {
			continue
		}

		var hParser collector.Parser
		var jParser collector.Parser

		hParser = html.New(pCfg.HTML, pCfg.DateLayouts)

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

	return parser.NewRegistry(logger, tracer, parsers)
}
