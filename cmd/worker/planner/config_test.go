package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoadConfigShippedConfig(t *testing.T) {
	setShippedConfigEnv(t)

	cfg, err := LoadConfig([]string{"--config", filepath.Join("..", "..", "..", "configs", "worker", "planner", "config.yaml")})
	require.NoError(t, err)

	require.Equal(t, 8094, cfg.HealthPort)
	require.Equal(t, "/app/assets/worker/planner/prompts/analysis/extractor.md", cfg.PromptPath)
	require.Equal(t, "postgres", cfg.Postgres.Host)
	require.Equal(t, "gemini", cfg.LLM.Provider)
	require.Equal(t, "gemini-2.0-flash", cfg.LLM.Model)
	require.Equal(t, "prism.planner", cfg.Telemetry.ServiceName)
}

func setShippedConfigEnv(t *testing.T) {
	t.Helper()
	t.Setenv("POSTGRES_HOST", "postgres")
	t.Setenv("POSTGRES_PORT", "5432")
	t.Setenv("POSTGRES_APP_USER", "prism")
	t.Setenv("POSTGRES_APP_DB", "prism")
	t.Setenv("PRISM_PLANNER_LLM_PROVIDER", "gemini")
	t.Setenv("PRISM_PLANNER_LLM_MODEL", "gemini-2.0-flash")
	t.Setenv("PRISM_PLANNER_SEARCH_TARGET_YAHOO_ENABLE", "true")
	t.Setenv("PRISM_WORKER_OTEL_ENABLED", "true")
	t.Setenv("OTEL_COLLECTOR_ENDPOINT", "otel-collector:4317")
}

func TestLoadConfigSearchTargetsFromYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := []byte(`
llm:
  model: gemini-test
search:
  targets:
    yahoo:
      enable: true
      source_abbr: yahoo
      url: https://tw.news.yahoo.com
      site: tw.news.yahoo.com
`)
	require.NoError(t, os.WriteFile(path, body, 0600))

	cfg, err := LoadConfig([]string{"--config", path})
	require.NoError(t, err)
	targets := cfg.Search.EnabledTargets()
	require.Len(t, targets, 1)
	require.Equal(t, "yahoo", targets[0].SourceAbbr)
	require.Equal(t, "tw.news.yahoo.com", targets[0].Site)
}

func TestLoadConfigTelemetryFlags(t *testing.T) {
	cfg, err := LoadConfig([]string{
		"--llm-model=gemini-test",
		"--otel-enabled",
		"--otel-service-version=dev",
		"--otel-environment=test",
		"--otel-endpoint=collector:4317",
		"--otel-sample-ratio=0.5",
		"--otel-headers=authorization=masked-value",
		"--otel-timeout=3s",
	})
	require.NoError(t, err)

	require.True(t, cfg.Telemetry.Enabled)
	require.Equal(t, "prism.worker.planner", cfg.Telemetry.ServiceName)
	require.Equal(t, "dev", cfg.Telemetry.ServiceVersion)
	require.Equal(t, "test", cfg.Telemetry.Environment)
	require.Equal(t, "collector:4317", cfg.Telemetry.Endpoint)
	require.Equal(t, 0.5, cfg.Telemetry.SampleRatio)
	require.Equal(t, "masked-value", cfg.Telemetry.Headers["authorization"])
	require.Equal(t, 3*time.Second, cfg.Telemetry.Timeout)
}

func TestLoadConfigSearchTargetsFromJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := []byte(`{
  "llm": {"model": "gemini-test"},
  "search": {
    "targets": {
      "yahoo": {"enable": true, "source_abbr": "yahoo", "url": "https://tw.news.yahoo.com", "site": "tw.news.yahoo.com"}
    }
  }
}`)
	require.NoError(t, os.WriteFile(path, body, 0600))

	cfg, err := LoadConfig([]string{"--config", path})
	require.NoError(t, err)
	targets := cfg.Search.EnabledTargets()
	require.Len(t, targets, 1)
	require.Equal(t, "https://tw.news.yahoo.com", targets[0].URL)
}
