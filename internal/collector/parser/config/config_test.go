package config_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

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

	reg, err := config.BuildRegistry(cfg, logger, tracer)
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

	reg, err := config.BuildRegistry(cfg, logger, tracer)
	require.NoError(t, err)

	// Composite parser is wired; smoke test that Parse returns without
	// "no matching parser" — exact extraction is the parser package's job.
	_, err = reg.Parse(context.Background(), "https://jsonld.example/", "<html></html>")
	require.NoError(t, err)
}

func TestBuildRegistry_NilLogger_PropagatesRegistryErr(t *testing.T) {
	cfg := config.Config{Parsers: map[string]config.ParserConfig{}}
	tracer := noop.NewTracerProvider().Tracer("test")

	_, err := config.BuildRegistry(cfg, nil, tracer)
	require.Error(t, err)
	assert.ErrorIs(t, err, parser.ErrParamMissing)
}

func TestBuildRegistry_EmptyConfig(t *testing.T) {
	cfg := config.Config{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracer := noop.NewTracerProvider().Tracer("test")

	reg, err := config.BuildRegistry(cfg, logger, tracer)
	require.NoError(t, err)

	_, err = reg.Parse(context.Background(), "https://anything.example/", "<html></html>")
	require.Error(t, err)
	assert.ErrorIs(t, err, parser.ErrNoMatchingParser)
}
