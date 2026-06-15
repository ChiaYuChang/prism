package llm_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/ChiaYuChang/prism/internal/collector"
	parserllm "github.com/ChiaYuChang/prism/internal/collector/parser/llm"
	"github.com/ChiaYuChang/prism/internal/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeGenerator struct {
	resp *llm.GenerateResponse
	err  error
}

func (f *fakeGenerator) Generate(_ context.Context, _ *llm.GenerateRequest) (*llm.GenerateResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNewParser_NilGenerator(t *testing.T) {
	_, err := parserllm.NewParser(nil, discardLogger(), "test-model", "stub prompt")
	require.Error(t, err)
	assert.ErrorIs(t, err, parserllm.ErrParamMissing)
}

func TestNewParser_NilLogger(t *testing.T) {
	_, err := parserllm.NewParser(&fakeGenerator{}, nil, "test-model", "stub prompt")
	require.Error(t, err)
	assert.ErrorIs(t, err, parserllm.ErrParamMissing)
}

func TestNewParser_EmptyPrompt(t *testing.T) {
	_, err := parserllm.NewParser(&fakeGenerator{}, discardLogger(), "test-model", "")
	require.Error(t, err)
	assert.ErrorIs(t, err, parserllm.ErrParamMissing)
}

func TestParser_Parse_Success(t *testing.T) {
	const cannedJSON = `{
  "title": [{"selector": "h1", "value": "Example Headline"}],
  "author": [{"selector": ".byline", "value": "Jane Doe"}],
  "published_at": [{"selector": "time", "value": "2026-04-01"}],
  "date_layouts": ["2006-01-02"],
  "content": [{"selector": "article p", "value": "First paragraph body."}]
}`

	gen := &fakeGenerator{
		resp: &llm.GenerateResponse{
			Model:      "test-model",
			Text:       cannedJSON,
			JsonSchema: parserllm.ParserConfigJSONSchema,
		},
	}

	p, err := parserllm.NewParser(gen, discardLogger(), "test-model", "stub prompt")
	require.NoError(t, err)

	article, err := p.Parse(context.Background(), "https://example.com/post/1", "<html><body>...</body></html>")
	require.NoError(t, err)
	require.NotNil(t, article)

	assert.Equal(t, "https://example.com/post/1", article.URL)
	assert.Equal(t, "Example Headline", article.Title)
	assert.Equal(t, "Jane Doe", article.Author)
	assert.Equal(t, "First paragraph body.", article.Content)
	assert.False(t, article.PublishedAt.IsZero(), "published_at should parse via supplied layout")
	assert.Equal(t, 2026, article.PublishedAt.Year())
}

func TestParser_Parse_GeneratorError(t *testing.T) {
	sentinel := errors.New("upstream broker down")
	gen := &fakeGenerator{err: sentinel}

	p, err := parserllm.NewParser(gen, discardLogger(), "test-model", "stub prompt")
	require.NoError(t, err)

	_, err = p.Parse(context.Background(), "https://example.com/", "<html></html>")
	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel)
	assert.ErrorContains(t, err, "llm generate")
}

func TestParser_Parse_DecodeError(t *testing.T) {
	gen := &fakeGenerator{
		resp: &llm.GenerateResponse{
			Text:       `{"title": "not-a-list"}`, // schema expects array
			JsonSchema: parserllm.ParserConfigJSONSchema,
		},
	}

	p, err := parserllm.NewParser(gen, discardLogger(), "test-model", "stub prompt")
	require.NoError(t, err)

	_, err = p.Parse(context.Background(), "https://example.com/", "<html></html>")
	require.Error(t, err)
	assert.ErrorContains(t, err, "llm decode")
}

func TestParser_Parse_InputTypeValidation(t *testing.T) {
	const cannedJSON = `{
  "title": [{"selector": "h1", "value": "Example Headline"}],
  "author": [],
  "published_at": [],
  "date_layouts": [],
  "content": [{"selector": "p", "value": "Content body"}]
}`
	gen := &fakeGenerator{
		resp: &llm.GenerateResponse{
			Model:      "test-model",
			Text:       cannedJSON,
			JsonSchema: parserllm.ParserConfigJSONSchema,
		},
	}

	p, err := parserllm.NewParser(gen, discardLogger(), "test-model", "stub prompt")
	require.NoError(t, err)

	tests := []struct {
		url     string
		wantErr bool
	}{
		{"https://example.com/post/1.html", false},
		{"https://example.com/post/1.htm", false},
		{"https://example.com/post/1", false},
		{"https://example.com/post/1/", false},
		{"https://example.com/post/1.json", true},
		{"https://example.com/post/1.xml", true},
		{"https://example.com/post/1.JSON", true},
		{"https://example.com/post/1.XML", true},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			article, err := p.Parse(context.Background(), tt.url, "<html></html>")
			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, collector.ErrUnsupportedFallbackType)
				assert.Nil(t, article)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, article)
			}
		})
	}
}

func TestParser_Parse_PayloadTypeValidation(t *testing.T) {
	const cannedJSON = `{
  "title": [{"selector": "h1", "value": "Example Headline"}],
  "author": [],
  "published_at": [],
  "date_layouts": [],
  "content": [{"selector": "p", "value": "Content body"}]
}`
	gen := &fakeGenerator{
		resp: &llm.GenerateResponse{
			Model:      "test-model",
			Text:       cannedJSON,
			JsonSchema: parserllm.ParserConfigJSONSchema,
		},
	}

	p, err := parserllm.NewParser(gen, discardLogger(), "test-model", "stub prompt")
	require.NoError(t, err)

	tests := []struct {
		name    string
		url     string
		data    string
		wantErr bool
	}{
		{
			name:    "valid HTML",
			url:     "https://example.com/post/1",
			data:    "<html><body><h1>Hello</h1></body></html>",
			wantErr: false,
		},
		{
			name:    "valid HTML with doctype",
			url:     "https://example.com/post/1",
			data:    "<!DOCTYPE html><html><body><h1>Hello</h1></body></html>",
			wantErr: false,
		},
		{
			name:    "valid JSON object payload",
			url:     "https://example.com/api/v1/news",
			data:    `  { "title": "news title", "content": "body" }  `,
			wantErr: true,
		},
		{
			name:    "valid JSON array payload",
			url:     "https://example.com/api/v1/news",
			data:    `[{"title": "news"}]`,
			wantErr: true,
		},
		{
			name:    "valid XML proc-inst rss payload",
			url:     "https://example.com/feed",
			data:    `<?xml version="1.0" encoding="UTF-8"?><rss><channel><title>RSS</title></channel></rss>`,
			wantErr: true,
		},
		{
			name:    "valid XML rss payload without proc-inst",
			url:     "https://example.com/feed",
			data:    ` <rss version="2.0"><channel><title>RSS</title></channel></rss> `,
			wantErr: true,
		},
		{
			name:    "valid XML atom payload without proc-inst",
			url:     "https://example.com/feed",
			data:    `<feed><title>Atom</title></feed>`,
			wantErr: true,
		},
		{
			name:    "invalid xml (looks like xml but is actually malformed html)",
			url:     "https://example.com/post/1",
			data:    `<html><meta charset="utf-8"><br></html>`,
			wantErr: false,
		},
		{
			name:    "plain text",
			url:     "https://example.com/post/1",
			data:    `This is plain text and not JSON or XML`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			article, err := p.Parse(context.Background(), tt.url, tt.data)
			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, collector.ErrUnsupportedFallbackType)
				assert.Nil(t, article)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, article)
			}
		})
	}
}

func TestParser_String(t *testing.T) {
	p, err := parserllm.NewParser(&fakeGenerator{}, discardLogger(), "test-model", "stub prompt")
	require.NoError(t, err)
	assert.Equal(t, "LLMParser", p.String())
}
