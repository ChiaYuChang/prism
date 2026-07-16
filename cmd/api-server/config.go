package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	app "github.com/ChiaYuChang/prism/internal/appconfig"
	"github.com/ChiaYuChang/prism/internal/http/api"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/go-playground/validator/v10"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// CacheConfig toggles and tunes the GET /fetches/{id} progress cache.
type CacheConfig struct {
	Enabled     bool          `mapstructure:"enabled"`
	LiveTTL     time.Duration `mapstructure:"live-ttl"     validate:"min=0"`
	TerminalTTL time.Duration `mapstructure:"terminal-ttl" validate:"min=0"`
}

// RateLimitConfig toggles and tunes the per-IP rate limit on GET /fetches/{id}.
type RateLimitConfig struct {
	Enabled     bool    `mapstructure:"enabled"`
	RPS         float64 `mapstructure:"rps"            validate:"min=0"`
	Burst       int     `mapstructure:"burst"          validate:"min=0"`
	IPCacheSize int     `mapstructure:"ip-cache-size"  validate:"min=0"`
}

// AuthConfig groups API authentication methods. JWT can be added alongside
// token auth without changing middleware wiring.
type AuthConfig struct {
	HashAlgorithm string                     `mapstructure:"hash-algorithm" validate:"required"`
	TokenTypes    map[string]TokenTypeConfig `mapstructure:"token-types"`
}

type TokenTypeConfig struct {
	Prefix     string        `mapstructure:"prefix"      validate:"required"`
	DefaultTTL time.Duration `mapstructure:"default-ttl" validate:"required,min=1s"`
	MaxTTL     time.Duration `mapstructure:"max-ttl"     validate:"required,min=1s"`
}

type PromptConfig struct {
	StorageURI string `mapstructure:"storage-uri" validate:"required"`
}

// Config is the runtime configuration for the API server.
type Config struct {
	Port             int                    `mapstructure:"port"              validate:"required,min=1024,max=65535"`
	Admin            AdminConfig            `mapstructure:"admin"`
	ReadTimeout      time.Duration          `mapstructure:"read-timeout"      validate:"required,min=1s"`
	WriteTimeout     time.Duration          `mapstructure:"write-timeout"     validate:"required,min=1s"`
	ShutdownTimeout  time.Duration          `mapstructure:"shutdown-timeout"  validate:"required,min=1s"`
	CORSOrigins      []string               `mapstructure:"cors-origins"`
	Logger           obs.LoggingConfig      `mapstructure:"logger"`
	Telemetry        obs.TelemetryConfig    `mapstructure:"telemetry"`
	Postgres         app.PostgresConfig     `mapstructure:"postgres"`
	S3               app.S3Config           `mapstructure:"s3"`
	Valkey           app.ValkeyConfig       `mapstructure:"valkey"`
	NATS             app.NatsConfig         `mapstructure:"nats"`
	Cache            CacheConfig            `mapstructure:"cache"`
	RateLimit        RateLimitConfig        `mapstructure:"rate-limit"`
	Auth             AuthConfig             `mapstructure:"auth"`
	Prompts          PromptConfig           `mapstructure:"prompts"`
	Monitoring       MonitoringConfig       `mapstructure:"monitoring"`
	SchedulerControl SchedulerControlConfig `mapstructure:"scheduler-control"`
}

type SchedulerControlConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

type AdminConfig struct {
	Enabled bool `mapstructure:"enabled"`
	Port    int  `mapstructure:"port" validate:"required,min=1024,max=65535"`
}

type MonitoringTarget struct {
	Enabled           *bool `mapstructure:"enabled"`
	api.MonitorTarget `mapstructure:",squash"`
}

func (t MonitoringTarget) IsEnabled() bool {
	return t.Enabled == nil || *t.Enabled
}

func (t MonitoringTarget) Normalized(defaultTimeout time.Duration) MonitoringTarget {
	return MonitoringTarget{
		Enabled: t.Enabled,
		MonitorTarget: api.MonitorTarget{
			URL:         t.URL,
			DisplayName: t.DisplayName,
			Description: t.Description,
			Group:       t.Group,
			Timeout:     t.Timeout,
		}.Normalized(defaultTimeout),
	}
}

type MonitoringConfig struct {
	Backend      string                      `mapstructure:"backend"          validate:"required,oneof=memory valkey"`
	Mode         string                      `mapstructure:"mode"             validate:"required,oneof=pull push"`
	Interval     time.Duration               `mapstructure:"interval"         validate:"required,min=1s"`
	Timeout      time.Duration               `mapstructure:"timeout"          validate:"required,min=100ms"`
	Targets      map[string]MonitoringTarget `mapstructure:"targets"`
	StatusKey    string                      `mapstructure:"status-key"`
	InternalPort int                         `mapstructure:"internal-port"    validate:"required_if=Mode push,omitempty,min=1024,max=65535"`
}

func LoadConfig(args []string) (*Config, error) {
	v := viper.New()
	v.SetEnvPrefix("PRISM_API")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	v.AutomaticEnv()

	v.SetDefault("monitoring.mode", "pull")
	v.SetDefault("monitoring.backend", "memory")
	v.SetDefault("monitoring.interval", 10*time.Second)
	v.SetDefault("monitoring.timeout", 2*time.Second)
	v.SetDefault("monitoring.status-key", "api:status")
	v.SetDefault("monitoring.internal-port", 8089)
	v.SetDefault("admin.enabled", true)
	v.SetDefault("admin.port", 8091)
	v.SetDefault("prompts.storage-uri", "file://runtime/prompts")
	v.SetDefault("nats.nats-host", "nats")
	v.SetDefault("nats.nats-port", 4222)
	v.SetDefault("auth.hash-algorithm", "sha256")
	v.SetDefault("auth.token-types.admin.prefix", "padm")
	v.SetDefault("auth.token-types.admin.default-ttl", 720*time.Hour)
	v.SetDefault("auth.token-types.admin.max-ttl", 2160*time.Hour)
	v.SetDefault("auth.token-types.user.prefix", "pusr")
	v.SetDefault("auth.token-types.user.default-ttl", 24*time.Hour)
	v.SetDefault("auth.token-types.user.max-ttl", 168*time.Hour)
	v.SetDefault("auth.token-types.worker.prefix", "pwrk")
	v.SetDefault("auth.token-types.worker.default-ttl", 720*time.Hour)
	v.SetDefault("auth.token-types.worker.max-ttl", 2160*time.Hour)

	fs := pflag.NewFlagSet("api-server", pflag.ContinueOnError)
	fs.StringP("config", "c", "", "Path to the configuration file (YAML or JSON)")

	fs.Int("port", 8090, "HTTP listen port")
	fs.Bool("admin-enabled", true, "Enable admin HTTP listener")
	fs.Int("admin-port", 8091, "Admin HTTP listen port")
	fs.Int("monitoring-internal-port", 8089, "HTTP listen port for internal administration and status updates")
	fs.Duration("read-timeout", 10*time.Second, "HTTP server read timeout")
	fs.Duration("write-timeout", 30*time.Second, "HTTP server write timeout")
	fs.Duration("shutdown-timeout", 10*time.Second, "Graceful shutdown timeout")
	fs.StringSlice("cors-origins", []string{}, "Allowed CORS origins (comma-separated; empty disables CORS)")

	obs.RegisterLoggingFlags(fs, obs.DefaultLoggingConfig("prism.api-server"))
	obs.RegisterTelemetryFlags(fs, obs.DefaultTelemetryConfig("prism.api-server"))

	fs.String("pg-host", "localhost", "Postgres host")
	fs.Int("pg-port", 5432, "Postgres port")
	fs.String("pg-username", "postgres", "Postgres username")
	fs.String("pg-password", "postgres", "Postgres password")
	fs.String("pg-db", "prism", "Postgres database name")
	fs.String("pg-sslmode", "disable", "Postgres SSL mode")

	fs.String("valkey-host", "localhost", "Valkey/Redis host (used only when --cache-enabled)")
	fs.Int("valkey-port", 6379, "Valkey/Redis port (used only when --cache-enabled)")
	fs.String("valkey-username", "", "Valkey/Redis username")
	fs.String("valkey-password", "", "Valkey/Redis password")
	fs.String("valkey-password-file", "", "Path to file containing the Valkey password")
	fs.Int("valkey-db", 0, "Valkey/Redis DB index")
	fs.String("nats-host", "nats", "NATS host")
	fs.Int("nats-port", 4222, "NATS client port")
	fs.String("nats-username", "", "NATS username")
	fs.String("nats-password", "", "NATS password")
	fs.String("nats-token", "", "NATS authentication token")
	fs.String("nats-password-file", "", "Path to file containing the NATS password")
	fs.String("nats-token-file", "", "Path to file containing the NATS token")

	fs.Bool("cache-enabled", false, "Enable Valkey-backed progress cache for GET /fetches/{id}")
	fs.Duration("cache-live-ttl", 2*time.Second, "Progress cache TTL for non-terminal responses")
	fs.Duration("cache-terminal-ttl", 60*time.Second, "Progress cache TTL for terminal responses")

	fs.Bool("rate-limit-enabled", false, "Enable per-IP rate limit on GET /fetches/{id}")
	fs.Float64("rate-limit-rps", 5, "Per-IP requests-per-second budget")
	fs.Int("rate-limit-burst", 10, "Per-IP burst capacity")
	fs.Int("rate-limit-ip-cache-size", 4096, "Max distinct IPs tracked by the rate limiter (LRU)")

	fs.String("auth-hash-algorithm", "sha256", "Token hash algorithm")
	fs.String("prompts-storage-uri", "file://runtime/prompts", "Storage URI for uploaded prompt objects")
	fs.String("s3-endpoint", "", "S3 endpoint URL")
	fs.String("s3-region", "us-east-1", "S3 region")
	fs.String("s3-access-key", "", "S3 access key")
	fs.String("s3-secret-key", "", "S3 secret key")
	fs.String("s3-secret-key-file", "", "Path to file containing the S3 secret key")
	fs.Bool("s3-use-path-style", true, "Use path style addressing")

	fs.String("monitoring-mode", "pull", "Monitoring mode: pull or push")
	fs.String("monitoring-backend", "memory", "Monitoring status backend: memory or valkey")
	fs.Duration("monitoring-interval", 10*time.Second, "Interval to ping worker/app health endpoints in pull mode")
	fs.Duration("monitoring-timeout", 2*time.Second, "Timeout for monitoring pings")
	fs.String("monitoring-status-key", "api:status", "Valkey hash key for monitoring statuses")

	if err := fs.Parse(args); err != nil {
		return nil, fmt.Errorf("failed to parse flags: %w", err)
	}

	if configPath, _ := fs.GetString("config"); configPath != "" {
		if err := app.ReadConfigFile(v, configPath); err != nil {
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
	if err := cfg.Valkey.BindFlags(v, fs); err != nil {
		return nil, err
	}
	if err := cfg.S3.BindFlags(v, fs); err != nil {
		return nil, err
	}
	if err := bindCacheFlags(v, fs); err != nil {
		return nil, err
	}
	if err := bindRateLimitFlags(v, fs); err != nil {
		return nil, err
	}
	if err := bindAuthFlags(v, fs); err != nil {
		return nil, err
	}
	if err := bindPromptFlags(v, fs); err != nil {
		return nil, err
	}
	if err := bindNATSFlags(v, fs); err != nil {
		return nil, err
	}
	if err := bindMonitoringFlags(v, fs); err != nil {
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
	if err := cfg.S3.ResolveSecrets(); err != nil {
		return nil, fmt.Errorf("s3 secrets: %w", err)
	}
	if err := cfg.NATS.ResolveSecrets(); err != nil {
		return nil, fmt.Errorf("nats secrets: %w", err)
	}

	if cfg.Cache.Enabled || cfg.Monitoring.Backend == "valkey" {
		if err := cfg.Valkey.ResolveSecrets(); err != nil {
			return nil, fmt.Errorf("valkey secrets: %w", err)
		}
	}

	// Normalize monitoring targets after load
	for k, target := range cfg.Monitoring.Targets {
		if target.Enabled == nil {
			t := true
			target.Enabled = &t
		}
		target = target.Normalized(cfg.Monitoring.Timeout)
		cfg.Monitoring.Targets[k] = target
	}

	validate := validator.New()
	if err := validate.Struct(&cfg); err != nil {
		return nil, fmt.Errorf("config validation failed: %v", err)
	}

	if err := validateMonitoringTargets(validate, cfg.Monitoring.Targets); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func validateMonitoringTargets(validate *validator.Validate, targets map[string]MonitoringTarget) error {
	errs := []error{}
	for name, target := range targets {
		if !target.IsEnabled() {
			continue
		}
		if err := validate.Struct(target.MonitorTarget); err != nil {
			errs = append(errs, fmt.Errorf("monitoring target %q validation failed: %w", name, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("monitoring target validation failed: %w", errors.Join(errs...))
	}
	return nil
}

func bindCacheFlags(v *viper.Viper, fs *pflag.FlagSet) error {
	for flag, key := range map[string]string{
		"cache-enabled":      "cache.enabled",
		"cache-live-ttl":     "cache.live-ttl",
		"cache-terminal-ttl": "cache.terminal-ttl",
	} {
		if err := v.BindPFlag(key, fs.Lookup(flag)); err != nil {
			return fmt.Errorf("bind %s: %w", key, err)
		}
	}
	return nil
}

func bindRateLimitFlags(v *viper.Viper, fs *pflag.FlagSet) error {
	for flag, key := range map[string]string{
		"rate-limit-enabled":       "rate-limit.enabled",
		"rate-limit-rps":           "rate-limit.rps",
		"rate-limit-burst":         "rate-limit.burst",
		"rate-limit-ip-cache-size": "rate-limit.ip-cache-size",
	} {
		if err := v.BindPFlag(key, fs.Lookup(flag)); err != nil {
			return fmt.Errorf("bind %s: %w", key, err)
		}
	}
	return nil
}

func bindAuthFlags(v *viper.Viper, fs *pflag.FlagSet) error {
	for flag, key := range map[string]string{
		"auth-hash-algorithm": "auth.hash-algorithm",
		"admin-enabled":       "admin.enabled",
		"admin-port":          "admin.port",
	} {
		if err := v.BindPFlag(key, fs.Lookup(flag)); err != nil {
			return fmt.Errorf("bind %s: %w", key, err)
		}
	}
	return nil
}

func bindPromptFlags(v *viper.Viper, fs *pflag.FlagSet) error {
	if err := v.BindPFlag("prompts.storage-uri", fs.Lookup("prompts-storage-uri")); err != nil {
		return fmt.Errorf("bind prompts.storage-uri: %w", err)
	}
	return nil
}

func bindNATSFlags(v *viper.Viper, fs *pflag.FlagSet) error {
	for flag, key := range map[string]string{
		"nats-host":          "nats.nats-host",
		"nats-port":          "nats.nats-port",
		"nats-username":      "nats.nats-username",
		"nats-password":      "nats.nats-password",
		"nats-token":         "nats.nats-token",
		"nats-password-file": "nats.nats-password-file",
		"nats-token-file":    "nats.nats-token-file",
	} {
		if err := v.BindPFlag(key, fs.Lookup(flag)); err != nil {
			return fmt.Errorf("bind %s: %w", key, err)
		}
	}
	return nil
}

func bindMonitoringFlags(v *viper.Viper, fs *pflag.FlagSet) error {
	for flag, key := range map[string]string{
		"monitoring-backend":       "monitoring.backend",
		"monitoring-mode":          "monitoring.mode",
		"monitoring-interval":      "monitoring.interval",
		"monitoring-timeout":       "monitoring.timeout",
		"monitoring-status-key":    "monitoring.status-key",
		"monitoring-internal-port": "monitoring.internal-port",
	} {
		if err := v.BindPFlag(key, fs.Lookup(flag)); err != nil {
			return fmt.Errorf("bind %s: %w", key, err)
		}
	}
	return nil
}
