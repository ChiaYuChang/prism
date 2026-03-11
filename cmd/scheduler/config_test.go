package main

import (
	"os"
	"testing"
	"time"
)

func TestLoadConfig_Defaults(t *testing.T) {
	config, err := LoadConfig([]string{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if config.Interval != 10*time.Minute {
		t.Errorf("expected default interval 10m, got %v", config.Interval)
	}
	if config.Postgres.Host != "localhost" {
		t.Errorf("expected default postgres host localhost, got %s", config.Postgres.Host)
	}
	if config.MessengerType != "nats" {
		t.Errorf("expected default messenger nats, got %s", config.MessengerType)
	}
}

func TestLoadConfig_FromFlags(t *testing.T) {
	args := []string{
		"--interval=5m",
		"--host=127.0.0.1",
		"--port=5433",
		"--messenger-type=gochannel",
	}

	config, err := LoadConfig(args)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if config.Interval != 5*time.Minute {
		t.Errorf("expected interval 5m, got %v", config.Interval)
	}
	if config.Postgres.Host != "127.0.0.1" {
		t.Errorf("expected host 127.0.0.1, got %s", config.Postgres.Host)
	}
	if config.Postgres.Port != 5433 {
		t.Errorf("expected port 5433, got %d", config.Postgres.Port)
	}
	if config.MessengerType != "gochannel" {
		t.Errorf("expected messenger gochannel, got %s", config.MessengerType)
	}
}

func TestLoadConfig_EnvironmentVariables(t *testing.T) {
	os.Setenv("PRISM_SCHEDULER_INTERVAL", "15m")
	os.Setenv("PRISM_SCHEDULER_USER", "tester")
	defer func() {
		os.Unsetenv("PRISM_SCHEDULER_INTERVAL")
		os.Unsetenv("PRISM_SCHEDULER_USER")
	}()

	config, err := LoadConfig([]string{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if config.Interval != 15*time.Minute {
		t.Errorf("expected interval 15m from env, got %v", config.Interval)
	}
	if config.Postgres.User != "tester" {
		t.Errorf("expected user tester from env, got %s", config.Postgres.User)
	}
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
	if cfg.ConnString() != expected {
		t.Errorf("expected %s, got %s", expected, cfg.ConnString())
	}
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
			if err == nil {
				t.Errorf("expected error for %s, but got nil", tt.name)
			}
		})
	}
}
