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

// Config carries shared OpenTelemetry settings for Prism commands.
// Commands should use RegisterFlags and BindFlags instead of duplicating
// observability config loading locally.
type Config struct {
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

// DefaultConfig returns safe local defaults for OTLP observability.
func DefaultConfig(serviceName string) Config {
	return Config{
		ServiceName: serviceName,
		Environment: "local",
		Endpoint:    "otel-collector:4317",
		Insecure:    true,
		SampleRatio: 1,
		Timeout:     10 * time.Second,
	}
}

// RegisterFlags adds shared OTEL flags to fs. Call from command LoadConfig.
func RegisterFlags(fs *pflag.FlagSet, defaults Config) {
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

// BindFlags binds otel-* flags to viper keys under telemetry.*.
func BindFlags(v *viper.Viper, fs *pflag.FlagSet) error {
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

// LoadConfig unmarshals the shared telemetry config from viper.
func LoadConfig(v *viper.Viper) (Config, error) {
	return Config{
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

// String renders a redacted config summary. OTLP headers may contain API keys
// or authorization tokens, so all header values are masked.
func (c Config) String() string {
	return fmt.Sprintf("enabled=%t service_name=%s service_version=%s environment=%s endpoint=%s insecure=%t sample_ratio=%g headers=%s headers_file=%s timeout=%s",
		c.Enabled, c.ServiceName, c.ServiceVersion, c.Environment, c.Endpoint, c.Insecure,
		c.SampleRatio, maskedHeadersString(c.Headers), c.HeadersFile, c.Timeout)
}

// LogValue redacts sensitive values when logged with slog.Any.
func (c Config) LogValue() slog.Value {
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
