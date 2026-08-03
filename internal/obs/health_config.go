package obs

import (
	"fmt"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	HealthzPath = "/healthz"
	ReadyzPath  = "/readyz"
	MetricsPath = "/metrics"
)

// HealthConfig contains the process-local health server settings.
type HealthConfig struct {
	Enabled bool `mapstructure:"enabled"`
	Port    int  `mapstructure:"port" validate:"required,min=1024,max=65535"`
}

// DefaultHealthConfig returns health settings for a command with the given port.
func DefaultHealthConfig(port int) HealthConfig {
	return HealthConfig{Enabled: true, Port: port}
}

// RegisterHealthFlags registers the shared health server flags.
func RegisterHealthFlags(fs *pflag.FlagSet, defaults HealthConfig) {
	fs.Bool("health-enabled", defaults.Enabled, "Enable the health server")
	fs.Int("health-port", defaults.Port, "The port for the health server")
}

// BindHealthFlags binds health flags under the health configuration key.
func BindHealthFlags(v *viper.Viper, fs *pflag.FlagSet) error {
	if err := v.BindPFlag("health.enabled", fs.Lookup("health-enabled")); err != nil {
		return fmt.Errorf("bind health.enabled: %w", err)
	}
	if err := v.BindPFlag("health.port", fs.Lookup("health-port")); err != nil {
		return fmt.Errorf("bind health.port: %w", err)
	}
	return nil
}
