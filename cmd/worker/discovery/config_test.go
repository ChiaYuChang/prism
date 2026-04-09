package main

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := LoadConfig([]string{})
	require.NoError(t, err)

	assert.Equal(t, 8081, cfg.HealthPort)
	assert.Equal(t, DefaultScoutConfigPath, cfg.ScoutConfigPath)
	assert.Equal(t, 30*time.Second, cfg.HTTPTimeout)
	assert.Equal(t, "localhost", cfg.Postgres.Host)
	assert.Equal(t, "nats", cfg.MessengerType)
	require.NotNil(t, cfg.Messenger)
}

func TestLoadConfigFromFlags(t *testing.T) {
	cfg, err := LoadConfig([]string{
		"--health-port=9091",
		"--http-timeout=45s",
		"--scout-config=/tmp/scouts.yaml",
		"--pg-host=127.0.0.1",
		"--pg-port=5433",
		"--messenger-type=gochannel",
	})
	require.NoError(t, err)

	assert.Equal(t, 9091, cfg.HealthPort)
	assert.Equal(t, 45*time.Second, cfg.HTTPTimeout)
	assert.Equal(t, "/tmp/scouts.yaml", cfg.ScoutConfigPath)
	assert.Equal(t, "127.0.0.1", cfg.Postgres.Host)
	assert.Equal(t, 5433, cfg.Postgres.Port)
	assert.Equal(t, "gochannel", cfg.MessengerType)
}

func TestLoadConfigFromEnvironment(t *testing.T) {
	require.NoError(t, os.Setenv("PRISM_DISCOVERY_WORKER_HTTP_TIMEOUT", "40s"))
	require.NoError(t, os.Setenv("PRISM_DISCOVERY_WORKER_POSTGRES_USERNAME", "tester"))
	defer func() {
		_ = os.Unsetenv("PRISM_DISCOVERY_WORKER_HTTP_TIMEOUT")
		_ = os.Unsetenv("PRISM_DISCOVERY_WORKER_POSTGRES_USERNAME")
	}()

	cfg, err := LoadConfig([]string{})
	require.NoError(t, err)

	assert.Equal(t, 40*time.Second, cfg.HTTPTimeout)
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
