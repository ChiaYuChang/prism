package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/ChiaYuChang/prism/internal/appconfig"
	"github.com/go-playground/validator/v10"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	DefaultScoutConfigPath = "internal/discovery/scout/config/scouts.yaml"
)

type Config struct {
	HealthPort      int                       `mapstructure:"health-port"    validate:"required,min=1024,max=65535"`
	Logger          appconfig.LoggerConfig    `mapstructure:"logger"`
	ScoutConfigPath string                    `mapstructure:"scout-config"   validate:"required"`
	HTTPTimeout     time.Duration             `mapstructure:"http-timeout"   validate:"required,min=1s"`
	Postgres        appconfig.PostgresConfig  `mapstructure:"postgres"`
	MessengerType   string                    `mapstructure:"messenger-type" validate:"oneof=nats gochannel"`
	Messenger       appconfig.MessengerConfig `mapstructure:"-"`
}

func LoadConfig(args []string) (*Config, error) {
	v := viper.New()
	v.SetEnvPrefix("PRISM_DISCOVERY_WORKER")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	v.AutomaticEnv()

	fs := pflag.NewFlagSet("worker-discovery", pflag.ContinueOnError)
	fs.StringP("config", "c", "", "Path to the configuration file (YAML or JSON)")
	fs.Int("health-port", 8081, "The port for the health check server")
	fs.String("log-path", "", "The file path for logs (empty for stdout)")
	fs.String("log-level", "info", "The log level (debug, info, warn, error)")
	fs.String("messenger-type", "nats", "The messenger backend type (nats, gochannel)")
	fs.String("scout-config", DefaultScoutConfigPath, "path to scout config file")
	fs.Duration("http-timeout", 30*time.Second, "HTTP timeout for outbound discovery requests")

	fs.String("pg-host", "localhost", "Postgres host")
	fs.Int("pg-port", 5432, "Postgres port")
	fs.String("pg-username", "postgres", "Postgres username")
	fs.String("pg-password", "postgres", "Postgres password")
	fs.String("pg-db", "prism", "Postgres database name")
	fs.String("pg-sslmode", "disable", "Postgres SSL mode")

	fs.String("nats-url", "nats://localhost:4222", "The URL for the NATS server")
	fs.String("queue-group", "discovery-worker", "Queue group for worker subscriptions")
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
	var config Config
	if err := config.Postgres.BindFlags(v, fs); err != nil {
		return nil, err
	}
	if err := config.Logger.BindFlags(v, fs); err != nil {
		return nil, err
	}
	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	switch config.MessengerType {
	case "nats":
		var natsCfg appconfig.NatsConfig
		if err := v.Unmarshal(&natsCfg); err != nil {
			return nil, fmt.Errorf("failed to unmarshal nats config: %w", err)
		}
		if natsCfg.SubscribersCount == 0 {
			natsCfg.SubscribersCount = 1
		}
		if natsCfg.AckWaitTimeout == 0 {
			natsCfg.AckWaitTimeout = 30 * time.Second
		}
		config.Messenger = &natsCfg
	case "gochannel":
		var goChannelCfg appconfig.GoChannelConfig
		if err := v.Unmarshal(&goChannelCfg); err != nil {
			return nil, fmt.Errorf("failed to unmarshal gochannel config: %w", err)
		}
		if goChannelCfg.ChannelBuffer == 0 {
			goChannelCfg.ChannelBuffer = 100
		}
		config.Messenger = &goChannelCfg
	}

	validate := validator.New()
	if err := validate.Struct(&config); err != nil {
		return nil, fmt.Errorf("config validation failed: %v", err)
	}
	if config.Messenger != nil {
		if err := validate.Struct(config.Messenger); err != nil {
			return nil, fmt.Errorf("messenger config validation failed: %v", err)
		}
	}

	return &config, nil
}
