package config_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/ChiaYuChang/prism/internal/collector"
	"github.com/ChiaYuChang/prism/internal/collector/parser"
	"github.com/ChiaYuChang/prism/internal/collector/parser/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

func writeTempYAML(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "parsers.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func TestLoadConfig_Success(t *testing.T) {
	body := `
version: 1
parsers:
  example.com:
    enabled: true
    jsonld: false
    date_layouts:
      - "2006-01-02"
    html:
      title:
        - "h1"
`
	cfg, err := config.LoadConfig(writeTempYAML(t, body))
	require.NoError(t, err)
	assert.Equal(t, 1, cfg.Version)
	require.Contains(t, cfg.Parsers, "example.com")

	pc := cfg.Parsers["example.com"]
	require.NotNil(t, pc.Enabled)
	assert.True(t, *pc.Enabled)
	assert.False(t, pc.JSONLD)
	assert.Equal(t, []string{"2006-01-02"}, pc.DateLayouts)
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := config.LoadConfig(filepath.Join(t.TempDir(), "missing.yaml"))
	require.Error(t, err)
	assert.ErrorContains(t, err, "open parser config")
}

func TestLoadConfig_MalformedYAML(t *testing.T) {
	body := "version: 1\nparsers: [not-a-map\n" // unclosed bracket
	_, err := config.LoadConfig(writeTempYAML(t, body))
	require.Error(t, err)
	assert.ErrorContains(t, err, "decode parser config")
}

func TestBuildRegistry_DisabledHostSkipped(t *testing.T) {
	disabled := false
	enabled := true
	cfg := config.Config{
		Parsers: map[string]config.ParserConfig{
			"on.example":  {Enabled: &enabled},
			"off.example": {Enabled: &disabled},
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracer := noop.NewTracerProvider().Tracer("test")

	reg, err := config.BuildRegistry(cfg, logger, tracer, nil)
	require.NoError(t, err)

	// Routing probe: enabled host parses; disabled host returns ErrNoMatchingParser.
	_, err = reg.Parse(context.Background(), "https://on.example/", "<html></html>")
	require.NoError(t, err)

	_, err = reg.Parse(context.Background(), "https://off.example/", "<html></html>")
	require.Error(t, err)
	assert.ErrorIs(t, err, parser.ErrNoMatchingParser)
}

func TestBuildRegistry_JSONLDComposite(t *testing.T) {
	cfg := config.Config{
		Parsers: map[string]config.ParserConfig{
			"jsonld.example": {JSONLD: true},
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracer := noop.NewTracerProvider().Tracer("test")

	reg, err := config.BuildRegistry(cfg, logger, tracer, nil)
	require.NoError(t, err)

	// Composite parser is wired; smoke test that Parse returns without
	// "no matching parser" — exact extraction is the parser package's job.
	_, err = reg.Parse(context.Background(), "https://jsonld.example/", "<html></html>")
	require.NoError(t, err)
}

func TestBuildRegistry_NilLogger_PropagatesRegistryErr(t *testing.T) {
	cfg := config.Config{Parsers: map[string]config.ParserConfig{}}
	tracer := noop.NewTracerProvider().Tracer("test")

	_, err := config.BuildRegistry(cfg, nil, tracer, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, parser.ErrParamMissing)
}

func TestBuildRegistry_EmptyConfig(t *testing.T) {
	cfg := config.Config{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracer := noop.NewTracerProvider().Tracer("test")

	reg, err := config.BuildRegistry(cfg, logger, tracer, nil)
	require.NoError(t, err)

	_, err = reg.Parse(context.Background(), "https://anything.example/", "<html></html>")
	require.Error(t, err)
	assert.ErrorIs(t, err, parser.ErrNoMatchingParser)
}

// stubParser is a tiny collector.Parser used only to verify fallback wiring
// without pulling in the parser/llm package and its LLM dependency.
type stubParser struct{ marker string }

func (s *stubParser) Parse(_ context.Context, url string, _ string) (*collector.Article, error) {
	return &collector.Article{URL: url, Title: s.marker}, nil
}

func TestBuildRegistry_FallbackEnabled_UsesFactory(t *testing.T) {
	cfg := config.Config{
		Fallback: config.FallbackConfig{Enable: true},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracer := noop.NewTracerProvider().Tracer("test")

	factory := func() (collector.Parser, error) {
		return &stubParser{marker: "from-fallback"}, nil
	}

	reg, err := config.BuildRegistry(cfg, logger, tracer, factory)
	require.NoError(t, err)

	got, err := reg.Parse(context.Background(), "https://anything.example/x", "<html></html>")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "from-fallback", got.Title)
}

func TestBuildRegistry_FallbackEnabled_NilFactory_Errors(t *testing.T) {
	cfg := config.Config{
		Fallback: config.FallbackConfig{Enable: true},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracer := noop.NewTracerProvider().Tracer("test")

	_, err := config.BuildRegistry(cfg, logger, tracer, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, config.ErrFallbackEnabledNoFactory)
}

func TestBuildRegistry_FallbackEnabled_FactoryError_Propagates(t *testing.T) {
	cfg := config.Config{
		Fallback: config.FallbackConfig{Enable: true},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracer := noop.NewTracerProvider().Tracer("test")

	sentinel := errors.New("provider unavailable")
	factory := func() (collector.Parser, error) { return nil, sentinel }

	_, err := config.BuildRegistry(cfg, logger, tracer, factory)
	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel)
}

func TestLoadConfig_FallbackEnabled_ResolvesKeyFile(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "llm-key")
	require.NoError(t, os.WriteFile(keyPath, []byte("secret-from-file"), 0o600))
	promptPath := filepath.Join(dir, "prompt.md")
	require.NoError(t, os.WriteFile(promptPath, []byte("you are a parser"), 0o600))

	body := `
version: 1
fallback:
  enable: true
  prompt_file: ` + promptPath + `
  llm:
    provider: gemini
    model: gemini-2.0-flash
    key_file: ` + keyPath + `
parsers: {}
`
	cfg, err := config.LoadConfig(writeTempYAML(t, body))
	require.NoError(t, err)
	assert.True(t, cfg.Fallback.Enable)
	assert.Equal(t, "gemini", cfg.Fallback.LLM.Provider)
	assert.Equal(t, "gemini-2.0-flash", cfg.Fallback.LLM.Model)
	assert.Equal(t, "secret-from-file", cfg.Fallback.LLM.Key,
		"ResolveSecrets should override Key with the contents of KeyFile")
	assert.Equal(t, promptPath, cfg.Fallback.PromptFile)
}

func TestLoadConfig_FallbackEnabled_MissingPromptFile(t *testing.T) {
	body := `
version: 1
fallback:
  enable: true
  llm:
    provider: gemini
    model: gemini-2.0-flash
    key: inline-key
parsers: {}
`
	_, err := config.LoadConfig(writeTempYAML(t, body))
	require.Error(t, err)
	assert.ErrorContains(t, err, "prompt_file is required")
}

func TestLoadConfig_FallbackEnabled_MissingProvider_FailsValidation(t *testing.T) {
	dir := t.TempDir()
	promptPath := filepath.Join(dir, "prompt.md")
	require.NoError(t, os.WriteFile(promptPath, []byte("p"), 0o600))

	body := `
version: 1
fallback:
  enable: true
  prompt_file: ` + promptPath + `
  llm:
    model: some-model
    key: inline-key
parsers: {}
`
	_, err := config.LoadConfig(writeTempYAML(t, body))
	require.Error(t, err)
	assert.ErrorContains(t, err, "fallback llm config invalid")
}

func TestLoadFallbackPrompt_TrimsTrailingWhitespace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prompt.md")
	require.NoError(t, os.WriteFile(path, []byte("   you are a parser\n\n"), 0o600))

	got, err := config.LoadFallbackPrompt(config.FallbackConfig{PromptFile: path})
	require.NoError(t, err)
	assert.Equal(t, "   you are a parser", got)
}

func TestLoadFallbackPrompt_EmptyPath_Errors(t *testing.T) {
	_, err := config.LoadFallbackPrompt(config.FallbackConfig{})
	require.Error(t, err)
	assert.ErrorContains(t, err, "empty")
}

func TestLoadFallbackPrompt_MissingFile_Errors(t *testing.T) {
	_, err := config.LoadFallbackPrompt(config.FallbackConfig{
		PromptFile: filepath.Join(t.TempDir(), "missing.md"),
	})
	require.Error(t, err)
}

func TestLoadConfig_FallbackDisabled_SkipsLLMValidation(t *testing.T) {
	// Empty LLM block must NOT error when fallback is disabled — the LLM
	// fields are only required when fallback.enable=true.
	body := `
version: 1
fallback:
  enable: false
parsers: {}
`
	cfg, err := config.LoadConfig(writeTempYAML(t, body))
	require.NoError(t, err)
	assert.False(t, cfg.Fallback.Enable)
}
