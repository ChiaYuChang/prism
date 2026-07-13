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
	providerName, err := cfg.LLM.ProviderName()
	require.NoError(t, err)
	require.Equal(t, "ollama", providerName)
	require.Equal(t, "glm-5.2:cloud", cfg.LLM.Model)
	providerCfg, err := cfg.LLM.ProviderConfig()
	require.NoError(t, err)
	require.Equal(t, "http://host.docker.internal:11434", providerCfg["base_url"])
	require.Equal(t, "prism.planner", cfg.Telemetry.ServiceName)
}

func setShippedConfigEnv(t *testing.T) {
	t.Helper()
	t.Setenv("POSTGRES_HOST", "postgres")
	t.Setenv("POSTGRES_PORT", "5432")
	t.Setenv("POSTGRES_APP_USER", "prism")
	t.Setenv("POSTGRES_APP_DB", "prism")
	t.Setenv("PRISM_PLANNER_LLM_MODEL", "glm-5.2:cloud")
	t.Setenv("PRISM_PLANNER_SEARCH_TARGET_YAHOO_ENABLE", "true")
	t.Setenv("PRISM_WORKER_OTEL_ENABLED", "true")
	t.Setenv("OTEL_COLLECTOR_ENDPOINT", "otel-collector:4317")
}

func TestLoadConfigSearchTargetsFromYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := []byte(`
llm:
  provider:
    ollama:
      base_url: http://ollama:11434
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
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
llm:
  provider:
    ollama:
      base_url: http://ollama:11434
  model: ollama-test
`), 0o600))

	cfg, err := LoadConfig([]string{
		"--config", path,
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

func TestLoadConfigRequiresLLMProvider(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
llm:
  model: model-test
`), 0o600))

	_, err := LoadConfig([]string{"--config", path})
	require.Error(t, err)
}

func TestLoadConfigOpencodeProviderConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := []byte(`
llm:
  provider:
    opencode:
      base_url: http://opencode:4096
      agent: general
      directory: /workspace
      username: opencode
      password_file: /run/secrets/opencode
  model: opencode/deepseek-v4-flash-free
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

	providerName, err := cfg.LLM.ProviderName()
	require.NoError(t, err)
	require.Equal(t, "opencode", providerName)
	require.Equal(t, "opencode/deepseek-v4-flash-free", cfg.LLM.Model)
	providerCfg, err := cfg.LLM.ProviderConfig()
	require.NoError(t, err)
	require.Equal(t, "http://opencode:4096", providerCfg["base_url"])
	require.Equal(t, "general", providerCfg["agent"])
	require.Equal(t, "/workspace", providerCfg["directory"])
	require.Equal(t, "opencode", providerCfg["username"])
	require.Equal(t, "/run/secrets/opencode", providerCfg["password_file"])
}

func TestLoadConfigSearchTargetsFromJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := []byte(`{
   "llm": {"provider": {"ollama": {"base_url": "http://ollama:11434"}}, "model": "ollama-test"},
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
