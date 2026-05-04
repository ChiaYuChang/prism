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

// Config holds the scheduler's runtime configuration.
type Config struct {
	Interval      time.Duration       `mapstructure:"interval"       validate:"required,min=1s"`
	HealthPort    int                 `mapstructure:"health-port"    validate:"required,min=1024,max=65535"`
	Valkey        app.ValkeyConfig    `mapstructure:"valkey"`
	Logger        app.LoggerConfig    `mapstructure:"logger"`
	BatchSize     int                 `mapstructure:"batch-size"     validate:"required,min=1,max=200"`
	Kinds         []string            `mapstructure:"kinds"          validate:"required,min=1,dive,oneof=DIRECTORY_FETCH KEYWORD_SEARCH PAGE_FETCH"`
	Postgres      app.PostgresConfig  `mapstructure:"postgres"`
	MessengerType string              `mapstructure:"messenger-type" validate:"oneof=nats gochannel"`
	Messenger     app.MessengerConfig `mapstructure:"-"`

	// LockKey is the Valkey key used for the distributed scheduler lock.
	// Different scheduler instances (fast/slow) must use different keys.
	// If empty, it is derived from the sorted kinds list at startup.
	LockKey string `mapstructure:"lock-key"`

	// MediaQuota is the number of PAGE_FETCH+MEDIA slots reserved per tick.
	// When > 0, the tick uses a two-step claim: MEDIA first, PARTY fills the rest.
	// Only meaningful when kinds includes PAGE_FETCH.
	MediaQuota int `mapstructure:"media-quota" validate:"min=0"`

	// Buffer is the extra tasks claimed beyond quota to absorb rate-limited slots.
	// E.g. if MediaQuota=30 and Buffer=10, step 1 claims up to 40 MEDIA tasks.
	Buffer int `mapstructure:"buffer" validate:"min=0"`

	// RateLimitConfigPath is the path to the per-source rate limit YAML config.
	// If empty, a conservative default (1 req/s, burst 2) is used for all sources.
	RateLimitConfigPath string `mapstructure:"rate-limit-config"`
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

	fs.Duration("interval", 10*time.Minute, "The ticker interval for the scheduler (min: 1s, default: 10m)")
	fs.Int("health-port", 8090, "The port for the health check server (default: 8090)")
	fs.String("valkey-host", "localhost", "The host of the Valkey/Redis instance")
	fs.Int("valkey-port", 6379, "The port of the Valkey/Redis instance")
	fs.String("valkey-username", "", "The username for the Valkey/Redis instance")
	fs.String("valkey-password", "", "The password for the Valkey/Redis instance")
	fs.String("valkey-password-file", "", "Path to file containing the Valkey password (overrides --valkey-password and the env var)")
	fs.Int("valkey-db", 0, "The database index for the Valkey/Redis instance")

	// Postgres individual flags
	fs.String("pg-host", "localhost", "Postgres host")
	fs.Int("pg-port", 5432, "Postgres port")
	fs.String("pg-username", "postgres", "Postgres username")
	fs.String("pg-password", "postgres", "Postgres password")
	fs.String("pg-password-file", "", "Path to file containing the Postgres password (overrides --pg-password and the env var)")
	fs.String("pg-db", "prism", "Postgres database name")
	fs.String("pg-sslmode", "disable", "Postgres SSL mode (disable, require, etc.)")

	fs.String("nats-host", "localhost", "The NATS server host")
	fs.Int("nats-port", 4222, "The NATS server port")
	fs.String("nats-token", "", "The NATS server auth token")
	fs.String("nats-token-file", "", "Path to file containing the NATS auth token (overrides --nats-token and the env var)")
	fs.String("nats-password", "", "The NATS server password")
	fs.String("nats-password-file", "", "Path to file containing the NATS password (overrides --nats-password and the env var)")
	fs.String("queue-group", "", "Queue group for NATS subscribers (unused by scheduler publisher)")
	fs.Int("subscribers-count", 1, "Subscriber count for NATS consumers (unused by scheduler publisher)")
	fs.Duration("ack-wait-timeout", 30*time.Second, "Ack wait timeout for NATS subscribers (unused by scheduler publisher)")
	fs.Int64("channel-buffer", 100, "GoChannel output buffer size")
	fs.Bool("persistent", true, "Whether GoChannel should persist messages in memory")
	fs.String("log-path", "", "The file path for logs (empty for stdout)")
	fs.String("log-level", "info", "The log level (debug, info, warn, error)")
	fs.String("messenger-type", "nats", "The messenger backend type (nats, gochannel, default: nats)")
	fs.Int("batch-size", 100, "Number of tasks to claim per tick (max: 200, default: 100)")
	fs.StringSlice("kinds", []string{"DIRECTORY_FETCH", "KEYWORD_SEARCH"}, "Task kinds this scheduler instance will claim (comma-separated)")
	fs.String("lock-key", "", "Valkey lock key for this scheduler instance (derived from kinds if empty)")
	fs.Int("media-quota", 0, "Reserved PAGE_FETCH+MEDIA slots per tick; 0 disables priority split")
	fs.Int("buffer", 10, "Extra tasks to over-claim per step to absorb rate-limited slots")
	fs.String("rate-limit-config", "", "Path to per-source rate limit YAML config (uses safe defaults if empty)")

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
	if err := config.Valkey.BindFlags(v, fs); err != nil {
		return nil, err
	}
	if err := config.Postgres.BindFlags(v, fs); err != nil {
		return nil, err
	}
	if err := config.Logger.BindFlags(v, fs); err != nil {
		return nil, err
	}
	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Resolve file-based secrets before validation so the required-Password
	// check sees the file-derived value when running under prod overlay.
	if err := config.Postgres.ResolveSecrets(); err != nil {
		return nil, fmt.Errorf("postgres secrets: %w", err)
	}
	if err := config.Valkey.ResolveSecrets(); err != nil {
		return nil, fmt.Errorf("valkey secrets: %w", err)
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
		if err := natsCfg.ResolveSecrets(); err != nil {
			return nil, fmt.Errorf("nats secrets: %w", err)
		}
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
