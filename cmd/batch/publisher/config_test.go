package main

import (
	"os"
	"testing"
	"time"

	app "github.com/ChiaYuChang/prism/internal/appconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig_Defaults(t *testing.T) {
	cfg, err := LoadConfig([]string{})
	require.NoError(t, err)

	assert.Equal(t, time.Minute, cfg.Interval)
	assert.False(t, cfg.Once)
	assert.Equal(t, int32(100), cfg.RecentLimit)
	assert.Equal(t, 8084, cfg.HealthPort)
	assert.Equal(t, "localhost", cfg.Postgres.Host)
	assert.Equal(t, "nats", cfg.MessengerType)

	// Messenger polymorphism: default nats type populated.
	natsCfg, ok := cfg.Messenger.(*app.NatsConfig)
	require.True(t, ok, "default messenger should be *NatsConfig")
	assert.Equal(t, "localhost", natsCfg.Host)
	assert.Equal(t, 4222, natsCfg.Port)
}

func TestLoadConfig_FromFlags_Gochannel(t *testing.T) {
	args := []string{
		"--interval=45s",
		"--once=true",
		"--recent-limit=25",
		"--messenger-type=gochannel",
		"--channel-buffer=256",
		"--pg-host=10.0.0.1",
		"--pg-username=u",
		"--pg-password=p",
	}

	cfg, err := LoadConfig(args)
	require.NoError(t, err)

	assert.Equal(t, 45*time.Second, cfg.Interval)
	assert.True(t, cfg.Once)
	assert.Equal(t, int32(25), cfg.RecentLimit)
	assert.Equal(t, "gochannel", cfg.MessengerType)

	goCfg, ok := cfg.Messenger.(*app.GoChannelConfig)
	require.True(t, ok, "messenger should be *GoChannelConfig")
	assert.Equal(t, int64(256), goCfg.ChannelBuffer)
}

func TestLoadConfig_FromFlags_Nats(t *testing.T) {
	args := []string{
		"--messenger-type=nats",
		"--nats-host=nats.local",
		"--nats-port=4222",
		"--pg-username=u",
		"--pg-password=p",
	}

	cfg, err := LoadConfig(args)
	require.NoError(t, err)

	assert.Equal(t, "nats", cfg.MessengerType)
	natsCfg, ok := cfg.Messenger.(*app.NatsConfig)
	require.True(t, ok)
	assert.Equal(t, "nats.local", natsCfg.Host)
	assert.Equal(t, 4222, natsCfg.Port)
}

func TestLoadConfig_EnvironmentVariables(t *testing.T) {
	t.Setenv("PRISM_BATCH_PUBLISHER_INTERVAL", "90s")
	t.Setenv("PRISM_BATCH_PUBLISHER_POSTGRES_USERNAME", "envuser")
	t.Setenv("PRISM_BATCH_PUBLISHER_MESSENGER_TYPE", "gochannel")

	cfg, err := LoadConfig([]string{})
	require.NoError(t, err)

	assert.Equal(t, 90*time.Second, cfg.Interval)
	assert.Equal(t, "envuser", cfg.Postgres.Username)
	assert.Equal(t, "gochannel", cfg.MessengerType)
}

func TestLoadConfig_ValidationFailed(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"interval too short", []string{"--interval=1s"}},
		{"recent-limit zero", []string{"--recent-limit=0"}},
		{"recent-limit above max", []string{"--recent-limit=501"}},
		{"health-port out of range", []string{"--health-port=80"}},
		{"invalid messenger-type", []string{"--messenger-type=bogus"}},
		{"invalid pg-port", []string{"--pg-port=70000"}},
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
		"PRISM_BATCH_PUBLISHER_INTERVAL",
		"PRISM_BATCH_PUBLISHER_POSTGRES_USERNAME",
		"PRISM_BATCH_PUBLISHER_MESSENGER_TYPE",
	} {
		_ = os.Unsetenv(k)
	}
	os.Exit(m.Run())
}
