package appconfig

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

type mockHandler struct {
	records []slog.Record
}

func (h *mockHandler) Enabled(context.Context, slog.Level) bool {
	return true
}

func (h *mockHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r)
	return nil
}

func (h *mockHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h
}

func (h *mockHandler) WithGroup(name string) slog.Handler {
	return h
}

func TestNatsConfig_NewMessenger_Warnings(t *testing.T) {
	handler := &mockHandler{}
	logger := slog.New(handler)

	t.Run("no auth", func(t *testing.T) {
		handler.records = nil
		cfg := &NatsConfig{
			Host: "localhost",
			Port: 4222,
		}
		// Expecting error because NATS server is not running
		_, _ = cfg.NewMessenger(logger)

		found := false
		for _, r := range handler.records {
			if r.Level == slog.LevelWarn && r.Message == "connecting to NATS server without authentication" {
				found = true
				break
			}
		}
		assert.True(t, found, "Expected warning log for missing authentication")
	})

	t.Run("no password", func(t *testing.T) {
		handler.records = nil
		cfg := &NatsConfig{
			Host:     "localhost",
			Port:     4222,
			Username: "user",
		}
		_, _ = cfg.NewMessenger(logger)

		found := false
		for _, r := range handler.records {
			if r.Level == slog.LevelWarn && r.Message == "connecting to NATS server without password" {
				found = true
				break
			}
		}
		assert.True(t, found, "Expected warning log for missing password")
	})

	t.Run("with token - no warning", func(t *testing.T) {
		handler.records = nil
		cfg := &NatsConfig{
			Host:  "localhost",
			Port:  4222,
			Token: "secret",
		}
		_, _ = cfg.NewMessenger(logger)

		for _, r := range handler.records {
			assert.NotEqual(t, slog.LevelWarn, r.Level)
		}
	})

	t.Run("with user and password - no warning", func(t *testing.T) {
		handler.records = nil
		cfg := &NatsConfig{
			Host:     "localhost",
			Port:     4222,
			Username: "user",
			Password: "password",
		}
		_, _ = cfg.NewMessenger(logger)

		for _, r := range handler.records {
			assert.NotEqual(t, slog.LevelWarn, r.Level)
		}
	})
}
