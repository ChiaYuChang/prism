package logger

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewLoggerFromHandlers_FansOutAndRunsHooks(t *testing.T) {
	var left strings.Builder
	var right strings.Builder

	l := NewLoggerFromHandlers([]slog.Handler{
		NewJSONHandler(&left, slog.LevelInfo),
		NewJSONHandler(&right, slog.LevelInfo),
	}, AttrHook("service", "test-service"))

	l.InfoContext(context.Background(), "hello", slog.String("key", "value"))

	assert.Contains(t, left.String(), "hello")
	assert.Contains(t, left.String(), "test-service")
	assert.Contains(t, right.String(), "hello")
	assert.Contains(t, right.String(), "test-service")
}
