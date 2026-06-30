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
	HealthPort      int                       `mapstructure:"health-port" validate:"required,min=1024,max=65535"`
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
	fs.Int("health-port", 8095, "The port for the health check server")
	fs.Duration("shutdown-timeout", 30*time.Second, "Graceful shutdown drain timeout")
	fs.String("archive", "", "S3 archive URI (s3://bucket/prefix)")
	obs.RegisterLoggingFlags(fs, obs.DefaultLoggingConfig("prism.worker.archive"))
	obs.RegisterTelemetryFlags(fs, obs.DefaultTelemetryConfig("prism.worker.archive"))
	fs.String("messenger-type", "nats", "The messenger backend type (nats, gochannel)")
	fs.String("nats-host", "localhost", "The NATS server host")
	fs.Int("nats-port", 4222, "The NATS server port")
	fs.String("nats-token", "", "The NATS server auth token")
	fs.String("nats-token-file", "", "Path to file containing the NATS auth token")
	fs.String("queue-group", "archive-worker", "Queue group for worker subscriptions")
	fs.Int("subscribers-count", 1, "How many subscriber goroutines to run")
	fs.Duration("ack-wait-timeout", 30*time.Second, "Ack wait timeout for NATS subscriber")
	fs.Int64("channel-buffer", 100, "GoChannel output buffer size")
	fs.Bool("persistent", true, "Whether GoChannel should persist messages in memory")
	fs.String("s3-endpoint", "", "S3 endpoint URL")
	fs.String("s3-region", "us-east-1", "S3 region")
	fs.String("s3-access-key", "", "S3 access key")
	fs.String("s3-secret-key", "", "S3 secret key")
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

	switch config.MessengerType {
	case "nats":
		var natsCfg appconfig.NatsConfig
		if err := v.Unmarshal(&natsCfg); err != nil {
			return nil, fmt.Errorf("unmarshal nats config: %w", err)
		}
		if err := natsCfg.ResolveSecrets(); err != nil {
			return nil, fmt.Errorf("nats secrets: %w", err)
		}
		config.Messenger = &natsCfg
	case "gochannel":
		var goChannelCfg appconfig.GoChannelConfig
		if err := v.Unmarshal(&goChannelCfg); err != nil {
			return nil, fmt.Errorf("unmarshal gochannel config: %w", err)
		}
		config.Messenger = &goChannelCfg
	}
	if err := validator.New().Struct(&config); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}
	return &config, nil
}
