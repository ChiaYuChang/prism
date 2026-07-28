package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/ChiaYuChang/prism/internal/appconfig"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/go-playground/validator/v10"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

type Config struct {
	Health          obs.HealthConfig          `mapstructure:"health"`
	ShutdownTimeout time.Duration             `mapstructure:"shutdown-timeout" validate:"required,min=1s"`
	Archive         string                    `mapstructure:"archive" validate:"required"`
	S3              appconfig.S3Config        `mapstructure:"s3"`
	Logger          obs.LoggingConfig         `mapstructure:"logger"`
	Telemetry       obs.TelemetryConfig       `mapstructure:"telemetry"`
	MessengerType   string                    `mapstructure:"messenger-type" validate:"oneof=nats gochannel"`
	Messenger       appconfig.MessengerConfig `mapstructure:"-"`
}

func LoadConfig(args []string) (*Config, error) {
	v := viper.New()
	v.SetEnvPrefix("PRISM_ARCHIVE_WORKER")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	v.AutomaticEnv()

	fs := pflag.NewFlagSet("worker-archive", pflag.ContinueOnError)
	fs.StringP("config", "c", "", "Path to the configuration file (YAML or JSON)")
	obs.RegisterHealthFlags(fs, obs.DefaultHealthConfig(8095))
	fs.Duration("shutdown-timeout", 30*time.Second, "Graceful shutdown drain timeout")
	fs.String("archive", "", "S3 archive URI (s3://bucket/prefix)")
	obs.RegisterLoggingFlags(fs, obs.DefaultLoggingConfig("prism.worker.archive"))
	obs.RegisterTelemetryFlags(fs, obs.DefaultTelemetryConfig("prism.worker.archive"))
	appconfig.RegisterMessengerFlags(fs, "archive-worker")

	fs.String("s3-endpoint", "", "S3 endpoint URL")
	fs.String("s3-region", "us-east-1", "S3 region")
	fs.String("s3-access-key", "", "S3 access key")
	fs.String("s3-secret-key", "", "S3 secret key")
	fs.String("s3-secret-key-file", "", "Path to file containing the S3 secret key")
	fs.Bool("s3-use-path-style", true, "Use path style addressing")

	if err := fs.Parse(args); err != nil {
		return nil, fmt.Errorf("parse flags: %w", err)
	}
	if configPath, _ := fs.GetString("config"); configPath != "" {
		if err := appconfig.ReadConfigFile(v, configPath); err != nil {
			return nil, fmt.Errorf("read config file: %w", err)
		}
	}
	if err := v.BindPFlags(fs); err != nil {
		return nil, fmt.Errorf("bind flags: %w", err)
	}
	if err := obs.BindHealthFlags(v, fs); err != nil {
		return nil, fmt.Errorf("bind health flags: %w", err)
	}
	var config Config
	if err := config.S3.BindFlags(v, fs); err != nil {
		return nil, err
	}
	if err := obs.BindLoggingFlags(v, fs); err != nil {
		return nil, err
	}
	if err := obs.BindTelemetryFlags(v, fs); err != nil {
		return nil, err
	}
	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	loggerCfg, err := obs.LoadLoggingConfig(v)
	if err != nil {
		return nil, err
	}
	config.Logger = loggerCfg
	telemetryCfg, err := obs.LoadTelemetryConfig(v)
	if err != nil {
		return nil, err
	}
	config.Telemetry = telemetryCfg
	if err := config.S3.ResolveSecrets(); err != nil {
		return nil, fmt.Errorf("s3 secrets: %w", err)
	}

	msgrCfg, err := appconfig.LoadMessengerConfig(v)
	if err != nil {
		return nil, err
	}
	config.Messenger = msgrCfg
	if err := validator.New().Struct(&config); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}
	return &config, nil
}
