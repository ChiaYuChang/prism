package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig_Defaults(t *testing.T) {
	cfg, err := LoadConfig([]string{})
	require.NoError(t, err)

	assert.Equal(t, 8090, cfg.Port)
	assert.Equal(t, 10*time.Second, cfg.ReadTimeout)
	assert.Equal(t, 30*time.Second, cfg.WriteTimeout)
	assert.Equal(t, 10*time.Second, cfg.ShutdownTimeout)
	assert.Empty(t, cfg.CORSOrigins)
	assert.Equal(t, "localhost", cfg.Postgres.Host)
	assert.Equal(t, 5432, cfg.Postgres.Port)
	assert.Equal(t, "info", cfg.Logger.Level)
}

func TestLoadConfig_ShippedConfig(t *testing.T) {
	cfg, err := LoadConfig([]string{"--config", filepath.Join("..", "..", "configs", "api-server", "config.yaml")})
	require.NoError(t, err)

	assert.Equal(t, 8090, cfg.Port)
	assert.Equal(t, "postgres", cfg.Postgres.Host)
	assert.Equal(t, "valkey", cfg.Valkey.Host)
	assert.True(t, cfg.Cache.Enabled)
	assert.True(t, cfg.RateLimit.Enabled)
	assert.Equal(t, "prism.api", cfg.Telemetry.ServiceName)
}

func TestLoadConfig_FromFlags(t *testing.T) {
	args := []string{
		"--port=9000",
		"--read-timeout=5s",
		"--write-timeout=15s",
		"--shutdown-timeout=20s",
		"--cors-origins=https://a.example,https://b.example",
		"--pg-host=10.0.0.1",
		"--pg-username=u",
		"--pg-password=p",
		"--log-level=debug",
	}

	cfg, err := LoadConfig(args)
	require.NoError(t, err)

	assert.Equal(t, 9000, cfg.Port)
	assert.Equal(t, 5*time.Second, cfg.ReadTimeout)
	assert.Equal(t, 15*time.Second, cfg.WriteTimeout)
	assert.Equal(t, 20*time.Second, cfg.ShutdownTimeout)
	assert.Equal(t, []string{"https://a.example", "https://b.example"}, cfg.CORSOrigins)
	assert.Equal(t, "10.0.0.1", cfg.Postgres.Host)
	assert.Equal(t, "debug", cfg.Logger.Level)
}

func TestLoadConfig_AuthTokenFlagsAndFile(t *testing.T) {
	tokenFile := writeTempFile(t, "file-token-a\n\n file-token-b \n")

	cfg, err := LoadConfig([]string{
		"--auth-token=inline-token-a,inline-token-b",
		"--auth-token-file=" + tokenFile,
	})
	require.NoError(t, err)

	tokens, err := cfg.Auth.Token.TokenSet()
	require.NoError(t, err)
	assert.Contains(t, tokens, "inline-token-a")
	assert.Contains(t, tokens, "inline-token-b")
	assert.Contains(t, tokens, "file-token-a")
	assert.Contains(t, tokens, "file-token-b")
	assert.Len(t, tokens, 4)
}

func TestLoadConfig_AuthTokenConfigFile(t *testing.T) {
	tokenFile := writeTempFile(t, "file-token\n")
	configFile := writeTempFile(t, "auth:\n  token:\n    tokens:\n      - config-token\n    file: "+tokenFile+"\n")

	cfg, err := LoadConfig([]string{"--config=" + configFile})
	require.NoError(t, err)

	tokens, err := cfg.Auth.Token.TokenSet()
	require.NoError(t, err)
	assert.Contains(t, tokens, "config-token")
	assert.Contains(t, tokens, "file-token")
	assert.Len(t, tokens, 2)
}

func TestTokenAuthConfig_TokenSetNotConfigured(t *testing.T) {
	tokens, err := TokenAuthConfig{}.TokenSet()
	require.NoError(t, err)
	assert.Nil(t, tokens)
}

func TestTokenAuthConfig_TokenSetMissingFile(t *testing.T) {
	_, err := (TokenAuthConfig{File: "missing-token-file"}).TokenSet()
	require.Error(t, err)
}

func TestTokenAuthConfig_TokenSetEmptyConfiguredFile(t *testing.T) {
	_, err := (TokenAuthConfig{File: writeTempFile(t, "\n\t\n")}).TokenSet()
	require.Error(t, err)
}

func TestLoadConfig_TelemetryFlags(t *testing.T) {
	cfg, err := LoadConfig([]string{
		"--otel-enabled",
		"--otel-service-name=prism.api.test",
		"--otel-service-version=dev",
		"--otel-environment=test",
		"--otel-endpoint=collector:4317",
		"--otel-sample-ratio=0.25",
		"--otel-timeout=3s",
	})
	require.NoError(t, err)

	assert.True(t, cfg.Telemetry.Enabled)
	assert.Equal(t, "prism.api.test", cfg.Telemetry.ServiceName)
	assert.Equal(t, "dev", cfg.Telemetry.ServiceVersion)
	assert.Equal(t, "test", cfg.Telemetry.Environment)
	assert.Equal(t, "collector:4317", cfg.Telemetry.Endpoint)
	assert.Equal(t, 0.25, cfg.Telemetry.SampleRatio)
	assert.Equal(t, 3*time.Second, cfg.Telemetry.Timeout)
}

func TestLoadConfig_EnvironmentVariables(t *testing.T) {
	t.Setenv("PRISM_API_PORT", "9100")
	t.Setenv("PRISM_API_POSTGRES_USERNAME", "envuser")
	t.Setenv("PRISM_API_LOGGER_LEVEL", "warn")

	cfg, err := LoadConfig([]string{})
	require.NoError(t, err)

	assert.Equal(t, 9100, cfg.Port)
	assert.Equal(t, "envuser", cfg.Postgres.Username)
	assert.Equal(t, "warn", cfg.Logger.Level)
}

func TestLoadConfig_ValidationFailed(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"port below range", []string{"--port=80"}},
		{"port above range", []string{"--port=70000"}},
		{"read-timeout too short", []string{"--read-timeout=0s"}},
		{"invalid log-level", []string{"--log-level=verbose"}},
		{"invalid pg-sslmode", []string{"--pg-sslmode=bogus"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadConfig(tt.args)
			assert.Error(t, err)
		})
	}
}

func TestLoadConfig_UnknownFlag(t *testing.T) {
	_, err := LoadConfig([]string{"--does-not-exist=1"})
	require.Error(t, err)
}

func TestMain(m *testing.M) {
	for _, k := range []string{
		"PRISM_API_PORT",
		"PRISM_API_POSTGRES_USERNAME",
		"PRISM_API_LOGGER_LEVEL",
	} {
		_ = os.Unsetenv(k)
	}
	os.Exit(m.Run())
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "config-*.yaml")
	require.NoError(t, err)
	_, err = f.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}
