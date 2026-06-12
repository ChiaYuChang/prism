package factory_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/ChiaYuChang/prism/internal/appconfig"
	llmfactory "github.com/ChiaYuChang/prism/internal/llm/factory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNewGenerator_UnsupportedProvider(t *testing.T) {
	cfg := appconfig.LLMConfig{Provider: "not-a-real-provider", Model: "x", Key: "y"}
	_, err := llmfactory.NewGenerator(context.Background(), cfg, discardLogger())
	require.Error(t, err)
	assert.ErrorContains(t, err, "unsupported LLM provider")
}

func TestNewEmbedder_UnsupportedProvider(t *testing.T) {
	cfg := appconfig.LLMConfig{Provider: "not-a-real-provider", Model: "x", Key: "y"}
	_, err := llmfactory.NewEmbedder(context.Background(), cfg, discardLogger())
	require.Error(t, err)
	assert.ErrorContains(t, err, "unsupported LLM provider")
}

func TestNewProvider_UnsupportedProvider(t *testing.T) {
	cfg := appconfig.LLMConfig{Provider: "not-a-real-provider", Model: "x", Key: "y"}
	_, err := llmfactory.NewProvider(context.Background(), cfg, discardLogger())
	require.Error(t, err)
	assert.ErrorContains(t, err, "unsupported LLM provider")
}

// Provider construction success paths are covered by the per-provider
// unit tests in internal/llm/{gemini,openai,ollama}. This file only guards
// the dispatch / unsupported-provider branch — the actual provider code
// requires real API keys / endpoints that don't belong in unit tests.
