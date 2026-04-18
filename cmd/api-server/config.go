package main

import (
	"fmt"
	"strings"
	"time"

	app "github.com/ChiaYuChang/prism/internal/appconfig"
	"github.com/go-playground/validator/v10"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// Config is the runtime configuration for the API server.
type Config struct {
	Port            int                `mapstructure:"port"              validate:"required,min=1024,max=65535"`
	ReadTimeout     time.Duration      `mapstructure:"read-timeout"      validate:"required,min=1s"`
	WriteTimeout    time.Duration      `mapstructure:"write-timeout"     validate:"required,min=1s"`
	ShutdownTimeout time.Duration      `mapstructure:"shutdown-timeout"  validate:"required,min=1s"`
	CORSOrigins     []string           `mapstructure:"cors-origins"`
	Logger          app.LoggerConfig   `mapstructure:"logger"`
	Postgres        app.PostgresConfig `mapstructure:"postgres"`
}

func LoadConfig(args []string) (*Config, error) {
	v := viper.New()
	v.SetEnvPrefix("PRISM_API")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	v.AutomaticEnv()

	fs := pflag.NewFlagSet("api-server", pflag.ContinueOnError)
	fs.StringP("config", "c", "", "Path to the configuration file (YAML or JSON)")

	fs.Int("port", 8090, "HTTP listen port")
	fs.Duration("read-timeout", 10*time.Second, "HTTP server read timeout")
	fs.Duration("write-timeout", 30*time.Second, "HTTP server write timeout")
	fs.Duration("shutdown-timeout", 10*time.Second, "Graceful shutdown timeout")
	fs.StringSlice("cors-origins", []string{}, "Allowed CORS origins (comma-separated; empty disables CORS)")

	fs.String("log-path", "", "The file path for logs (empty for stdout)")
	fs.String("log-level", "info", "The log level (debug, info, warn, error)")

	fs.String("pg-host", "localhost", "Postgres host")
	fs.Int("pg-port", 5432, "Postgres port")
	fs.String("pg-username", "postgres", "Postgres username")
	fs.String("pg-password", "postgres", "Postgres password")
	fs.String("pg-db", "prism", "Postgres database name")
	fs.String("pg-sslmode", "disable", "Postgres SSL mode")

	if err := fs.Parse(args); err != nil {
		return nil, fmt.Errorf("failed to parse flags: %w", err)
	}

	if configPath, _ := fs.GetString("config"); configPath != "" {
		v.SetConfigFile(configPath)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	}
	if err := v.BindPFlags(fs); err != nil {
		return nil, fmt.Errorf("failed to bind flags: %w", err)
	}

	var cfg Config
	if err := cfg.Postgres.BindFlags(v, fs); err != nil {
		return nil, err
	}
	if err := cfg.Logger.BindFlags(v, fs); err != nil {
		return nil, err
	}
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	validate := validator.New()
	if err := validate.Struct(&cfg); err != nil {
		return nil, fmt.Errorf("config validation failed: %v", err)
	}
	return &cfg, nil
}
