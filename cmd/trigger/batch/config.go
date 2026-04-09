package main

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	app "github.com/ChiaYuChang/prism/internal/appconfig"
	"github.com/go-playground/validator/v10"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

type Config struct {
	Interval      time.Duration       `mapstructure:"interval"       validate:"required,min=10s"`
	RecentLimit   int32               `mapstructure:"recent-limit"   validate:"required,min=1,max=500"`
	HealthPort    int                 `mapstructure:"health-port"    validate:"required,min=1024,max=65535"`
	Valkey        app.ValkeyConfig    `mapstructure:"valkey"`
	LogPath       string              `mapstructure:"log-path"`
	LogLevel      string              `mapstructure:"log-level"      validate:"oneof=debug info warn error"`
	Postgres      app.PostgresConfig  `mapstructure:"postgres"`
	MessengerType string              `mapstructure:"messenger-type" validate:"oneof=nats gochannel"`
	Messenger     app.MessengerConfig `mapstructure:"-"`
}

func (c *Config) GetLogLevel() slog.Level {
	switch strings.ToLower(c.LogLevel) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func LoadConfig(args []string) (*Config, error) {
	v := viper.New()
	v.SetEnvPrefix("PRISM_TRIGGER_BATCH")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	v.AutomaticEnv()

	fs := pflag.NewFlagSet("trigger-batch", pflag.ContinueOnError)
	fs.StringP("config", "c", "", "Path to the configuration file (YAML or JSON)")
	fs.Duration("interval", time.Minute, "Polling interval for batch completion checks")
	fs.Int32("recent-limit", 100, "Maximum recent PARTY contents to inspect for batch completion")
	fs.Int("health-port", 8082, "The port for the health check server")
	fs.String("valkey-host", "localhost", "The host of the Valkey/Redis instance")
	fs.Int("valkey-port", 6379, "The port of the Valkey/Redis instance")
	fs.String("valkey-username", "", "The username for the Valkey/Redis instance")
	fs.String("valkey-password", "", "The password for the Valkey/Redis instance")
	fs.Int("valkey-db", 0, "The database index for the Valkey/Redis instance")
	fs.String("log-path", "", "The file path for logs (empty for stdout)")
	fs.String("log-level", "info", "The log level (debug, info, warn, error)")
	fs.String("messenger-type", "nats", "The messenger backend type (nats, gochannel)")
	fs.String("pg-host", "localhost", "Postgres host")
	fs.Int("pg-port", 5432, "Postgres port")
	fs.String("pg-username", "postgres", "Postgres username")
	fs.String("pg-password", "postgres", "Postgres password")
	fs.String("pg-db", "prism", "Postgres database name")
	fs.String("pg-sslmode", "disable", "Postgres SSL mode")
	fs.String("nats-url", "nats://localhost:4222", "The URL for the NATS server")
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
	if err := v.BindPFlag("valkey.host", fs.Lookup("valkey-host")); err != nil {
		return nil, fmt.Errorf("failed to bind valkey.host: %w", err)
	}
	if err := v.BindPFlag("valkey.port", fs.Lookup("valkey-port")); err != nil {
		return nil, fmt.Errorf("failed to bind valkey.port: %w", err)
	}
	if err := v.BindPFlag("valkey.username", fs.Lookup("valkey-username")); err != nil {
		return nil, fmt.Errorf("failed to bind valkey.username: %w", err)
	}
	if err := v.BindPFlag("valkey.password", fs.Lookup("valkey-password")); err != nil {
		return nil, fmt.Errorf("failed to bind valkey.password: %w", err)
	}
	if err := v.BindPFlag("valkey.db", fs.Lookup("valkey-db")); err != nil {
		return nil, fmt.Errorf("failed to bind valkey.db: %w", err)
	}
	if err := v.BindPFlag("postgres.host", fs.Lookup("pg-host")); err != nil {
		return nil, fmt.Errorf("failed to bind postgres.host: %w", err)
	}
	if err := v.BindPFlag("postgres.port", fs.Lookup("pg-port")); err != nil {
		return nil, fmt.Errorf("failed to bind postgres.port: %w", err)
	}
	if err := v.BindPFlag("postgres.username", fs.Lookup("pg-username")); err != nil {
		return nil, fmt.Errorf("failed to bind postgres.username: %w", err)
	}
	if err := v.BindPFlag("postgres.password", fs.Lookup("pg-password")); err != nil {
		return nil, fmt.Errorf("failed to bind postgres.password: %w", err)
	}
	if err := v.BindPFlag("postgres.db", fs.Lookup("pg-db")); err != nil {
		return nil, fmt.Errorf("failed to bind postgres.db: %w", err)
	}
	if err := v.BindPFlag("postgres.sslmode", fs.Lookup("pg-sslmode")); err != nil {
		return nil, fmt.Errorf("failed to bind postgres.sslmode: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

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
