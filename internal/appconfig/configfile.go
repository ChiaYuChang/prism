package appconfig

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/spf13/viper"
)

type pendingLog struct {
	Time    time.Time
	Level   slog.Level
	Message string
	Attr    []slog.Attr
}

type pendingLogQueue struct {
	mu  sync.Mutex
	msg []pendingLog
}

// Add appends a new structured log message to the queue with the current time.
func (q *pendingLogQueue) Add(level slog.Level, msg string, attr ...slog.Attr) {
	q.AddWithTime(time.Now(), level, msg, attr...)
}

// AddWithTime appends a new structured log message to the queue with a specific time.
func (q *pendingLogQueue) AddWithTime(t time.Time, level slog.Level, msg string, attr ...slog.Attr) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.msg = append(q.msg, pendingLog{
		Time:    t,
		Level:   level,
		Message: msg,
		Attr:    attr,
	})
}

// timeOverrideHandler wraps an existing handler and overrides the record's time
// to match the original timestamp when the log was queued.
type timeOverrideHandler struct {
	slog.Handler
	t time.Time
}

func (h timeOverrideHandler) Handle(ctx context.Context, r slog.Record) error {
	r.Time = h.t
	return h.Handler.Handle(ctx, r)
}

func (h timeOverrideHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return timeOverrideHandler{
		Handler: h.Handler.WithAttrs(attrs),
		t:       h.t,
	}
}

func (h timeOverrideHandler) WithGroup(name string) slog.Handler {
	return timeOverrideHandler{
		Handler: h.Handler.WithGroup(name),
		t:       h.t,
	}
}

// Flush logs all queued messages using the active default slog logger,
// overriding the output timestamp to match when they were queued,
// and clears the queue.
func (q *pendingLogQueue) Flush() {
	q.mu.Lock()
	msgs := q.msg
	q.msg = nil
	q.mu.Unlock()

	for _, m := range msgs {
		h := timeOverrideHandler{
			Handler: slog.Default().Handler(),
			t:       m.Time,
		}
		logger := slog.New(h)

		args := make([]any, len(m.Attr))
		for i, attr := range m.Attr {
			args[i] = attr
		}
		logger.Log(context.Background(), m.Level, m.Message, args...)
	}
}

func (q *pendingLogQueue) Reset() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.msg = nil
}

var startupLogs pendingLogQueue

// FlushPendingLogs logs startup messages queued before the configured logger is ready.
func FlushPendingLogs() {
	startupLogs.Flush()
}

// ReadTemplatedConfig reads path as a Go text/template config file and returns
// the rendered bytes plus the config type expected by Viper.
func ReadTemplatedConfig(path string) ([]byte, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read config file %q: %w", path, err)
	}

	tmpl, err := template.New(filepath.Base(path)).Funcs(template.FuncMap{
		"env": envWithDefault,
	}).Parse(string(raw))
	if err != nil {
		return nil, "", fmt.Errorf("parse config template %q: %w", path, err)
	}

	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, nil); err != nil {
		return nil, "", fmt.Errorf("render config template %q: %w", path, err)
	}

	configType, err := configTypeForPath(path)
	if err != nil {
		return nil, "", err
	}
	return rendered.Bytes(), configType, nil
}

// ReadConfigFile renders a template-capable config file and loads it into v.
func ReadConfigFile(v *viper.Viper, path string) error {
	body, configType, err := ReadTemplatedConfig(path)
	if err != nil {
		return err
	}
	v.SetConfigType(configType)
	if err := v.ReadConfig(bytes.NewReader(body)); err != nil {
		return fmt.Errorf("parse rendered config file %q: %w", path, err)
	}
	startupLogs.Add(slog.LevelInfo, "loaded config file",
		slog.String("path", path),
		slog.String("type", configType),
	)
	return nil
}

func envWithDefault(name, fallback string) string {
	value, ok := os.LookupEnv(name)
	if !ok {
		startupLogs.Add(slog.LevelWarn, "Environment variable not found, using fallback",
			slog.String("name", name),
			slog.String("fallback", fallback),
		)
		return fallback
	}
	return value
}

func configTypeForPath(path string) (string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		return "yaml", nil
	case ".json":
		return "json", nil
	default:
		return "", fmt.Errorf("unsupported config file extension %q for %q", filepath.Ext(path), path)
	}
}
