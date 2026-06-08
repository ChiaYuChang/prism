package main

import (
	"fmt"
	"strings"
	"time"

	app "github.com/ChiaYuChang/prism/internal/appconfig"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/go-playground/validator/v10"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

type Config struct {
	Interval    time.Duration       `mapstructure:"interval"       validate:"required,min=10s"`
	Once        bool                `mapstructure:"once"`
	RecentLimit int32               `mapstructure:"recent-limit"   validate:"required,min=1,max=500"`
	HealthPort  int                 `mapstructure:"health-port"    validate:"required,min=1024,max=65535"`
	Logger      obs.LoggingConfig   `mapstructure:"logger"`
	Telemetry   obs.TelemetryConfig `mapstructure:"telemetry"`
	Postgres    app.PostgresConfig  `mapstructure:"postgres"`
}

func LoadConfig(args []string) (*Config, error) {
	v := viper.New()
	v.SetEnvPrefix("PRISM_BATCH_DETECTOR")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	v.AutomaticEnv()

	fs := pflag.NewFlagSet("batch-detector", pflag.ContinueOnError)
	fs.StringP("config", "c", "", "Path to the configuration file (YAML or JSON)")
	fs.Duration("interval", time.Minute, "Polling interval for batch completion checks")
	fs.Bool("once", false, "Execute once and exit (for Lambda/Cron)")
	fs.Int32("recent-limit", 100, "Maximum recent batches to inspect for completion")
	fs.Int("health-port", 8083, "The port for the health check server")

	obs.RegisterLoggingFlags(fs, obs.DefaultLoggingConfig("prism.batch.detector"))
	obs.RegisterTelemetryFlags(fs, obs.DefaultTelemetryConfig("prism.batch.detector"))

	fs.String("pg-host", "localhost", "Postgres host")
	fs.Int("pg-port", 5432, "Postgres port")
	fs.String("pg-username", "postgres", "Postgres username")
	fs.String("pg-password", "postgres", "Postgres password")
	fs.String("pg-db", "prism", "Postgres database name")
	fs.String("pg-sslmode", "disable", "Postgres SSL mode")

	if err := fs.Parse(args); err != nil {
		return nil, fmt.Errorf("failed to parse flags: %w", err)
	}

	configPath, _ := fs.GetString("config")
	if configPath != "" {
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
	if err := obs.BindLoggingFlags(v, fs); err != nil {
		return nil, err
	}
	if err := obs.BindTelemetryFlags(v, fs); err != nil {
		return nil, err
	}
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	loggerCfg, err := obs.LoadLoggingConfig(v)
	if err != nil {
		return nil, err
	}
	cfg.Logger = loggerCfg
	telemetryCfg, err := obs.LoadTelemetryConfig(v)
	if err != nil {
		return nil, err
	}
	cfg.Telemetry = telemetryCfg

	validate := validator.New()
	if err := validate.Struct(&cfg); err != nil {
		return nil, fmt.Errorf("config validation failed: %v", err)
	}
	return &cfg, nil
}
