package main

import (
	"os"
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
