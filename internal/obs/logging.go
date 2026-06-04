package obs

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	prismlogger "github.com/ChiaYuChang/prism/pkg/logger"
	"github.com/spf13/viper"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	logsdk "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
)

// LoggerConfig configures slog fan-out handlers. Console, file, and OTEL are
// independent sinks combined with slog.NewMultiHandler.
type LoggerConfig struct {
	Level   string              `mapstructure:"level"`
	Console ConsoleLoggerConfig `mapstructure:"console"`
	File    FileLoggerConfig    `mapstructure:"file"`
	OTEL    OTELLoggerConfig    `mapstructure:"otel"`

	// Path preserves the existing logger.path flag shape until commands migrate
	// to logger.file.file. When set, file logging is enabled for this path.
	Path string `mapstructure:"path"`
}

type ConsoleLoggerConfig struct {
	Enable bool   `mapstructure:"enable"`
	Level  string `mapstructure:"level"`
}

type FileLoggerConfig struct {
	Enable bool   `mapstructure:"enable"`
	File   string `mapstructure:"file"`
	Level  string `mapstructure:"level"`
}

type OTELLoggerConfig struct {
	Enable      bool              `mapstructure:"enable"`
	URL         string            `mapstructure:"url"`
	Level       string            `mapstructure:"level"`
	Insecure    bool              `mapstructure:"insecure"`
	Headers     map[string]string `mapstructure:"headers"`
	Timeout     time.Duration     `mapstructure:"timeout"`
	ServiceName string            `mapstructure:"service-name"`
	Environment string            `mapstructure:"environment"`
}

func DefaultLoggerConfig() LoggerConfig {
	return LoggerConfig{
		Level: "info",
		Console: ConsoleLoggerConfig{
			Enable: true,
		},
		OTEL: OTELLoggerConfig{
			URL:      "otel-collector:4317",
			Insecure: true,
			Timeout:  10 * time.Second,
		},
	}
}

func LoadLoggerConfig(v *viper.Viper) (LoggerConfig, error) {
	cfg := DefaultLoggerConfig()
	if err := v.UnmarshalKey("logger", &cfg); err != nil {
		return LoggerConfig{}, fmt.Errorf("unmarshal logger config: %w", err)
	}
	if cfg.Path != "" && cfg.File.File == "" {
		cfg.File.Enable = true
		cfg.File.File = cfg.Path
	}
	return cfg, nil
}

func (c LoggerConfig) LevelValue() slog.Level {
	return parseSlogLevel(c.Level, slog.LevelInfo)
}

func (c LoggerConfig) String() string {
	return fmt.Sprintf("level=%s console={enable=%t level=%s} file={enable=%t file=%s level=%s} otel={%s}",
		c.Level, c.Console.Enable, c.Console.Level, c.File.Enable, c.File.File, c.File.Level, c.OTEL.String())
}

func (c LoggerConfig) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("level", c.Level),
		slog.Any("console", c.Console),
		slog.Any("file", c.File),
		slog.Any("otel", c.OTEL),
	)
}

func (c ConsoleLoggerConfig) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Bool("enable", c.Enable),
		slog.String("level", c.Level),
	)
}

func (c FileLoggerConfig) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Bool("enable", c.Enable),
		slog.String("file", c.File),
		slog.String("level", c.Level),
	)
}

func (c OTELLoggerConfig) LogValue() slog.Value {
	attrs := []slog.Attr{
		slog.Bool("enable", c.Enable),
		slog.String("url", c.URL),
		slog.String("level", c.Level),
		slog.Bool("insecure", c.Insecure),
		slog.Duration("timeout", c.Timeout),
		slog.String("service_name", c.ServiceName),
		slog.String("environment", c.Environment),
	}
	for _, key := range sortedHeaderKeys(c.Headers) {
		attrs = append(attrs, slog.String("header_"+key, prismlogger.SecretMask(c.Headers[key])))
	}
	return slog.GroupValue(attrs...)
}

func (c OTELLoggerConfig) String() string {
	return fmt.Sprintf("enable=%t url=%s level=%s insecure=%t timeout=%s service_name=%s environment=%s headers=%s",
		c.Enable, c.URL, c.Level, c.Insecure, c.Timeout, c.ServiceName, c.Environment, maskedHeadersString(c.Headers))
}

// InitConfiguredLogger builds console/file/OTEL handlers and combines them via
// slog.NewMultiHandler. The returned shutdown flushes and stops the OTEL log
// provider when OTEL logging is enabled.
func InitConfiguredLogger(ctx context.Context, cfg LoggerConfig, hooks ...prismlogger.SLogHook) (*slog.Logger, *os.File, func(context.Context) error, error) {
	globalLevel := cfg.LevelValue()
	var handlers []slog.Handler
	var file *os.File
	shutdown := func(context.Context) error { return nil }

	if cfg.Console.Enable {
		handlers = append(handlers, prismlogger.NewJSONHandler(os.Stdout, parseSlogLevel(cfg.Console.Level, globalLevel)))
	}
	if cfg.File.Enable {
		if cfg.File.File == "" {
			return nil, nil, nil, fmt.Errorf("logger file enabled but file path is empty")
		}
		f, err := os.OpenFile(cfg.File.File, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("open log file: %w", err)
		}
		file = f
		handlers = append(handlers, prismlogger.NewJSONHandler(f, parseSlogLevel(cfg.File.Level, globalLevel)))
	}
	if cfg.OTEL.Enable {
		h, stop, err := newOTELLogHandler(ctx, cfg.OTEL, globalLevel)
		if err != nil {
			if file != nil {
				_ = file.Close()
			}
			return nil, nil, nil, err
		}
		handlers = append(handlers, h)
		shutdown = stop
	}

	if len(handlers) == 0 {
		handlers = append(handlers, prismlogger.NewJSONHandler(os.Stdout, globalLevel))
	}

	l := prismlogger.NewLoggerFromHandlers(handlers, hooks...)
	slog.SetDefault(l)
	return l, file, shutdown, nil
}

func newOTELLogHandler(ctx context.Context, cfg OTELLoggerConfig, defaultLevel slog.Level) (slog.Handler, func(context.Context) error, error) {
	if cfg.URL == "" {
		return nil, nil, fmt.Errorf("otel logger enabled but url is empty")
	}
	opts := []otlploggrpc.Option{
		otlploggrpc.WithEndpoint(cfg.URL),
		otlploggrpc.WithTimeout(cfg.Timeout),
	}
	if cfg.Insecure {
		opts = append(opts, otlploggrpc.WithInsecure())
	}
	if len(cfg.Headers) > 0 {
		opts = append(opts, otlploggrpc.WithHeaders(cfg.Headers))
	}
	exporter, err := otlploggrpc.New(ctx, opts...)
	if err != nil {
		return nil, nil, fmt.Errorf("create otel log exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			attribute.String("service.name", cfg.ServiceName),
			attribute.String("deployment.environment", cfg.Environment),
		),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("create otel log resource: %w", err)
	}
	provider := logsdk.NewLoggerProvider(
		logsdk.WithResource(res),
		logsdk.WithProcessor(logsdk.NewBatchProcessor(exporter)),
	)

	h := otelslog.NewHandler(cfg.ServiceName,
		otelslog.WithLoggerProvider(provider),
		otelslog.WithSource(false),
	)
	level := parseSlogLevel(cfg.Level, defaultLevel)
	return levelFilter{next: h, level: level}, provider.Shutdown, nil
}

type levelFilter struct {
	next  slog.Handler
	level slog.Level
}

func (h levelFilter) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.level && h.next.Enabled(ctx, level)
}

func (h levelFilter) Handle(ctx context.Context, r slog.Record) error { return h.next.Handle(ctx, r) }
func (h levelFilter) WithAttrs(attrs []slog.Attr) slog.Handler {
	return levelFilter{next: h.next.WithAttrs(attrs), level: h.level}
}
func (h levelFilter) WithGroup(name string) slog.Handler {
	return levelFilter{next: h.next.WithGroup(name), level: h.level}
}

func parseSlogLevel(raw string, fallback slog.Level) slog.Level {
	switch strings.ToLower(raw) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	case "info", "":
		return fallback
	default:
		return fallback
	}
}
