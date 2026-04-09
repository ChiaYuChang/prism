package appconfig

import "fmt"

type ValkeyConfig struct {
	Host     string `mapstructure:"host"     validate:"required"`
	Port     int    `mapstructure:"port"     validate:"required,min=1,max=65535"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"       validate:"min=0"`
}

func (v ValkeyConfig) Addr() string {
	return fmt.Sprintf("%s:%d", v.Host, v.Port)
}
