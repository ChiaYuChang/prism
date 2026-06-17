package appconfig

import (
	"fmt"
	"log/slog"

	prismlogger "github.com/ChiaYuChang/prism/pkg/logger"
)

type ValkeyConfig struct {
	Host     string `mapstructure:"host"     validate:"required"`
	Port     int    `mapstructure:"port"     validate:"required,min=1,max=65535"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"       validate:"min=0"`

	// PasswordFile: see PostgresConfig.PasswordFile.
	PasswordFile string `mapstructure:"password-file"`

	// ClientName is a stable logical name for metrics/traces, e.g. api-shared.
	ClientName string `mapstructure:"client-name" validate:"omitempty"`

	// TracingEnabled enables Redis OpenTelemetry tracing for this client.
	TracingEnabled bool `mapstructure:"tracing-enabled"`

	// MetricsEnabled enables Redis Prometheus client metrics for this client.
	MetricsEnabled bool `mapstructure:"metrics-enabled"`
}

// ResolveSecrets loads PasswordFile if set, replacing Password.
func (v *ValkeyConfig) ResolveSecrets() error {
	val, err := LoadFromFile(v.PasswordFile)
	if err != nil {
		return err
	}
	if val != "" {
		v.Password = val
	}
	return nil
}

func (v ValkeyConfig) Addr() string {
	return fmt.Sprintf("%s:%d", v.Host, v.Port)
}

func (v ValkeyConfig) MetricsName() string {
	if v.ClientName != "" {
		return v.ClientName
	}
	return "default"
}

// String renders a human-readable summary with the password redacted. The
// default fmt formatting paths (%v, %+v) call this so logging a ValkeyConfig
// value cannot leak credentials.
func (v ValkeyConfig) String() string {
	return fmt.Sprintf("host=%s port=%d username=%s password=%s db=%d client_name=%s tracing_enabled=%t metrics_enabled=%t",
		v.Host, v.Port, v.Username, prismlogger.SecretMask(v.Password), v.DB,
		v.ClientName, v.TracingEnabled, v.MetricsEnabled)
}

// LogValue redacts the password when the config is logged via slog.Any.
func (v ValkeyConfig) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("host", v.Host),
		slog.Int("port", v.Port),
		slog.String("username", v.Username),
		slog.String("password", prismlogger.SecretMask(v.Password)),
		slog.Int("db", v.DB),
		slog.String("client_name", v.ClientName),
		slog.Bool("tracing_enabled", v.TracingEnabled),
		slog.Bool("metrics_enabled", v.MetricsEnabled),
	)
}
