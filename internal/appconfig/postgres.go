package appconfig

import (
	"fmt"
	"time"

	prismlogger "github.com/ChiaYuChang/prism/pkg/logger"
)

type PostgresConfig struct {
	Host     string `mapstructure:"host"     validate:"required"`
	Port     int    `mapstructure:"port"     validate:"required,min=1,max=65535"`
	Username string `mapstructure:"username" validate:"required"`
	Password string `mapstructure:"password" validate:"required"`
	DB       string `mapstructure:"db"       validate:"required"`
	SSLMode  string `mapstructure:"sslmode"  validate:"oneof=disable require verify-ca verify-full"`

	// PasswordFile is an optional path to a file containing the password.
	// When non-empty, ResolveSecrets reads the file and overrides Password,
	// so prod deployments can mount a docker / k8s secret at e.g.
	// /run/secrets/pg-password without exposing the value in env or argv.
	PasswordFile string `mapstructure:"password-file"`

	// Pool tuning. Zero values fall back to defaults in pg.Factory.
	MaxConns        int32         `mapstructure:"max-conns"     validate:"min=0"`
	MinConns        int32         `mapstructure:"min-conns"     validate:"min=0"`
	MaxConnIdleTime time.Duration `mapstructure:"max-idle-time" validate:"min=0"`
	MaxConnLifetime time.Duration `mapstructure:"max-lifetime"  validate:"min=0"`
}

// ResolveSecrets loads PasswordFile if set, replacing Password. Call after
// viper.Unmarshal but before validation so the required-Password check sees
// the file-derived value.
func (p *PostgresConfig) ResolveSecrets() error {
	v, err := LoadFromFile(p.PasswordFile)
	if err != nil {
		return err
	}
	if v != "" {
		p.Password = v
	}
	return nil
}

func (p *PostgresConfig) ConnString() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		p.Username, p.Password, p.Host, p.Port, p.DB, p.SSLMode)
}

func (p PostgresConfig) String() string {
	return fmt.Sprintf("host=%s port=%d username=%s password=%s db=%s sslmode=%s max_conns=%d min_conns=%d max_idle=%s max_lifetime=%s",
		p.Host, p.Port, p.Username, prismlogger.SecretMask(p.Password), p.DB, p.SSLMode,
		p.MaxConns, p.MinConns, p.MaxConnIdleTime, p.MaxConnLifetime)
}
