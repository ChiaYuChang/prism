//go:build manual

// Manual smoke tests against the live Gemini API. Skipped by default;
// run with `go test -tags=manual -count=1 -run Gemini ./internal/llm/gemini/...`.
//
// API key is read from a file (default `.secrets/gemini`, override via
// PRISM_GEMINI_KEY_FILE) and is never logged. Test failures print
// non-secret diagnostics only (model id, token counts, output length).
//
// Set PRISM_GEMINI_RECORD=1 to capture each response into testdata/
// cassettes/<name>.json. Replay tests in gemini_replay_test.go consume
// those cassettes offline.
package gemini_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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

const (
	defaultGenerateModel = "gemini-2.5-flash"
	defaultEmbedModel    = "gemini-embedding-001"
	defaultKeyRelPath    = ".secrets/gemini"
)

// loadAPIKey returns the trimmed key without ever logging it.
// Resolution order:
//  1. PRISM_GEMINI_KEY_FILE env (path) — falls back to repo `.secrets/gemini`.
//  2. PRISM_GEMINI_KEY env (raw key).
//
// Skips the test (with a warning, no key material) if neither yields a value.
func loadAPIKey(t *testing.T) string {
	t.Helper()

	path := os.Getenv("PRISM_GEMINI_KEY_FILE")
	if path == "" {
		// Resolve repo root: package dir is internal/llm/gemini → up 3.
		wd, err := os.Getwd()
		require.NoError(t, err)
		path = filepath.Join(wd, "..", "..", "..", defaultKeyRelPath)
	}

	if raw, err := os.ReadFile(path); err == nil {
		if key := strings.TrimSpace(string(raw)); key != "" {
			return key
		}
	} else if !os.IsNotExist(err) {
		require.NoError(t, err)
	}

	if key := strings.TrimSpace(os.Getenv("PRISM_GEMINI_KEY")); key != "" {
		return key
	}

	t.Log("warning: gemini key not found via PRISM_GEMINI_KEY_FILE or PRISM_GEMINI_KEY; skipping live test")
	t.Skip("gemini key unavailable")
	return ""
}

// newRecordingProvider wires a Gemini provider whose http.Client persists
// the next response under the given cassette name. The cassette file is
// always overwritten when PRISM_GEMINI_RECORD=1.
func newRecordingProvider(t *testing.T, ctx context.Context, apiKey, cassetteName string) *gemini.Provider {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracer := noop.NewTracerProvider().Tracer("gemini-smoke")
	v := validator.New()
	m := mold.New()
	hc := &http.Client{
		Timeout: 60 * time.Second,
		Transport: &recordingTransport{
			t:    t,
			name: cassetteName,
			base: http.DefaultTransport,
		},
	}
	p, err := gemini.New(ctx, logger, tracer, v, m, hc,
		gemini.Config{
			APIKey:  apiKey,
			Timeout: 60 * time.Second,
		})
	require.NoError(t, err)
	return p
}

func generateModel() string {
	if v := os.Getenv("PRISM_GEMINI_GENERATE_MODEL"); v != "" {
		return v
	}
	return defaultGenerateModel
}

func embedModel() string {
	if v := os.Getenv("PRISM_GEMINI_EMBED_MODEL"); v != "" {
		return v
	}
	return defaultEmbedModel
}

func TestGeminiGenerate_Text(t *testing.T) {
	if !recordingEnabled() {
		t.Skip("set PRISM_GEMINI_RECORD=1 to call the live API and capture cassette")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	p := newRecordingProvider(t, ctx, loadAPIKey(t), "generate_text")

	// gemini-2.5-flash is a thinking model; a tight MaxTokens cap can leave
	// no budget for visible output. Either omit the cap or set it generous.
	resp, err := p.Generate(ctx, &llm.GenerateRequest{
		Model:             generateModel(),
		SystemInstruction: "Reply with a single short word.",
		Prompt:            "Say hi.",
		Temperature:       utils.Ptr(float32(0.0)),
		Format:            llm.ResponseFormatText,
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Text)
	require.Greater(t, resp.Usage.Total, 0)
	t.Logf("model=%s text_len=%d total_tokens=%d", resp.Model, len(resp.Text), resp.Usage.Total)
}

// TestGeminiGenerate_JSONSchema exercises structured output + schema
// validation + decode round-trip — the path the Planner / Analysis layers
// rely on in production.
func TestGeminiGenerate_JSONSchema(t *testing.T) {
	if !recordingEnabled() {
		t.Skip("set PRISM_GEMINI_RECORD=1 to call the live API and capture cassette")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	p := newRecordingProvider(t, ctx, loadAPIKey(t), "generate_jsonschema")

	type Greeting struct {
		Greeting string `json:"greeting"`
		Language string `json:"language"`
	}
	schema := pkgschema.NewSkeleton[Greeting]("greeting", 1)

	resp, err := p.Generate(ctx, &llm.GenerateRequest{
		Model:             generateModel(),
		SystemInstruction: "Return a short greeting and the ISO 639-1 language code.",
		Prompt:            "Greet someone in Traditional Chinese.",
		Temperature:       utils.Ptr(float32(0.0)),
		Format:            llm.ResponseFormatJsonSchema,
		JSONSchema:        schema,
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Text)

	var out Greeting
	require.NoError(t, resp.DecodeJSONSchema(&out))
	require.NotEmpty(t, out.Greeting)
	require.NotEmpty(t, out.Language)
	t.Logf("model=%s greeting_len=%d language=%s total_tokens=%d",
		resp.Model, len(out.Greeting), out.Language, resp.Usage.Total)
}

func TestGeminiEmbed(t *testing.T) {
	if !recordingEnabled() {
		t.Skip("set PRISM_GEMINI_RECORD=1 to call the live API and capture cassette")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	p := newRecordingProvider(t, ctx, loadAPIKey(t), "embed")

	resp, err := p.Embed(ctx, &llm.EmbedRequest{
		Model: embedModel(),
		Input: []string{"立法院今日通過國會改革法案"},
	})
	require.NoError(t, err)
	require.Len(t, resp.Vectors, 1)
	require.Greater(t, len(resp.Vectors[0]), 0)
	t.Logf("model=%s vectors=%d dim=%d", resp.Model, len(resp.Vectors), len(resp.Vectors[0]))
}

// TestGeminiUnauthorized records the response from a Generate call with
// an obviously invalid API key. The expectation is a non-2xx status that
// the provider surfaces as ErrGenAPIError. Replay tests assert the same
// classification offline.
func TestGeminiUnauthorized(t *testing.T) {
	if !recordingEnabled() {
		t.Skip("set PRISM_GEMINI_RECORD=1 to call the live API and capture cassette")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	// Bogus key: any non-empty string passes validation; the API returns
	// 4xx. Do not log this string back in case it ever holds a real key.
	p := newRecordingProvider(t, ctx, "INVALID_KEY_FOR_TEST", "unauthorized")

	_, err := p.Generate(ctx, &llm.GenerateRequest{
		Model:             generateModel(),
		SystemInstruction: "Reply with a single short word.",
		Prompt:            "Say hi.",
		Format:            llm.ResponseFormatText,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, llm.ErrGenAPIError)
}
