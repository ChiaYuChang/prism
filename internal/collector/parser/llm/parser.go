package llm

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	neturl "net/url"
	"path"
	"strings"

	"github.com/ChiaYuChang/prism/internal/collector"
	"github.com/ChiaYuChang/prism/internal/llm"
)

var (
	ErrParamMissing = errors.New("param missing")
)

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
	u, err := neturl.Parse(url)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}
	ext := strings.ToLower(path.Ext(u.Path))
	if ext == ".json" || ext == ".xml" {
		return nil, fmt.Errorf("%w: %s", collector.ErrUnsupportedFallbackType, ext)
	}

	trimmedData := strings.TrimSpace(data)
	if isJSONPayload(trimmedData) {
		return nil, fmt.Errorf("%w: JSON payload detected", collector.ErrUnsupportedFallbackType)
	}
	if isXMLPayload(trimmedData) {
		return nil, fmt.Errorf("%w: XML payload detected", collector.ErrUnsupportedFallbackType)
	}

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
		slog.Int("input_tokens", resp.Usage.Input),
		slog.Int("output_tokens", resp.Usage.Output),
	)

	return out.ToArticleContent(url), nil
}

func isJSONPayload(data string) bool {
	if !strings.HasPrefix(data, "{") && !strings.HasPrefix(data, "[") {
		return false
	}
	return json.Valid([]byte(data))
}

func isXMLPayload(data string) bool {
	if strings.HasPrefix(strings.ToLower(data), "<?xml") {
		return true
	}
	if !strings.HasPrefix(data, "<") {
		return false
	}
	limitReader := io.LimitReader(strings.NewReader(data), 8192)
	decoder := xml.NewDecoder(limitReader)
	for {
		t, err := decoder.Token()
		if err != nil {
			return false
		}
		if se, ok := t.(xml.StartElement); ok {
			return !strings.EqualFold(se.Name.Local, "html")
		}
	}
}
