package obs

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	prismlogger "github.com/ChiaYuChang/prism/pkg/logger"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	logsdk "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
)

// LoggingConfig configures slog handler fan-out. Console and file are JSON
// handlers backed by io.Writer sinks; OTEL is a native slog.Handler sink.
type LoggingConfig struct {
	Level   string           `mapstructure:"level" validate:"oneof=debug info warn error"`
	Console ConsoleLogConfig `mapstructure:"console"`
	File    FileLogConfig    `mapstructure:"file"`
	OTEL    OTELLogConfig    `mapstructure:"otel"`

	// Path preserves the existing logger.path flag shape until commands migrate
	// to logger.file.file. When set, file logging is enabled for this path.
	Path string `mapstructure:"path"`
}

type ConsoleLogConfig struct {
	Enable bool   `mapstructure:"enable"`
	Level  string `mapstructure:"level"  validate:"omitempty,oneof=debug info warn error"`
}

type FileLogConfig struct {
	Enable bool   `mapstructure:"enable"`
	File   string `mapstructure:"file"   validate:"omitempty,filepath"`
	Level  string `mapstructure:"level"  validate:"omitempty,oneof=debug info warn error"`
}

type OTELLogConfig struct {
	Enable      bool              `mapstructure:"enable"`
	URL         string            `mapstructure:"url"`
	Level       string            `mapstructure:"level" validate:"omitempty,oneof=debug info warn error"`
	Insecure    bool              `mapstructure:"insecure"`
	Headers     map[string]string `mapstructure:"headers"`
	Timeout     time.Duration     `mapstructure:"timeout"`
	ServiceName string            `mapstructure:"service-name"`
	Environment string            `mapstructure:"environment"`
}

func DefaultLoggingConfig(serviceName ...string) LoggingConfig {
	cfg := LoggingConfig{
		Level: "info",
		Console: ConsoleLogConfig{
			Enable: true,
		},
		OTEL: OTELLogConfig{
			URL:      "otel-collector:4317",
			Insecure: true,
			Timeout:  10 * time.Second,
		},
	}
	if len(serviceName) > 0 {
		cfg.OTEL.ServiceName = serviceName[0]
	}
	return cfg
}

// RegisterLoggingFlags adds shared logging fan-out flags. The legacy log-path
// and log-level flags are preserved while commands migrate to nested config.
func RegisterLoggingFlags(fs *pflag.FlagSet, defaults LoggingConfig) {
	fs.String("log-path", defaults.Path, "Legacy file log path; empty disables file logging unless --log-file-enable is set")
	fs.String("log-level", defaults.Level, "Global log level (debug, info, warn, error)")
	fs.Bool("log-console-enable", defaults.Console.Enable, "Enable console JSON logging")
	fs.String("log-console-level", defaults.Console.Level, "Console log level override (debug, info, warn, error)")
	fs.Bool("log-file-enable", defaults.File.Enable, "Enable file JSON logging")
	fs.String("log-file-file", defaults.File.File, "File log path")
	fs.String("log-file-level", defaults.File.Level, "File log level override (debug, info, warn, error)")
	fs.Bool("log-otel-enable", defaults.OTEL.Enable, "Enable OTLP log export")
	fs.String("log-otel-url", defaults.OTEL.URL, "OTLP log exporter gRPC endpoint host:port")
	fs.String("log-otel-level", defaults.OTEL.Level, "OTLP log level override (debug, info, warn, error)")
	fs.Bool("log-otel-insecure", defaults.OTEL.Insecure, "Use insecure OTLP log transport")
	fs.StringToString("log-otel-headers", defaults.OTEL.Headers, "OTLP log headers as key=value pairs; values are masked in logs")
	fs.Duration("log-otel-timeout", defaults.OTEL.Timeout, "OTLP log exporter timeout")
	fs.String("log-otel-service-name", defaults.OTEL.ServiceName, "OTLP log service.name resource attribute")
	fs.String("log-otel-environment", defaults.OTEL.Environment, "OTLP log deployment.environment resource attribute")
}

// BindLoggingFlags binds log-* flags to nested viper keys under logger.*.
func BindLoggingFlags(v *viper.Viper, fs *pflag.FlagSet) error {
	return bindPrefixedFlags(v, fs, "log-", "logger.")
}

func LoadLoggingConfig(v *viper.Viper) (LoggingConfig, error) {
	cfg := LoggingConfig{
		Level: v.GetString("logger.level"),
		Path:  v.GetString("logger.path"),
		Console: ConsoleLogConfig{
			Enable: v.GetBool("logger.console.enable"),
			Level:  v.GetString("logger.console.level"),
		},
		File: FileLogConfig{
			Enable: v.GetBool("logger.file.enable"),
			File:   v.GetString("logger.file.file"),
			Level:  v.GetString("logger.file.level"),
		},
		OTEL: OTELLogConfig{
			Enable:      v.GetBool("logger.otel.enable"),
			URL:         v.GetString("logger.otel.url"),
			Level:       v.GetString("logger.otel.level"),
			Insecure:    v.GetBool("logger.otel.insecure"),
			Headers:     v.GetStringMapString("logger.otel.headers"),
			Timeout:     v.GetDuration("logger.otel.timeout"),
			ServiceName: firstString(v.GetString("logger.otel.service-name"), v.GetString("logger.otel.service.name")),
			Environment: v.GetString("logger.otel.environment"),
		},
	}
	if cfg.Path != "" && cfg.File.File == "" {
		cfg.File.Enable = true
		cfg.File.File = cfg.Path
	}
	return cfg, nil
}

func firstString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func bindPrefixedFlags(v *viper.Viper, fs *pflag.FlagSet, prefix, target string) error {
	var bindErr error
	fs.VisitAll(func(f *pflag.Flag) {
		if bindErr != nil || !strings.HasPrefix(f.Name, prefix) {
			return
		}
		key := target + strings.ReplaceAll(strings.TrimPrefix(f.Name, prefix), "-", ".")
		if err := v.BindPFlag(key, f); err != nil {
			bindErr = fmt.Errorf("bind %s: %w", key, err)
		}
	})
	return bindErr
}

func (c LoggingConfig) LevelValue() slog.Level {
	return parseSlogLevel(c.Level, slog.LevelInfo)
}

func (c LoggingConfig) String() string {
	return fmt.Sprintf("level=%s console={enable=%t level=%s} file={enable=%t file=%s level=%s} otel={%s}",
		c.Level, c.Console.Enable, c.Console.Level, c.File.Enable, c.File.File, c.File.Level, c.OTEL.String())
}

func (c LoggingConfig) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("level", c.Level),
		slog.Any("console", c.Console),
		slog.Any("file", c.File),
		slog.Any("otel", c.OTEL),
	)
}

func (c ConsoleLogConfig) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Bool("enable", c.Enable),
		slog.String("level", c.Level),
	)
}

func (c FileLogConfig) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Bool("enable", c.Enable),
		slog.String("file", c.File),
		slog.String("level", c.Level),
	)
}

func (c OTELLogConfig) LogValue() slog.Value {
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

func (c OTELLogConfig) String() string {
	return fmt.Sprintf("enable=%t url=%s level=%s insecure=%t timeout=%s service_name=%s environment=%s headers=%s",
		c.Enable, c.URL, c.Level, c.Insecure, c.Timeout, c.ServiceName, c.Environment, maskedHeadersString(c.Headers))
}

// BuildLoggingHandlers builds console/file/OTEL slog handlers. The caller owns
// constructing the *slog.Logger, usually via pkg/logger.NewLoggerFromHandlers.
func BuildLoggingHandlers(ctx context.Context, cfg LoggingConfig) ([]slog.Handler, *os.File, func(context.Context) error, error) {
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
		h, stop, err := NewOTELLogHandler(ctx, cfg.OTEL, globalLevel)
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

	return handlers, file, shutdown, nil
}

// NewOTELLogHandler builds a native OTEL slog.Handler. It does not serialize
// logs as JSON bytes, preserving attributes for VictoriaLogs/Grafana queries.
func NewOTELLogHandler(ctx context.Context, cfg OTELLogConfig, defaultLevel slog.Level) (slog.Handler, func(context.Context) error, error) {
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
