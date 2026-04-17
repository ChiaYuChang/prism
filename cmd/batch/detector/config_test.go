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

	assert.Equal(t, time.Minute, cfg.Interval)
	assert.False(t, cfg.Once)
	assert.Equal(t, int32(100), cfg.RecentLimit)
	assert.Equal(t, 8083, cfg.HealthPort)
	assert.Equal(t, "localhost", cfg.Postgres.Host)
	assert.Equal(t, 5432, cfg.Postgres.Port)
	assert.Equal(t, "disable", cfg.Postgres.SSLMode)
}

func TestLoadConfig_FromFlags(t *testing.T) {
	args := []string{
		"--interval=30s",
		"--once=true",
		"--recent-limit=50",
		"--health-port=9090",
		"--pg-host=127.0.0.1",
		"--pg-port=5433",
		"--pg-username=tester",
		"--pg-password=secret",
		"--pg-db=prismtest",
		"--log-level=debug",
	}

	cfg, err := LoadConfig(args)
	require.NoError(t, err)

	assert.Equal(t, 30*time.Second, cfg.Interval)
	assert.True(t, cfg.Once)
	assert.Equal(t, int32(50), cfg.RecentLimit)
	assert.Equal(t, 9090, cfg.HealthPort)
	assert.Equal(t, "127.0.0.1", cfg.Postgres.Host)
	assert.Equal(t, 5433, cfg.Postgres.Port)
	assert.Equal(t, "tester", cfg.Postgres.Username)
	assert.Equal(t, "secret", cfg.Postgres.Password)
	assert.Equal(t, "prismtest", cfg.Postgres.DB)
}

func TestLoadConfig_EnvironmentVariables(t *testing.T) {
	t.Setenv("PRISM_BATCH_DETECTOR_INTERVAL", "2m")
	t.Setenv("PRISM_BATCH_DETECTOR_POSTGRES_USERNAME", "envuser")
	t.Setenv("PRISM_BATCH_DETECTOR_RECENT_LIMIT", "75")

	cfg, err := LoadConfig([]string{})
	require.NoError(t, err)

	assert.Equal(t, 2*time.Minute, cfg.Interval)
	assert.Equal(t, "envuser", cfg.Postgres.Username)
	assert.Equal(t, int32(75), cfg.RecentLimit)
}

func TestLoadConfig_ValidationFailed(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"interval too short", []string{"--interval=1s"}},
		{"recent-limit zero", []string{"--recent-limit=0"}},
		{"recent-limit above max", []string{"--recent-limit=501"}},
		{"health-port below range", []string{"--health-port=80"}},
		{"health-port above range", []string{"--health-port=70000"}},
		{"pg-port above range", []string{"--pg-port=70000"}},
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
	// Guard against leaking env that would poison defaults test.
	for _, k := range []string{
		"PRISM_BATCH_DETECTOR_INTERVAL",
		"PRISM_BATCH_DETECTOR_POSTGRES_USERNAME",
		"PRISM_BATCH_DETECTOR_RECENT_LIMIT",
	} {
		_ = os.Unsetenv(k)
	}
	os.Exit(m.Run())
}
