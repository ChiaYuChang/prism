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
	Interval      time.Duration       `mapstructure:"interval"       validate:"required,min=10s"`
	Once          bool                `mapstructure:"once"`
	RecentLimit   int32               `mapstructure:"recent-limit"   validate:"required,min=1,max=500"`
	HealthPort    int                 `mapstructure:"health-port"    validate:"required,min=1024,max=65535"`
	Logger        obs.LoggingConfig   `mapstructure:"logger"`
	Telemetry     obs.TelemetryConfig `mapstructure:"telemetry"`
	Postgres      app.PostgresConfig  `mapstructure:"postgres"`
	MessengerType string              `mapstructure:"messenger-type" validate:"oneof=nats gochannel"`
	Messenger     app.MessengerConfig `mapstructure:"-"`
}

func LoadConfig(args []string) (*Config, error) {
	v := viper.New()
	v.SetEnvPrefix("PRISM_BATCH_PUBLISHER")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	v.AutomaticEnv()

	fs := pflag.NewFlagSet("batch-publisher", pflag.ContinueOnError)
	fs.StringP("config", "c", "", "Path to the configuration file (YAML or JSON)")
	fs.Duration("interval", time.Minute, "Polling interval for batch completion checks")
	fs.Bool("once", false, "Execute once and exit (for Lambda/Cron)")
	fs.Int32("recent-limit", 100, "Maximum recent batches to inspect for completion")
	fs.Int("health-port", 8084, "The port for the health check server")

	obs.RegisterLoggingFlags(fs, obs.DefaultLoggingConfig("prism.batch.publisher"))
	obs.RegisterTelemetryFlags(fs, obs.DefaultTelemetryConfig("prism.batch.publisher"))

	fs.String("pg-host", "localhost", "Postgres host")
	fs.Int("pg-port", 5432, "Postgres port")
	fs.String("pg-username", "postgres", "Postgres username")
	fs.String("pg-password", "postgres", "Postgres password")
	fs.String("pg-db", "prism", "Postgres database name")
	fs.String("pg-sslmode", "disable", "Postgres SSL mode")

	fs.String("messenger-type", "nats", "The messenger backend type (nats, gochannel)")
	fs.String("nats-host", "localhost", "The NATS server host")
	fs.Int("nats-port", 4222, "The NATS server port")
	fs.String("nats-token", "", "The NATS server auth token")
	fs.String("queue-group", "", "Queue group for NATS subscribers")
	fs.Int("subscribers-count", 1, "How many subscriber goroutines to run")
	fs.Duration("ack-wait-timeout", 30*time.Second, "Ack wait timeout for NATS subscriber")
	fs.Int64("channel-buffer", 100, "GoChannel output buffer size")
	fs.Bool("persistent", true, "Whether GoChannel should persist messages in memory")

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

	switch cfg.MessengerType {
	case "nats":
		var natsCfg app.NatsConfig
		if err := v.Unmarshal(&natsCfg); err != nil {
			return nil, fmt.Errorf("failed to unmarshal nats config: %w", err)
		}
		natsCfg.SubscribersCount = 1
		natsCfg.AckWaitTimeout = 30 * time.Second
		cfg.Messenger = &natsCfg
	case "gochannel":
		var goChannelCfg app.GoChannelConfig
		if err := v.Unmarshal(&goChannelCfg); err != nil {
			return nil, fmt.Errorf("failed to unmarshal gochannel config: %w", err)
		}
		if goChannelCfg.ChannelBuffer == 0 {
			goChannelCfg.ChannelBuffer = 100
		}
		cfg.Messenger = &goChannelCfg
	}

	validate := validator.New()
	if err := validate.Struct(&cfg); err != nil {
		return nil, fmt.Errorf("config validation failed: %v", err)
	}
	if cfg.Messenger != nil {
		if err := validate.Struct(cfg.Messenger); err != nil {
			return nil, fmt.Errorf("messenger config validation failed: %v", err)
		}
	}
	return &cfg, nil
}
