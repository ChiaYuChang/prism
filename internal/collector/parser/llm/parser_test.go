package llm_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

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

func TestParser_String(t *testing.T) {
	p, err := parserllm.NewParser(&fakeGenerator{}, discardLogger(), "test-model", "stub prompt")
	require.NoError(t, err)
	assert.Equal(t, "LLMParser", p.String())
}
