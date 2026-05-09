package llm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/ChiaYuChang/prism/internal/collector"
	"github.com/ChiaYuChang/prism/internal/llm"
)

var ErrParamMissing = errors.New("param missing")

// Parser extracts an Article from arbitrary text via an LLM. Used as the
// fallback parser when no host-specific entry matches in parsers.yaml. The
// JSON-schema response (ParserConfigJSONSchema) is shared with the snippet-
// generator role so an operator can promote the LLM output to a real
// html/jsonld rule once selectors look stable.
//
// The system instruction is supplied by the caller (typically loaded from
// assets/prompts/collector/article_parser.md). Keeping the prompt out of
// the binary lets operators iterate on extraction quality without
// rebuilding the worker image.
type Parser struct {
	generator llm.Generator
	logger    *slog.Logger
	model     string
	prompt    string
}

var _ collector.Parser = (*Parser)(nil)

// NewParser returns an LLM-backed parser. model is the provider-specific
// model identifier (e.g. "gemini-2.0-flash"); prompt is the system
// instruction text — load it from disk (see
// assets/prompts/collector/article_parser.md) and pass through unchanged.
func NewParser(generator llm.Generator, logger *slog.Logger, model, prompt string) (*Parser, error) {
	if generator == nil {
		return nil, fmt.Errorf("%w: generator", ErrParamMissing)
	}
	if logger == nil {
		return nil, fmt.Errorf("%w: logger", ErrParamMissing)
	}
	if prompt == "" {
		return nil, fmt.Errorf("%w: prompt", ErrParamMissing)
	}
	return &Parser{generator: generator, logger: logger, model: model, prompt: prompt}, nil
}

func (*Parser) String() string { return "LLMParser" }

func (p *Parser) Parse(ctx context.Context, url string, data string) (*collector.Article, error) {
	req := &llm.GenerateRequest{
		Model:             p.model,
		SystemInstruction: p.prompt,
		Prompt:            "URL: " + url + "\n\nHTML:\n" + data,
		Format:            llm.ResponseFormatJsonSchema,
		JSONSchema:        ParserConfigJSONSchema,
	}

	resp, err := p.generator.Generate(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("llm generate: %w", err)
	}

	var out LLMArticleContent
	if err := resp.DecodeJSONSchema(&out); err != nil {
		return nil, fmt.Errorf("llm decode: %w", err)
	}

	p.logger.DebugContext(ctx, "llm parser extracted article",
		slog.String("url", url),
		slog.Int("title_nodes", len(out.Title)),
		slog.Int("content_nodes", len(out.Content)),
		slog.Int("input_tokens", resp.Usage.InputTokenCount),
		slog.Int("output_tokens", resp.Usage.OutputTokenCount),
	)

	return out.ToArticleContent(url), nil
}
