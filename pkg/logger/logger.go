package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/ChiaYuChang/prism/pkg/utils"
)

// SLogHook defines a function that can modify or add fields to a log record.
type SLogHook func(ctx context.Context, r slog.Record) slog.Record

// HandlerWithSlogHooks is a custom slog.Handler that executes a list of hooks
// before passing the log record to the next handler in the chain.
type HandlerWithSlogHooks struct {
	next  slog.Handler
	hooks []SLogHook
}

// Enabled reports whether the handler handles records at the given level.
func (h *HandlerWithSlogHooks) Enabled(c context.Context, level slog.Level) bool {
	return h.next.Enabled(c, level)
}

// Handle executes all registered hooks and then passes the record to the next handler.
func (h *HandlerWithSlogHooks) Handle(c context.Context, r slog.Record) error {
	for _, hook := range h.hooks {
		r = hook(c, r)
	}
	return h.next.Handle(c, r)
}

// WithAttrs returns a new handler with the given attributes, while preserving the hooks.
func (h *HandlerWithSlogHooks) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &HandlerWithSlogHooks{
		next:  h.next.WithAttrs(attrs),
		hooks: h.hooks,
	}
}

// WithGroup returns a new handler with the given group name, while preserving the hooks.
func (h *HandlerWithSlogHooks) WithGroup(name string) slog.Handler {
	return &HandlerWithSlogHooks{
		next:  h.next.WithGroup(name),
		hooks: h.hooks,
	}
}

// ServiceHook adds a "service" field to every log record.
func ServiceHook(name string) SLogHook {
	return func(ctx context.Context, r slog.Record) slog.Record {
		r.AddAttrs(slog.String("service", name))
		return r
	}
}

func AttrHook(key string, id string) SLogHook {
	return func(ctx context.Context, r slog.Record) slog.Record {
		r.AddAttrs(slog.String(key, id))
		return r
	}
}

// SecretHook masks sensitive values using utils.SecretMask before logging.
func SecretHook(key string, value string) SLogHook {
	return func(ctx context.Context, r slog.Record) slog.Record {
		r.AddAttrs(slog.String(key, utils.SecretMask(value)))
		return r
	}
}

func SinceHook(key string, t time.Time) SLogHook {
	return func(ctx context.Context, r slog.Record) slog.Record {
		r.AddAttrs(slog.Duration(key, time.Since(t)))
		return r
	}
}

// WithHook returns a new logger with the given hook appended to the existing ones.
// If the logger's handler is not a HandlerWithSlogHooks, it returns the original logger.
func WithHook(l *slog.Logger, hooks ...SLogHook) *slog.Logger {
	h, ok := l.Handler().(*HandlerWithSlogHooks)
	if !ok {
		return l
	}

	newHooks := make([]SLogHook, len(h.hooks)+len(hooks))
	copy(newHooks, h.hooks)
	copy(newHooks[len(h.hooks):], hooks)

	return slog.New(&HandlerWithSlogHooks{
		next:  h.next,
		hooks: newHooks,
	})
}

// InitLogger initializes a default slog.Logger.
// If path is empty, it logs to stdout. Otherwise, it appends to the file.
func InitLogger(path string, level slog.Level, hooks ...SLogHook) (*slog.Logger, *os.File, error) {
	var out io.Writer = os.Stdout
	var logFile *os.File
	var err error

	// 1. Determine output destination
	if path != "" {
		logFile, err = os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to open log file: %w", err)
		}
		out = logFile
	}

	// 2. Create JSON Logger
	logger := slog.New(
		&HandlerWithSlogHooks{
			next:  slog.NewJSONHandler(out, &slog.HandlerOptions{Level: level}),
			hooks: hooks,
		},
	)
	slog.SetDefault(logger)

	return logger, logFile, nil
}
