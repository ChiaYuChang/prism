package main

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig_Defaults(t *testing.T) {
	config, err := LoadConfig([]string{})
	require.NoError(t, err)

	assert.Equal(t, 10*time.Minute, config.Interval)
	assert.Equal(t, "localhost", config.Postgres.Host)
	assert.Equal(t, "nats", config.MessengerType)
}

func TestLoadConfig_FromFlags(t *testing.T) {
	args := []string{
		"--interval=5m",
		"--host=127.0.0.1",
		"--port=5433",
		"--messenger-type=gochannel",
	}

	config, err := LoadConfig(args)
	require.NoError(t, err)

	assert.Equal(t, 5*time.Minute, config.Interval)
	assert.Equal(t, "127.0.0.1", config.Postgres.Host)
	assert.Equal(t, 5433, config.Postgres.Port)
	assert.Equal(t, "gochannel", config.MessengerType)
}

func TestLoadConfig_EnvironmentVariables(t *testing.T) {
	err := os.Setenv("PRISM_SCHEDULER_INTERVAL", "15m")
	require.NoError(t, err)
	err = os.Setenv("PRISM_SCHEDULER_USER", "tester")
	require.NoError(t, err)

	defer func() {
		err := os.Unsetenv("PRISM_SCHEDULER_INTERVAL")
		assert.NoError(t, err)
		err = os.Unsetenv("PRISM_SCHEDULER_USER")
		assert.NoError(t, err)
	}()

	config, err := LoadConfig([]string{})
	require.NoError(t, err)

	assert.Equal(t, 15*time.Minute, config.Interval)
	assert.Equal(t, "tester", config.Postgres.User)
}

func TestPostgresConfig_ConnString(t *testing.T) {
	cfg := PostgresConfig{
		Host:     "db.local",
		Port:     5432,
		User:     "user",
		Password: "password",
		DBName:   "prism",
		SSLMode:  "require",
	}

	expected := "postgres://user:password@db.local:5432/prism?sslmode=require"
	assert.Equal(t, expected, cfg.ConnString())
}

func TestLoadConfig_ValidationFailed(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "interval too short",
			args: []string{"--interval=30s"},
		},
		{
			name: "invalid messenger type",
			args: []string{"--messenger-type=invalid"},
		},
		{
			name: "invalid port (high)",
			args: []string{"--port=70000"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadConfig(tt.args)
			assert.Error(t, err)
		})
	}
}
