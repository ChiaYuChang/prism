package obs

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	prismlogger "github.com/ChiaYuChang/prism/pkg/logger"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const otelFlagPrefix = "otel-"

// TelemetryConfig carries shared OpenTelemetry settings for Prism commands.
// Commands should use RegisterTelemetryFlags and BindTelemetryFlags instead of
// duplicating observability config loading locally.
type TelemetryConfig struct {
	Enabled        bool              `mapstructure:"enabled"`
	ServiceName    string            `mapstructure:"service-name"`
	ServiceVersion string            `mapstructure:"service-version"`
	Environment    string            `mapstructure:"environment"`
	Endpoint       string            `mapstructure:"endpoint"`
	Insecure       bool              `mapstructure:"insecure"`
	SampleRatio    float64           `mapstructure:"sample-ratio" validate:"min=0,max=1"`
	Headers        map[string]string `mapstructure:"headers"`
	HeadersFile    string            `mapstructure:"headers-file"`
	Timeout        time.Duration     `mapstructure:"timeout" validate:"min=0"`
}

// Config is kept as a compatibility alias while commands migrate to the more
// explicit TelemetryConfig name.
type Config = TelemetryConfig

// DefaultTelemetryConfig returns safe local defaults for OTLP observability.
func DefaultTelemetryConfig(serviceName string) TelemetryConfig {
	return TelemetryConfig{
		ServiceName: serviceName,
		Environment: "local",
		Endpoint:    "otel-collector:4317",
		Insecure:    true,
		SampleRatio: 1,
		Timeout:     10 * time.Second,
	}
}

// DefaultConfig is a compatibility wrapper for DefaultTelemetryConfig.
func DefaultConfig(serviceName string) Config {
	return DefaultTelemetryConfig(serviceName)
}

// RegisterTelemetryFlags adds shared OTEL flags to fs. Call from command LoadConfig.
func RegisterTelemetryFlags(fs *pflag.FlagSet, defaults TelemetryConfig) {
	fs.Bool("otel-enabled", defaults.Enabled, "Enable OpenTelemetry OTLP export")
	fs.String("otel-service-name", defaults.ServiceName, "OpenTelemetry service.name resource attribute")
	fs.String("otel-service-version", defaults.ServiceVersion, "OpenTelemetry service.version resource attribute")
	fs.String("otel-environment", defaults.Environment, "OpenTelemetry deployment.environment resource attribute")
	fs.String("otel-endpoint", defaults.Endpoint, "OTLP gRPC endpoint host:port")
	fs.Bool("otel-insecure", defaults.Insecure, "Use insecure OTLP transport")
	fs.Float64("otel-sample-ratio", defaults.SampleRatio, "Trace sample ratio between 0 and 1")
	fs.StringToString("otel-headers", defaults.Headers, "OTLP headers as key=value pairs; values are treated as secrets in logs")
	fs.String("otel-headers-file", defaults.HeadersFile, "Path to OTLP headers file; values are treated as secrets in logs")
	fs.Duration("otel-timeout", defaults.Timeout, "OTLP exporter timeout")
}

// RegisterFlags is a compatibility wrapper for RegisterTelemetryFlags.
func RegisterFlags(fs *pflag.FlagSet, defaults Config) {
	RegisterTelemetryFlags(fs, defaults)
}

// BindTelemetryFlags binds otel-* flags to viper keys under telemetry.*.
func BindTelemetryFlags(v *viper.Viper, fs *pflag.FlagSet) error {
	var bindErr error
	fs.VisitAll(func(f *pflag.Flag) {
		if bindErr != nil || !strings.HasPrefix(f.Name, otelFlagPrefix) {
			return
		}
		key := "telemetry." + strings.TrimPrefix(f.Name, otelFlagPrefix)
		if err := v.BindPFlag(key, f); err != nil {
			bindErr = fmt.Errorf("bind %s: %w", key, err)
		}
	})
	return bindErr
}

// BindFlags is a compatibility wrapper for BindTelemetryFlags.
func BindFlags(v *viper.Viper, fs *pflag.FlagSet) error {
	return BindTelemetryFlags(v, fs)
}

// LoadTelemetryConfig loads the shared telemetry config from viper.
func LoadTelemetryConfig(v *viper.Viper) (TelemetryConfig, error) {
	return TelemetryConfig{
		Enabled:        v.GetBool("telemetry.enabled"),
		ServiceName:    v.GetString("telemetry.service-name"),
		ServiceVersion: v.GetString("telemetry.service-version"),
		Environment:    v.GetString("telemetry.environment"),
		Endpoint:       v.GetString("telemetry.endpoint"),
		Insecure:       v.GetBool("telemetry.insecure"),
		SampleRatio:    v.GetFloat64("telemetry.sample-ratio"),
		Headers:        v.GetStringMapString("telemetry.headers"),
		HeadersFile:    v.GetString("telemetry.headers-file"),
		Timeout:        v.GetDuration("telemetry.timeout"),
	}, nil
}

// LoadConfig is a compatibility wrapper for LoadTelemetryConfig.
func LoadConfig(v *viper.Viper) (Config, error) {
	return LoadTelemetryConfig(v)
}

// SaveTelemetryConfig stores cfg under telemetry.* keys in viper. It is useful
// when a command derives defaults such as service name before applying shared
// initialization code.
func SaveTelemetryConfig(v *viper.Viper, cfg TelemetryConfig) {
	v.Set("telemetry.enabled", cfg.Enabled)
	v.Set("telemetry.service-name", cfg.ServiceName)
	v.Set("telemetry.service-version", cfg.ServiceVersion)
	v.Set("telemetry.environment", cfg.Environment)
	v.Set("telemetry.endpoint", cfg.Endpoint)
	v.Set("telemetry.insecure", cfg.Insecure)
	v.Set("telemetry.sample-ratio", cfg.SampleRatio)
	v.Set("telemetry.headers", cfg.Headers)
	v.Set("telemetry.headers-file", cfg.HeadersFile)
	v.Set("telemetry.timeout", cfg.Timeout)
}

// String renders a redacted config summary. OTLP headers may contain API keys
// or authorization tokens, so all header values are masked.
func (c TelemetryConfig) String() string {
	return fmt.Sprintf("enabled=%t service_name=%s service_version=%s environment=%s endpoint=%s insecure=%t sample_ratio=%g headers=%s headers_file=%s timeout=%s",
		c.Enabled, c.ServiceName, c.ServiceVersion, c.Environment, c.Endpoint, c.Insecure,
		c.SampleRatio, maskedHeadersString(c.Headers), c.HeadersFile, c.Timeout)
}

// LogValue redacts sensitive values when logged with slog.Any.
func (c TelemetryConfig) LogValue() slog.Value {
	attrs := []slog.Attr{
		slog.Bool("enabled", c.Enabled),
		slog.String("service_name", c.ServiceName),
		slog.String("service_version", c.ServiceVersion),
		slog.String("environment", c.Environment),
		slog.String("endpoint", c.Endpoint),
		slog.Bool("insecure", c.Insecure),
		slog.Float64("sample_ratio", c.SampleRatio),
		slog.String("headers_file", c.HeadersFile),
		slog.Duration("timeout", c.Timeout),
	}
	for _, key := range sortedHeaderKeys(c.Headers) {
		attrs = append(attrs, slog.String("header_"+key, prismlogger.SecretMask(c.Headers[key])))
	}
	return slog.GroupValue(attrs...)
}

func maskedHeadersString(headers map[string]string) string {
	if len(headers) == 0 {
		return "{}"
	}
	parts := make([]string, 0, len(headers))
	for _, key := range sortedHeaderKeys(headers) {
		parts = append(parts, key+"="+prismlogger.SecretMask(headers[key]))
	}
	return "{" + strings.Join(parts, " ") + "}"
}

func sortedHeaderKeys(headers map[string]string) []string {
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
