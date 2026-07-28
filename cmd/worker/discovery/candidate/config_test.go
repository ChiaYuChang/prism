package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	app "github.com/ChiaYuChang/prism/internal/appconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := LoadConfig([]string{})
	require.NoError(t, err)

	assert.Equal(t, 8092, cfg.Health.Port)
	assert.Equal(t, DefaultScoutConfigPath, cfg.Scout.ConfigPath)
	assert.Equal(t, 30*time.Second, cfg.Scout.HTTPTimeout)
	assert.Equal(t, 3, cfg.RetryMax)
	assert.Equal(t, "localhost", cfg.Postgres.Host)
	assert.Equal(t, "nats", cfg.MessengerType)
	require.NotNil(t, cfg.Messenger)
}

func TestLoadConfigShippedConfig(t *testing.T) {
	setShippedConfigEnv(t)

	cfg, err := LoadConfig([]string{"--config", filepath.Join("..", "..", "..", "..", "configs", "worker", "discovery", "candidate", "config.yaml")})
	require.NoError(t, err)

	assert.Equal(t, 8092, cfg.Health.Port)
	assert.Equal(t, "/app/configs/worker/discovery/candidate/scouts.yaml", cfg.Scout.ConfigPath)
	assert.Equal(t, 3, cfg.RetryMax)
	assert.Equal(t, "postgres", cfg.Postgres.Host)
	natsCfg, ok := cfg.Messenger.(*app.NatsConfig)
	require.True(t, ok)
	require.Equal(t, "prism_task", natsCfg.Stream)
	require.Equal(t, "discovery-worker", natsCfg.Consumer)
	require.NotNil(t, natsCfg.AutoProvision)
	require.False(t, *natsCfg.AutoProvision)
	assert.Equal(t, "zh-hant", cfg.Search.Provider.Brave.SearchLang)
	assert.Equal(t, "prism.discovery", cfg.Telemetry.ServiceName)
	assert.Equal(t, "/logs/app.log", cfg.Logger.File.File)
}

func setShippedConfigEnv(t *testing.T) {
	t.Helper()
	t.Setenv("POSTGRES_HOST", "postgres")
	t.Setenv("POSTGRES_PORT", "5432")
	t.Setenv("POSTGRES_APP_USER", "prism")
	t.Setenv("POSTGRES_APP_DB", "prism")
	t.Setenv("FIXTURE_BASE", "")
	t.Setenv("PRISM_WORKER_OTEL_ENABLED", "true")
	t.Setenv("OTEL_COLLECTOR_ENDPOINT", "otel-collector:4317")
}

func TestLoadConfigSearchProvidersFromYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := []byte(`
search:
  provider:
    brave:
      enable: true
      api_key: brave-key
      count: 7
    google-cse:
      enable: true
      api_key: google-key
      cx: cx-id
      count: 5
`)
	require.NoError(t, os.WriteFile(path, body, 0600))

	cfg, err := LoadConfig([]string{"--config", path})
	require.NoError(t, err)
	require.True(t, cfg.Search.Provider.Brave.Enable)
	require.Equal(t, "brave-key", cfg.Search.Provider.Brave.APIKey)
	require.Equal(t, 7, cfg.Search.Provider.Brave.Count)
	require.True(t, cfg.Search.Provider.GoogleCSE.Enable)
	require.Equal(t, "google-key", cfg.Search.Provider.GoogleCSE.APIKey)
	require.Equal(t, "cx-id", cfg.Search.Provider.GoogleCSE.CX)
	require.Equal(t, 5, cfg.Search.Provider.GoogleCSE.Count)
}

func TestLoadConfigSearchProvidersFromJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := []byte(`{
  "search": {
    "provider": {
      "brave": {"enable": true, "api_key": "brave-key", "count": 8},
      "google-cse": {"enable": true, "api_key": "google-key", "cx": "cx-id", "count": 6}
    }
  }
}`)
	require.NoError(t, os.WriteFile(path, body, 0600))

	cfg, err := LoadConfig([]string{"--config", path})
	require.NoError(t, err)
	require.True(t, cfg.Search.Provider.Brave.Enable)
	require.Equal(t, 8, cfg.Search.Provider.Brave.Count)
	require.True(t, cfg.Search.Provider.GoogleCSE.Enable)
	require.Equal(t, 6, cfg.Search.Provider.GoogleCSE.Count)
}

func TestLoadConfigFromFlags(t *testing.T) {
	cfg, err := LoadConfig([]string{
		"--health-port=9091",
		"--http-timeout=45s",
		"--retry-max=2",
		"--scout-config=/tmp/scouts.yaml",
		"--pg-host=127.0.0.1",
		"--pg-port=5433",
		"--messenger-type=gochannel",
	})
	require.NoError(t, err)

	assert.Equal(t, 9091, cfg.Health.Port)
	assert.Equal(t, 45*time.Second, cfg.Scout.HTTPTimeout)
	assert.Equal(t, 2, cfg.RetryMax)
	assert.Equal(t, "/tmp/scouts.yaml", cfg.Scout.ConfigPath)
	assert.Equal(t, "127.0.0.1", cfg.Postgres.Host)
	assert.Equal(t, 5433, cfg.Postgres.Port)
	assert.Equal(t, "gochannel", cfg.MessengerType)
}

func TestLoadConfigTelemetryFlags(t *testing.T) {
	cfg, err := LoadConfig([]string{
		"--otel-enabled",
		"--otel-service-name=prism.discovery.test",
		"--otel-service-version=dev",
		"--otel-environment=test",
		"--otel-endpoint=collector:4317",
		"--otel-sample-ratio=0.25",
		"--otel-timeout=3s",
		"--messenger-type=gochannel",
	})
	require.NoError(t, err)

	assert.True(t, cfg.Telemetry.Enabled)
	assert.Equal(t, "prism.discovery.test", cfg.Telemetry.ServiceName)
	assert.Equal(t, "dev", cfg.Telemetry.ServiceVersion)
	assert.Equal(t, "test", cfg.Telemetry.Environment)
	assert.Equal(t, "collector:4317", cfg.Telemetry.Endpoint)
	assert.Equal(t, 0.25, cfg.Telemetry.SampleRatio)
	assert.Equal(t, 3*time.Second, cfg.Telemetry.Timeout)
}

func TestLoadConfigFromEnvironment(t *testing.T) {
	require.NoError(t, os.Setenv("PRISM_DISCOVERY_WORKER_SCOUT_HTTP_TIMEOUT", "40s"))
	require.NoError(t, os.Setenv("PRISM_DISCOVERY_WORKER_POSTGRES_USERNAME", "tester"))
	defer func() {
		_ = os.Unsetenv("PRISM_DISCOVERY_WORKER_SCOUT_HTTP_TIMEOUT")
		_ = os.Unsetenv("PRISM_DISCOVERY_WORKER_POSTGRES_USERNAME")
	}()

	cfg, err := LoadConfig([]string{})
	require.NoError(t, err)

	assert.Equal(t, 40*time.Second, cfg.Scout.HTTPTimeout)
	assert.Equal(t, "tester", cfg.Postgres.Username)
}

func TestLoadConfigValidationFailed(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "health-port too low", args: []string{"--health-port=1"}},
		{name: "http-timeout too short", args: []string{"--http-timeout=0s"}},
		{name: "invalid messenger type", args: []string{"--messenger-type=invalid"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadConfig(tt.args)
			assert.Error(t, err)
		})
	}
}
