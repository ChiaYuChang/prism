package parser

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ChiaYuChang/prism/internal/collector"
)

// CompositeParser runs a list of parsers in sequence and merges their results.
// It follows a coalesce pattern where fields are populated by the first
// parser that provides them.
type CompositeParser struct {
	logger  *slog.Logger
	parsers []collector.Parser
}

var _ collector.Parser = (*CompositeParser)(nil)

func NewCompositeParser(logger *slog.Logger, parsers ...collector.Parser) (*CompositeParser, error) {
	if logger == nil {
		return nil, fmt.Errorf("%w: logger", ErrParamMissing)
	}
	if len(parsers) == 0 {
		return nil, fmt.Errorf("%w: at least one parser is required", ErrParamMissing)
	}
	return &CompositeParser{
		logger:  logger,
		parsers: parsers,
	}, nil
}

func (p *CompositeParser) Parse(ctx context.Context, url string, data string) (*collector.Article, error) {
	var final *collector.Article

	for i, parser := range p.parsers {
		result, err := parser.Parse(ctx, url, data)
		if err != nil {
			p.logger.DebugContext(ctx, "sub-parser failed",
				slog.Int("index", i),
				slog.String("url", url),
				slog.Any("error", err),
			)
			continue
		}

		if final == nil {
			final = result
			continue
		}

		var filled []string
		if final.Title == "" && result.Title != "" {
			filled = append(filled, "title")
		}
		if final.Content == "" && result.Content != "" {
			filled = append(filled, "content")
		}
		if final.Author == "" && result.Author != "" {
			filled = append(filled, "author")
		}
		if final.PublishedAt.IsZero() && !result.PublishedAt.IsZero() {
			filled = append(filled, "published_at")
		}

		// Treat the accumulated 'final' as the priority over the new 'result'
		// so that fields provided by earlier parsers are not overwritten.
		final = MergeArticleContent(result, final)

		if len(filled) > 0 {
			p.logger.DebugContext(ctx, "sub-parser filled missing fields",
				slog.Int("index", i),
				slog.String("url", url),
				slog.Any("fields", filled),
			)
		}
	}

	if final == nil {
		return nil, fmt.Errorf("all parsers failed for %s", url)
	}

	final.URL = url
	return final, nil
}
