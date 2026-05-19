// Replay tests use cassettes captured by the manual smoke tests so the
// Ollama provider's request-shaping, response-parsing, schema decode,
// and error classification can be exercised offline — no local server
// required. Run as part of the default `go test ./...`.
package ollama_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/url"
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

// newReplayProvider builds an Ollama provider whose http.Client always
// returns the supplied cassette. BaseURL is a literal placeholder; the
// transport ignores the URL so no DNS / TCP / Unix-socket access happens.
func newReplayProvider(t *testing.T, ctx context.Context, c cassette) *ollama.Provider {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracer := noop.NewTracerProvider().Tracer("ollama-replay")
	v := validator.New()
	m := mold.New()
	hc := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &replayTransport{c: c},
	}
	p, err := ollama.New(ctx, logger, tracer, v, m, hc, ollama.Config{
		BaseURL: "http://replay-fixture.invalid",
		Timeout: 5 * time.Second,
	})
	require.NoError(t, err)
	return p
}

type captureURLTransport struct {
	c   cassette
	url *url.URL
}

func (t *captureURLTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.url = req.URL
	return (&replayTransport{c: t.c}).RoundTrip(req)
}

func TestOllamaNew_DefaultsEmptyBaseURL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracer := noop.NewTracerProvider().Tracer("ollama-replay")
	transport := &captureURLTransport{c: loadCassette(t, "generate_text")}
	hc := &http.Client{Timeout: 5 * time.Second, Transport: transport}
	p, err := ollama.New(ctx, logger, tracer, validator.New(), mold.New(), hc, ollama.Config{})
	require.NoError(t, err)

	_, err = p.Generate(ctx, &llm.GenerateRequest{
		Model:  "gemma-4-E4B-it:Q4_K_M",
		Prompt: "Say hi.",
		Format: llm.ResponseFormatText,
	})
	require.NoError(t, err)
	require.Equal(t, "http", transport.url.Scheme)
	require.Equal(t, "localhost:11434", transport.url.Host)
}

func TestOllamaReplay_GenerateText(t *testing.T) {
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

func TestOllamaReplay_GenerateJSONSchema(t *testing.T) {
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

func TestOllamaReplay_Embed(t *testing.T) {
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

// TestOllamaReplay_ModelNotFound exercises the 404 error path: the
// provider must surface a wrapped llm.ErrGenAPIError so callers can
// classify API failures without inspecting strings.
func TestOllamaReplay_ModelNotFound(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p := newReplayProvider(t, ctx, loadCassette(t, "model_not_found"))

	_, err := p.Generate(ctx, &llm.GenerateRequest{
		Model:  "definitely-not-a-real-model:tag",
		Prompt: "Say hi.",
		Format: llm.ResponseFormatText,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, llm.ErrGenAPIError)
}
