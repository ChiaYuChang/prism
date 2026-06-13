package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/appconfig"
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

func TestLoadConfig_ShippedConfigs(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		wantHealth int
		wantKinds  []string
		wantSvc    string
	}{
		{
			name:       "slow",
			path:       filepath.Join("..", "..", "configs", "scheduler", "slow.yaml"),
			wantHealth: 8090,
			wantKinds:  []string{"DIRECTORY_FETCH", "KEYWORD_SEARCH"},
			wantSvc:    "prism.scheduler.slow",
		},
		{
			name:       "fast",
			path:       filepath.Join("..", "..", "configs", "scheduler", "fast.yaml"),
			wantHealth: 8091,
			wantKinds:  []string{"PAGE_FETCH"},
			wantSvc:    "prism.scheduler.fast",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := LoadConfig([]string{"--config", tt.path})
			require.NoError(t, err)
			assert.Equal(t, tt.wantHealth, cfg.HealthPort)
			assert.Equal(t, tt.wantKinds, cfg.Kinds)
			assert.Equal(t, "postgres", cfg.Postgres.Host)
			assert.Equal(t, "valkey", cfg.Valkey.Host)
			assert.Equal(t, tt.wantSvc, cfg.Telemetry.ServiceName)
		})
	}
}

func TestLoadConfig_FromFlags(t *testing.T) {
	args := []string{
		"--interval=5m",
		"--pg-host=127.0.0.1",
		"--pg-port=5433",
		"--valkey-username=prism",
		"--valkey-password=secret",
		"--messenger-type=gochannel",
	}

	config, err := LoadConfig(args)
	require.NoError(t, err)

	assert.Equal(t, 5*time.Minute, config.Interval)
	assert.Equal(t, "127.0.0.1", config.Postgres.Host)
	assert.Equal(t, 5433, config.Postgres.Port)
	assert.Equal(t, "prism", config.Valkey.Username)
	assert.Equal(t, "secret", config.Valkey.Password)
	assert.Equal(t, 0, config.Valkey.DB)
	assert.Equal(t, "gochannel", config.MessengerType)
}

func TestLoadConfig_LoggerFlags(t *testing.T) {
	config, err := LoadConfig([]string{
		"--log-level=debug",
		"--log-path=/tmp/prism-scheduler.log",
		"--log-console-enable=false",
		"--log-otel-enable",
		"--log-otel-url=collector:4317",
		"--messenger-type=gochannel",
	})
	require.NoError(t, err)

	assert.Equal(t, "debug", config.Logger.Level)
	assert.False(t, config.Logger.Console.Enable)
	assert.True(t, config.Logger.File.Enable)
	assert.Equal(t, "/tmp/prism-scheduler.log", config.Logger.File.File)
	assert.True(t, config.Logger.OTEL.Enable)
	assert.Equal(t, "collector:4317", config.Logger.OTEL.URL)
	assert.Equal(t, "prism.scheduler", config.Logger.OTEL.ServiceName)
}

func TestLoadConfig_TelemetryFlags(t *testing.T) {
	config, err := LoadConfig([]string{
		"--otel-enabled",
		"--otel-service-version=dev",
		"--otel-environment=test",
		"--otel-endpoint=collector:4317",
		"--otel-sample-ratio=0.25",
		"--otel-headers=authorization=masked-value",
		"--otel-timeout=3s",
		"--messenger-type=gochannel",
	})
	require.NoError(t, err)

	assert.True(t, config.Telemetry.Enabled)
	assert.Equal(t, "prism.scheduler", config.Telemetry.ServiceName)
	assert.Equal(t, "dev", config.Telemetry.ServiceVersion)
	assert.Equal(t, "test", config.Telemetry.Environment)
	assert.Equal(t, "collector:4317", config.Telemetry.Endpoint)
	assert.Equal(t, 0.25, config.Telemetry.SampleRatio)
	assert.Equal(t, "masked-value", config.Telemetry.Headers["authorization"])
	assert.Equal(t, 3*time.Second, config.Telemetry.Timeout)
}

func TestLoadConfig_EnvironmentVariables(t *testing.T) {
	err := os.Setenv("PRISM_SCHEDULER_INTERVAL", "15m")
	require.NoError(t, err)
	err = os.Setenv("PRISM_SCHEDULER_POSTGRES_USERNAME", "tester")
	require.NoError(t, err)
	err = os.Setenv("PRISM_SCHEDULER_VALKEY_USERNAME", "cache-user")
	require.NoError(t, err)

	defer func() {
		err := os.Unsetenv("PRISM_SCHEDULER_INTERVAL")
		assert.NoError(t, err)
		err = os.Unsetenv("PRISM_SCHEDULER_POSTGRES_USERNAME")
		assert.NoError(t, err)
		err = os.Unsetenv("PRISM_SCHEDULER_VALKEY_USERNAME")
		assert.NoError(t, err)
	}()

	config, err := LoadConfig([]string{})
	require.NoError(t, err)

	assert.Equal(t, 15*time.Minute, config.Interval)
	assert.Equal(t, "tester", config.Postgres.Username)
	assert.Equal(t, "cache-user", config.Valkey.Username)
}

func TestPostgresConfig_ConnString(t *testing.T) {
	cfg := appconfig.PostgresConfig{
		Host:     "db.local",
		Port:     5432,
		Username: "user",
		Password: "password",
		DB:       "prism",
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
			args: []string{"--interval=500ms"},
		},
		{
			name: "invalid messenger type",
			args: []string{"--messenger-type=invalid"},
		},
		{
			name: "invalid port (high)",
			args: []string{"--pg-port=70000"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadConfig(tt.args)
			assert.Error(t, err)
		})
	}
}
