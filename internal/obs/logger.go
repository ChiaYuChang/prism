package obs

import (
	"context"
	"log/slog"
	"os"

	"github.com/ChiaYuChang/prism/pkg/logger"
	"github.com/google/uuid"
)

// TraceIDHook injects the OpenTelemetry TraceID from context into the log record.
// This is specific to Project Prism's observability layer.
func TraceIDHook(ctx context.Context, r slog.Record) slog.Record {
	if tid := ExtractTraceID(ctx); tid != "" && tid != DefaultTraceIDFallback {
		r.AddAttrs(slog.String("trace_id", tid))
	}
	return r
}

// UserIDHook injects the OpenTelemetry UserID from context into the log record.
// This is specific to Project Prism's observability layer.
func UserIDHook(ctx context.Context, r slog.Record) slog.Record {
	if uid := ExtractUserID(ctx); uid != uuid.Nil {
		r.AddAttrs(slog.String("user_id", uid.String()))
	}
	return r
}

// InitLogger is a wrapper around pkg/logger.InitLogger that simplifies
// initialization for the Prism project.
func InitLogger(path string, level slog.Level, hooks ...logger.SLogHook) (*slog.Logger, *os.File, error) {
	return logger.InitLogger(path, level, prismHooks(hooks...)...)
}

// WithHook is a wrapper around pkg/logger.WithHook.
func WithHook(l *slog.Logger, hooks ...logger.SLogHook) *slog.Logger {
	return logger.WithHook(l, hooks...)
}

// NewLoggerFromHandlers constructs a Prism logger with context observability hooks.
func NewLoggerFromHandlers(handlers []slog.Handler, hooks ...logger.SLogHook) *slog.Logger {
	return logger.NewLoggerFromHandlers(handlers, prismHooks(hooks...)...)
}

func prismHooks(hooks ...logger.SLogHook) []logger.SLogHook {
	out := make([]logger.SLogHook, 0, len(hooks)+2)
	out = append(out, TraceIDHook, UserIDHook)
	out = append(out, hooks...)
	return out
}
