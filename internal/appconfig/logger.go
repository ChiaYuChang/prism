package appconfig

import (
	"log/slog"
	"strings"
)

type LoggerConfig struct {
	Path  string `mapstructure:"path"  validate:"omitempty,filepath"`
	Level string `mapstructure:"level" validate:"oneof=debug info warn error"`
}

// GetLogLevel converts the string representation into a slog.Level.
func (c *LoggerConfig) GetLogLevel() slog.Level {
	switch strings.ToLower(c.Level) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
