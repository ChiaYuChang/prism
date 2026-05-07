//go:build manual

// Manual smoke tests against an OpenAI-compatible Responses endpoint.
// Default base URL is Ollama's compat shim at http://localhost:11434/v1
// since most non-OpenAI servers (vLLM, llama.cpp) implement only Chat
// Completions; Ollama is the local server known to honor /v1/responses.
//
// Run with `go test -tags=manual -count=1 -run OpenAI ./internal/llm/openai/...`.
//
// Env knobs:
//   PRISM_OPENAI_RECORD=1               capture cassettes
//   PRISM_OPENAI_BASE_URL=...           override base URL
//   PRISM_OPENAI_KEY_FILE=path / PRISM_OPENAI_KEY=raw
//   PRISM_OPENAI_GENERATE_MODEL=...     default gemma-4-E4B-it:Q4_K_M
//   PRISM_OPENAI_EMBED_MODEL=...        unset → embed test skipped
package openai_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/llm"
	"github.com/ChiaYuChang/prism/internal/llm/openai"
	pkgschema "github.com/ChiaYuChang/prism/pkg/schema"
	"github.com/ChiaYuChang/prism/pkg/utils"
	"github.com/go-playground/mold/v4"
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

const (
	defaultBaseURL       = "http://localhost:11434/v1"
	defaultGenerateModel = "gemma-4-E4B-it:Q4_K_M"
)

func baseURL() string {
	if v := os.Getenv("PRISM_OPENAI_BASE_URL"); v != "" {
		return v
	}
	return defaultBaseURL
}

func generateModel() string {
	if v := os.Getenv("PRISM_OPENAI_GENERATE_MODEL"); v != "" {
		return v
	}
	return defaultGenerateModel
}

func embedModel() string {
	return os.Getenv("PRISM_OPENAI_EMBED_MODEL")
}

// loadAPIKey returns a non-empty key without ever logging it. For local
// Ollama-compat targets the value is unused server-side; a placeholder
// keeps Config validation happy. For real OpenAI, supply via env.
func loadAPIKey(t *testing.T) string {
	t.Helper()
	if path := os.Getenv("PRISM_OPENAI_KEY_FILE"); path != "" {
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		if k := strings.TrimSpace(string(raw)); k != "" {
			return k
		}
	}
	if k := strings.TrimSpace(os.Getenv("PRISM_OPENAI_KEY")); k != "" {
		return k
	}
	// Local-target placeholder. Servers that don't validate (Ollama
	// compat shim, llama.cpp) accept anything non-empty.
	return "test-placeholder-key"
}

func newRecordingProvider(t *testing.T, ctx context.Context, cassetteName string) *openai.Provider {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracer := noop.NewTracerProvider().Tracer("openai-smoke")
	v := validator.New()
	m := mold.New()
	hc := &http.Client{
		Timeout: 120 * time.Second,
		Transport: &recordingTransport{
			t:    t,
			name: cassetteName,
			base: http.DefaultTransport,
		},
	}
	p, err := openai.New(ctx, logger, tracer, v, m, hc, openai.Config{
		APIKey:  loadAPIKey(t),
		BaseURL: baseURL(),
		Timeout: 120 * time.Second,
	})
	require.NoError(t, err)
	return p
}

func TestOpenAIGenerate_Text(t *testing.T) {
	if !recordingEnabled() {
		t.Skip("set PRISM_OPENAI_RECORD=1 to call the live server and capture cassette")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	p := newRecordingProvider(t, ctx, "generate_text")

	resp, err := p.Generate(ctx, &llm.GenerateRequest{
		Model:             generateModel(),
		SystemInstruction: "Reply with a single short word.",
		Prompt:            "Say hi.",
		Temperature:       utils.Ptr(float32(0.0)),
		Format:            llm.ResponseFormatText,
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Text)
	t.Logf("model=%s text_len=%d total_tokens=%d", resp.Model, len(resp.Text), resp.Usage.TotalTokenCount)
}

// TestOpenAIGenerate_JSONSchema exercises Responses API structured
// output: provider sends `text.format.json_schema`; server enforces
// schema and returns JSON matching it; DecodeJSONSchema validates +
// decodes into a typed struct.
func TestOpenAIGenerate_JSONSchema(t *testing.T) {
	if !recordingEnabled() {
		t.Skip("set PRISM_OPENAI_RECORD=1 to call the live server and capture cassette")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	p := newRecordingProvider(t, ctx, "generate_jsonschema")

	type Greeting struct {
		Greeting string `json:"greeting"`
		Language string `json:"language"`
	}
	schema := pkgschema.NewSkeleton[Greeting]("greeting", 1)

	resp, err := p.Generate(ctx, &llm.GenerateRequest{
		Model: generateModel(),
		SystemInstruction: "You must reply with a single JSON object and nothing else. " +
			"Fields: greeting (short greeting), language (ISO 639-1).",
		Prompt:      "Greet someone in Traditional Chinese.",
		Temperature: utils.Ptr(float32(0.0)),
		Format:      llm.ResponseFormatJsonSchema,
		JSONSchema:  schema,
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Text)

	var out Greeting
	require.NoError(t, resp.DecodeJSONSchema(&out))
	require.NotEmpty(t, out.Greeting)
	require.NotEmpty(t, out.Language)
	t.Logf("model=%s greeting_len=%d language=%s", resp.Model, len(out.Greeting), out.Language)
}

func TestOpenAIEmbed(t *testing.T) {
	if !recordingEnabled() {
		t.Skip("set PRISM_OPENAI_RECORD=1 to call the live server and capture cassette")
	}
	if embedModel() == "" {
		t.Skip("PRISM_OPENAI_EMBED_MODEL not set; pull / configure an embed model and set the env to capture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	p := newRecordingProvider(t, ctx, "embed")

	resp, err := p.Embed(ctx, &llm.EmbedRequest{
		Model: embedModel(),
		Input: []string{"立法院今日通過國會改革法案"},
	})
	require.NoError(t, err)
	require.Len(t, resp.Vectors, 1)
	require.Greater(t, len(resp.Vectors[0]), 0)
	t.Logf("model=%s vectors=%d dim=%d", resp.Model, len(resp.Vectors), len(resp.Vectors[0]))
}

// TestOpenAIModelNotFound captures the error returned for an unknown
// model. Replay tests assert classification as llm.ErrGenAPIError.
func TestOpenAIModelNotFound(t *testing.T) {
	if !recordingEnabled() {
		t.Skip("set PRISM_OPENAI_RECORD=1 to call the live server and capture cassette")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	p := newRecordingProvider(t, ctx, "model_not_found")

	_, err := p.Generate(ctx, &llm.GenerateRequest{
		Model:  "definitely-not-a-real-model",
		Prompt: "Say hi.",
		Format: llm.ResponseFormatText,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, llm.ErrGenAPIError)
}
