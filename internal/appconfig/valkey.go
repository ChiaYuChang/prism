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

// String renders a human-readable summary with the password redacted. The
// default fmt formatting paths (%v, %+v) call this so logging a ValkeyConfig
// value cannot leak credentials.
func (v ValkeyConfig) String() string {
	return fmt.Sprintf("host=%s port=%d username=%s password=%s db=%d",
		v.Host, v.Port, v.Username, prismlogger.SecretMask(v.Password), v.DB)
}

// LogValue redacts the password when the config is logged via slog.Any.
func (v ValkeyConfig) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("host", v.Host),
		slog.Int("port", v.Port),
		slog.String("username", v.Username),
		slog.String("password", prismlogger.SecretMask(v.Password)),
		slog.Int("db", v.DB),
	)
}
