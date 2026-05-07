// Replay tests use cassettes captured by the manual smoke tests so the
// OpenAI provider's request-shaping, Responses-API parsing, schema
// decode, and error classification can be exercised offline. Run as
// part of the default `go test ./...`.
package openai_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
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

func newReplayProvider(t *testing.T, ctx context.Context, c cassette) *openai.Provider {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracer := noop.NewTracerProvider().Tracer("openai-replay")
	v := validator.New()
	m := mold.New()
	hc := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &replayTransport{c: c},
	}
	p, err := openai.New(ctx, logger, tracer, v, m, hc, openai.Config{
		APIKey:  "replay-fixture-key",
		BaseURL: "http://replay-fixture.invalid/v1",
		Timeout: 5 * time.Second,
	})
	require.NoError(t, err)
	return p
}

func TestOpenAIReplay_GenerateText(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p := newReplayProvider(t, ctx, loadCassette(t, "generate_text"))

	resp, err := p.Generate(ctx, &llm.GenerateRequest{
		Model:             "gemma-4-E4B-it:Q4_K_M",
		SystemInstruction: "Reply with a single short word.",
		Prompt:            "Say hi.",
		Temperature:       utils.Ptr(float32(0.0)),
		Format:            llm.ResponseFormatText,
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Text)
}

func TestOpenAIReplay_GenerateJSONSchema(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p := newReplayProvider(t, ctx, loadCassette(t, "generate_jsonschema"))

	type Greeting struct {
		Greeting string `json:"greeting"`
		Language string `json:"language"`
	}
	schema := pkgschema.NewSkeleton[Greeting]("greeting", 1)

	resp, err := p.Generate(ctx, &llm.GenerateRequest{
		Model:       "gemma-4-E4B-it:Q4_K_M",
		Prompt:      "Greet someone in Traditional Chinese.",
		Temperature: utils.Ptr(float32(0.0)),
		Format:      llm.ResponseFormatJsonSchema,
		JSONSchema:  schema,
	})
	require.NoError(t, err)

	var out Greeting
	require.NoError(t, resp.DecodeJSONSchema(&out))
	require.NotEmpty(t, out.Greeting)
	require.NotEmpty(t, out.Language)
}

func TestOpenAIReplay_Embed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p := newReplayProvider(t, ctx, loadCassette(t, "embed"))

	resp, err := p.Embed(ctx, &llm.EmbedRequest{
		Model: "embeddinggemma:300m",
		Input: []string{"立法院今日通過國會改革法案"},
	})
	require.NoError(t, err)
	require.Len(t, resp.Vectors, 1)
	require.Greater(t, len(resp.Vectors[0]), 0)
}

// TestOpenAIReplay_ModelNotFound exercises the 404 error path: the
// provider must surface a wrapped llm.ErrGenAPIError so callers can
// classify failures without inspecting strings.
func TestOpenAIReplay_ModelNotFound(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p := newReplayProvider(t, ctx, loadCassette(t, "model_not_found"))

	_, err := p.Generate(ctx, &llm.GenerateRequest{
		Model:  "definitely-not-a-real-model",
		Prompt: "Say hi.",
		Format: llm.ResponseFormatText,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, llm.ErrGenAPIError)
}
