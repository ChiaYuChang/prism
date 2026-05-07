// Replay tests use cassettes captured by the manual smoke tests so the
// Gemini provider's request-shaping, response-parsing, schema decode, and
// error classification can be exercised offline — no API key or network
// access required. Run as part of the default `go test ./...`.
package gemini_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/llm"
	"github.com/ChiaYuChang/prism/internal/llm/gemini"
	pkgschema "github.com/ChiaYuChang/prism/pkg/schema"
	"github.com/ChiaYuChang/prism/pkg/utils"
	"github.com/go-playground/mold/v4"
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

// newReplayProvider builds a Gemini provider whose http.Client always
// returns the supplied cassette. APIKey is non-empty so the config
// validator is satisfied; the cassette transport ignores the request, so
// the value is never sent anywhere.
func newReplayProvider(t *testing.T, ctx context.Context, c cassette) *gemini.Provider {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracer := noop.NewTracerProvider().Tracer("gemini-replay")
	v := validator.New()
	m := mold.New()
	hc := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &replayTransport{c: c},
	}
	p, err := gemini.New(ctx, logger, tracer, v, m, hc, gemini.Config{
		APIKey:  "replay-fixture-key",
		Timeout: 5 * time.Second,
	})
	require.NoError(t, err)
	return p
}

func TestGeminiReplay_GenerateText(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p := newReplayProvider(t, ctx, loadCassette(t, "generate_text"))

	resp, err := p.Generate(ctx, &llm.GenerateRequest{
		Model:             "gemini-2.5-flash",
		SystemInstruction: "Reply with a single short word.",
		Prompt:            "Say hi.",
		Temperature:       utils.Ptr(float32(0.0)),
		Format:            llm.ResponseFormatText,
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Text)
	require.Greater(t, resp.Usage.TotalTokenCount, 0)
}

func TestGeminiReplay_GenerateJSONSchema(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p := newReplayProvider(t, ctx, loadCassette(t, "generate_jsonschema"))

	type Greeting struct {
		Greeting string `json:"greeting"`
		Language string `json:"language"`
	}
	schema := pkgschema.NewSkeleton[Greeting]("greeting", 1)

	resp, err := p.Generate(ctx, &llm.GenerateRequest{
		Model:             "gemini-2.5-flash",
		SystemInstruction: "Return a short greeting and the ISO 639-1 language code.",
		Prompt:            "Greet someone in Traditional Chinese.",
		Temperature:       utils.Ptr(float32(0.0)),
		Format:            llm.ResponseFormatJsonSchema,
		JSONSchema:        schema,
	})
	require.NoError(t, err)

	var out Greeting
	require.NoError(t, resp.DecodeJSONSchema(&out))
	require.NotEmpty(t, out.Greeting)
	require.NotEmpty(t, out.Language)
}

func TestGeminiReplay_Embed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p := newReplayProvider(t, ctx, loadCassette(t, "embed"))

	resp, err := p.Embed(ctx, &llm.EmbedRequest{
		Model: "gemini-embedding-001",
		Input: []string{"立法院今日通過國會改革法案"},
	})
	require.NoError(t, err)
	require.Len(t, resp.Vectors, 1)
	require.Greater(t, len(resp.Vectors[0]), 0)
}

// TestGeminiReplay_Unauthorized exercises the 4xx error path: the
// provider must surface a wrapped ErrGenAPIError so callers can classify
// API failures without inspecting strings.
func TestGeminiReplay_Unauthorized(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p := newReplayProvider(t, ctx, loadCassette(t, "unauthorized"))

	_, err := p.Generate(ctx, &llm.GenerateRequest{
		Model:             "gemini-2.5-flash",
		SystemInstruction: "Reply with a single short word.",
		Prompt:            "Say hi.",
		Format:            llm.ResponseFormatText,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, llm.ErrGenAPIError)
}
