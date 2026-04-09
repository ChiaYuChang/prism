package appconfig

import (
	"fmt"

	"github.com/ChiaYuChang/prism/pkg/utils"
)

type PostgresConfig struct {
	Host     string `mapstructure:"host"     validate:"required"`
	Port     int    `mapstructure:"port"     validate:"required,min=1,max=65535"`
	Username string `mapstructure:"username" validate:"required"`
	Password string `mapstructure:"password" validate:"required"`
	DB       string `mapstructure:"db"       validate:"required"`
	SSLMode  string `mapstructure:"sslmode"  validate:"oneof=disable require verify-ca verify-full"`
}

func (p *PostgresConfig) ConnString() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		p.Username, p.Password, p.Host, p.Port, p.DB, p.SSLMode)
}

func (p PostgresConfig) String() string {
	return fmt.Sprintf("host=%s port=%d username=%s password=%s db=%s sslmode=%s",
		p.Host, p.Port, p.Username, utils.SecretMask(p.Password), p.DB, p.SSLMode)
}
