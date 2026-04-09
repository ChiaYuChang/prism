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

// Config holds the scheduler's runtime configuration.
type Config struct {
	Interval      time.Duration       `mapstructure:"interval"       validate:"required,min=1m"`
	HealthPort    int                 `mapstructure:"health-port"    validate:"required,min=1024,max=65535"`
	Valkey        app.ValkeyConfig    `mapstructure:"valkey"`
	LogPath       string              `mapstructure:"log-path"`
	LogLevel      string              `mapstructure:"log-level"      validate:"oneof=debug info warn error"`
	BatchSize     int                 `mapstructure:"batch-size"     validate:"required,min=1,max=200"`
	Postgres      app.PostgresConfig  `mapstructure:"postgres"`
	MessengerType string              `mapstructure:"messenger-type" validate:"oneof=nats gochannel"`
	Messenger     app.MessengerConfig `mapstructure:"-"`
}

// GetLogLevel converts the string representation into a slog.Level.
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

// LoadConfig merges pflag, environment variables, and config files into the Config struct.
func LoadConfig(args []string) (*Config, error) {
	v := viper.New()
	v.SetEnvPrefix("PRISM_SCHEDULER")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	v.AutomaticEnv()

	fs := pflag.NewFlagSet("scheduler", pflag.ContinueOnError)

	// Add config file flag
	fs.StringP("config", "c", "", "Path to the configuration file (YAML or JSON)")

	fs.Duration("interval", 10*time.Minute, "The ticker interval for the scheduler (min: 1m, default: 10m)")
	fs.Int("health-port", 8080, "The port for the health check server (default: 8080)")
	fs.String("valkey-host", "localhost", "The host of the Valkey/Redis instance")
	fs.Int("valkey-port", 6379, "The port of the Valkey/Redis instance")
	fs.String("valkey-username", "", "The username for the Valkey/Redis instance")
	fs.String("valkey-password", "", "The password for the Valkey/Redis instance")
	fs.Int("valkey-db", 0, "The database index for the Valkey/Redis instance")

	// Postgres individual flags
	fs.String("pg-host", "localhost", "Postgres host")
	fs.Int("pg-port", 5432, "Postgres port")
	fs.String("pg-username", "postgres", "Postgres username")
	fs.String("pg-password", "postgres", "Postgres password")
	fs.String("pg-db", "prism", "Postgres database name")
	fs.String("pg-sslmode", "disable", "Postgres SSL mode (disable, require, etc.)")

	fs.String("nats-url", "nats://localhost:4222", "The URL for the NATS server (default: nats://localhost:4222)")
	fs.String("queue-group", "", "Queue group for NATS subscribers (unused by scheduler publisher)")
	fs.Int("subscribers-count", 1, "Subscriber count for NATS consumers (unused by scheduler publisher)")
	fs.Duration("ack-wait-timeout", 30*time.Second, "Ack wait timeout for NATS subscribers (unused by scheduler publisher)")
	fs.Int64("channel-buffer", 100, "GoChannel output buffer size")
	fs.Bool("persistent", true, "Whether GoChannel should persist messages in memory")
	fs.String("log-path", "", "The file path for logs (empty for stdout)")
	fs.String("log-level", "info", "The log level (debug, info, warn, error, default: info)")
	fs.String("messenger-type", "nats", "The messenger backend type (nats, gochannel, default: nats)")
	fs.Int("batch-size", 100, "Number of tasks to claim per tick (max: 200, default: 100)")

	if err := fs.Parse(args); err != nil {
		return nil, fmt.Errorf("failed to parse flags: %w", err)
	}

	// 1. Handle Configuration File
	configPath, _ := fs.GetString("config")
	if configPath != "" {
		v.SetConfigFile(configPath)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	}

	// 2. Bind flags to viper (Flags override file values)
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

	var config Config
	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Polymorphic initialization based on MessengerType
	switch config.MessengerType {
	case "nats":
		var natsCfg app.NatsConfig
		if err := v.Unmarshal(&natsCfg); err != nil {
			return nil, fmt.Errorf("failed to unmarshal nats config: %w", err)
		}
		natsCfg.SubscribersCount = 1
		natsCfg.AckWaitTimeout = 30 * time.Second
		config.Messenger = &natsCfg
	case "gochannel":
		var goChannelCfg app.GoChannelConfig
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

	// Validate the specific messenger config if it implements validation
	if config.Messenger != nil {
		if err := validate.Struct(config.Messenger); err != nil {
			return nil, fmt.Errorf("messenger config validation failed: %v", err)
		}
	}

	return &config, nil
}
