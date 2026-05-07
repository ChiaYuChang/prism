//go:build manual

// Manual smoke tests against a live local Ollama server. Skipped by default.
// Run with `go test -tags=manual -count=1 -run Ollama ./internal/llm/ollama/...`.
//
// PRISM_OLLAMA_BASE_URL overrides the default `http://localhost:11434`.
// PRISM_OLLAMA_GENERATE_MODEL / PRISM_OLLAMA_EMBED_MODEL select models.
// PRISM_OLLAMA_RECORD=1 fires live calls and captures cassettes for
// replay. Replay tests in ollama_replay_test.go consume them offline.
package ollama_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/llm"
	"github.com/ChiaYuChang/prism/internal/llm/ollama"
	pkgschema "github.com/ChiaYuChang/prism/pkg/schema"
	"github.com/ChiaYuChang/prism/pkg/utils"
	"github.com/go-playground/mold/v4"
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

const defaultGenerateModel = "gemma-4-E4B-it:Q4_K_M"

func generateModel() string {
	if v := os.Getenv("PRISM_OLLAMA_GENERATE_MODEL"); v != "" {
		return v
	}
	return defaultGenerateModel
}

func embedModel() string {
	return os.Getenv("PRISM_OLLAMA_EMBED_MODEL")
}

func baseURL() string {
	if v := os.Getenv("PRISM_OLLAMA_BASE_URL"); v != "" {
		return v
	}
	return "http://localhost:11434"
}

// newRecordingProvider wires an Ollama provider whose http.Client persists
// the next response under the given cassette name.
func newRecordingProvider(t *testing.T, ctx context.Context, cassetteName string) *ollama.Provider {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracer := noop.NewTracerProvider().Tracer("ollama-smoke")
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
	p, err := ollama.New(ctx, logger, tracer, v, m, hc, ollama.Config{
		BaseURL: baseURL(),
		Timeout: 120 * time.Second,
	})
	require.NoError(t, err)
	return p
}

func TestOllamaGenerate_Text(t *testing.T) {
	if !recordingEnabled() {
		t.Skip("set PRISM_OLLAMA_RECORD=1 to call the live server and capture cassette")
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
	t.Logf("model=%s text_len=%d", resp.Model, len(resp.Text))
}

// TestOllamaGenerate_JSONSchema exercises the structured-output path —
// Ollama's `Format` field receives the schema bytes; the model is
// expected to emit JSON matching the schema, which DecodeJSONSchema
// then validates and decodes into a typed struct.
//
// Not every local model honors structured output reliably (small
// quantized variants in particular freestyle as raw text). The
// system prompt below is deliberately blunt; if the chosen model still
// produces non-JSON, switch via PRISM_OLLAMA_GENERATE_MODEL to one with
// stronger format-following (e.g. llama3.1, qwen2.5).
func TestOllamaGenerate_JSONSchema(t *testing.T) {
	if !recordingEnabled() {
		t.Skip("set PRISM_OLLAMA_RECORD=1 to call the live server and capture cassette")
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
			"The object has exactly two string fields: `greeting` (a short greeting) and " +
			"`language` (ISO 639-1 code). Do not wrap in markdown. Do not add prose.",
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

func TestOllamaEmbed(t *testing.T) {
	if !recordingEnabled() {
		t.Skip("set PRISM_OLLAMA_RECORD=1 to call the live server and capture cassette")
	}
	if embedModel() == "" {
		t.Skip("PRISM_OLLAMA_EMBED_MODEL not set; pull an embed model and set the env to capture")
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

// TestOllamaModelNotFound captures the 404 returned for an unknown
// model. Replay tests assert the provider classifies the response as
// llm.ErrGenAPIError without inspecting strings.
func TestOllamaModelNotFound(t *testing.T) {
	if !recordingEnabled() {
		t.Skip("set PRISM_OLLAMA_RECORD=1 to call the live server and capture cassette")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	p := newRecordingProvider(t, ctx, "model_not_found")

	_, err := p.Generate(ctx, &llm.GenerateRequest{
		Model:  "definitely-not-a-real-model:tag",
		Prompt: "Say hi.",
		Format: llm.ResponseFormatText,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, llm.ErrGenAPIError)
}
