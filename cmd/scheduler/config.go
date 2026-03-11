package main

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ChiaYuChang/prism/internal/infra"
	"github.com/go-playground/validator/v10"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// MessengerConfig defines a polymorphic interface for different message queue backends.
type MessengerConfig interface {
	NewMessenger(logger *slog.Logger) (*infra.Messenger, error)
}

// NatsConfig implements MessengerConfig for NATS JetStream.
type NatsConfig struct {
	URL string `mapstructure:"nats-url" validate:"required,url"`
}

func (n *NatsConfig) NewMessenger(logger *slog.Logger) (*infra.Messenger, error) {
	return infra.NewNatsMessenger(n.URL, logger)
}

// GoChannelConfig implements MessengerConfig for in-memory Go Channels.
type GoChannelConfig struct{}

func (g *GoChannelConfig) NewMessenger(logger *slog.Logger) (*infra.Messenger, error) {
	return infra.NewGoChannelMessenger(logger)
}

// PostgresConfig holds the database connection details.
type PostgresConfig struct {
	Host     string `mapstructure:"host"     validate:"required"`
	Port     int    `mapstructure:"port"     validate:"required,min=1,max=65535"`
	User     string `mapstructure:"user"     validate:"required"`
	Password string `mapstructure:"password" validate:"required"`
	DBName   string `mapstructure:"db"       validate:"required"`
	SSLMode  string `mapstructure:"sslmode"  validate:"oneof=disable require verify-ca verify-full"`
}

// ConnString returns the DSN for pgx/pq.
func (p *PostgresConfig) ConnString() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		p.User, p.Password, p.Host, p.Port, p.DBName, p.SSLMode)
}

// Config holds the scheduler's runtime configuration.
type Config struct {
	Interval      time.Duration  `mapstructure:"interval"       validate:"required,min=1m"`
	HealthPort    int            `mapstructure:"health-port"    validate:"required,min=1024,max=65535"`
	ValkeyAddr    string         `mapstructure:"valkey-addr"    validate:"required"`
	Postgres      PostgresConfig `mapstructure:",squash"`
	LogPath       string         `mapstructure:"log-path"`
	LogLevel      string         `mapstructure:"log-level"      validate:"oneof=debug info warn error"`
	MessengerType string         `mapstructure:"messenger-type" validate:"oneof=nats gochannel"`
	BatchSize     int            `mapstructure:"batch-size"     validate:"required,min=1,max=200"`

	// Messenger holds the concrete configuration for the chosen messenger type.
	Messenger MessengerConfig `mapstructure:"-"`
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
	fs.String("valkey-addr", "localhost:6379", "The address of the Valkey/Redis instance (default: localhost:6379)")

	// Postgres individual flags
	fs.String("host", "localhost", "Postgres host")
	fs.Int("port", 5432, "Postgres port")
	fs.String("user", "postgres", "Postgres user")
	fs.String("password", "postgres", "Postgres password")
	fs.String("db", "prism", "Postgres database name")
	fs.String("sslmode", "disable", "Postgres SSL mode (disable, require, etc.)")

	fs.String("nats-url", "nats://localhost:4222", "The URL for the NATS server (default: nats://localhost:4222)")
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

	var config Config
	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Polymorphic initialization based on MessengerType
	switch config.MessengerType {
	case "nats":
		var natsCfg NatsConfig
		if err := v.Unmarshal(&natsCfg); err != nil {
			return nil, fmt.Errorf("failed to unmarshal nats config: %w", err)
		}
		config.Messenger = &natsCfg
	case "gochannel":
		config.Messenger = &GoChannelConfig{}
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
