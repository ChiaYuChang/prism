package main

import (
	"errors"
	"fmt"
	"os"
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
	Token TokenAuthConfig `mapstructure:"token"`
}

// TokenAuthConfig configures X-PRISM-TOKEN allow-list authentication.
type TokenAuthConfig struct {
	Tokens []string `mapstructure:"tokens"`
	File   string   `mapstructure:"file"`
}

func (c TokenAuthConfig) Enabled() bool {
	if strings.TrimSpace(c.File) != "" {
		return true
	}
	for _, token := range c.Tokens {
		if strings.TrimSpace(token) != "" {
			return true
		}
	}
	return false
}

func (c TokenAuthConfig) TokenSet() (map[string]struct{}, error) {
	if !c.Enabled() {
		return nil, nil
	}

	tokens := make(map[string]struct{})
	addTokens(tokens, c.Tokens)

	file := strings.TrimSpace(c.File)
	if file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("read auth token file %q: %w", file, err)
		}
		addTokens(tokens, strings.Split(string(b), "\n"))
	}
	if len(tokens) == 0 {
		return nil, fmt.Errorf("auth token config has no usable tokens")
	}
	return tokens, nil
}

func addTokens(dst map[string]struct{}, tokens []string) {
	for _, token := range tokens {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		dst[token] = struct{}{}
	}
}

// Config is the runtime configuration for the API server.
type Config struct {
	Port            int                 `mapstructure:"port"              validate:"required,min=1024,max=65535"`
	ReadTimeout     time.Duration       `mapstructure:"read-timeout"      validate:"required,min=1s"`
	WriteTimeout    time.Duration       `mapstructure:"write-timeout"     validate:"required,min=1s"`
	ShutdownTimeout time.Duration       `mapstructure:"shutdown-timeout"  validate:"required,min=1s"`
	CORSOrigins     []string            `mapstructure:"cors-origins"`
	Logger          obs.LoggingConfig   `mapstructure:"logger"`
	Telemetry       obs.TelemetryConfig `mapstructure:"telemetry"`
	Postgres        app.PostgresConfig  `mapstructure:"postgres"`
	Valkey          app.ValkeyConfig    `mapstructure:"valkey"`
	Cache           CacheConfig         `mapstructure:"cache"`
	RateLimit       RateLimitConfig     `mapstructure:"rate-limit"`
	Auth            AuthConfig          `mapstructure:"auth"`
	Monitoring      MonitoringConfig    `mapstructure:"monitoring"`
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
	Mode         string                      `mapstructure:"mode"             validate:"required,oneof=pull push"`
	Interval     time.Duration               `mapstructure:"interval"         validate:"required,min=1s"`
	Timeout      time.Duration               `mapstructure:"timeout"          validate:"required,min=100ms"`
	Targets      map[string]MonitoringTarget `mapstructure:"targets"`
	InternalPort int                         `mapstructure:"internal-port"    validate:"required_if=Mode push,omitempty,min=1024,max=65535"`
}

func LoadConfig(args []string) (*Config, error) {
	v := viper.New()
	v.SetEnvPrefix("PRISM_API")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	v.AutomaticEnv()

	v.SetDefault("monitoring.mode", "pull")
	v.SetDefault("monitoring.interval", 10*time.Second)
	v.SetDefault("monitoring.timeout", 2*time.Second)
	v.SetDefault("monitoring.internal-port", 8089)

	fs := pflag.NewFlagSet("api-server", pflag.ContinueOnError)
	fs.StringP("config", "c", "", "Path to the configuration file (YAML or JSON)")

	fs.Int("port", 8090, "HTTP listen port")
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

	fs.Bool("cache-enabled", false, "Enable Valkey-backed progress cache for GET /fetches/{id}")
	fs.Duration("cache-live-ttl", 2*time.Second, "Progress cache TTL for non-terminal responses")
	fs.Duration("cache-terminal-ttl", 60*time.Second, "Progress cache TTL for terminal responses")

	fs.Bool("rate-limit-enabled", false, "Enable per-IP rate limit on GET /fetches/{id}")
	fs.Float64("rate-limit-rps", 5, "Per-IP requests-per-second budget")
	fs.Int("rate-limit-burst", 10, "Per-IP burst capacity")
	fs.Int("rate-limit-ip-cache-size", 4096, "Max distinct IPs tracked by the rate limiter (LRU)")

	fs.StringSlice("auth-token", []string{}, "Allowed X-PRISM-TOKEN values (comma-separated or repeated)")
	fs.String("auth-token-file", "", "Path to allowed X-PRISM-TOKEN file (one token per line)")

	fs.String("monitoring-mode", "pull", "Monitoring mode: pull or push")
	fs.Duration("monitoring-interval", 10*time.Second, "Interval to ping worker/app health endpoints in pull mode")
	fs.Duration("monitoring-timeout", 2*time.Second, "Timeout for monitoring pings")

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
	if err := bindCacheFlags(v, fs); err != nil {
		return nil, err
	}
	if err := bindRateLimitFlags(v, fs); err != nil {
		return nil, err
	}
	if err := bindAuthFlags(v, fs); err != nil {
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

	if cfg.Cache.Enabled {
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
		"auth-token":      "auth.token.tokens",
		"auth-token-file": "auth.token.file",
	} {
		if err := v.BindPFlag(key, fs.Lookup(flag)); err != nil {
			return fmt.Errorf("bind %s: %w", key, err)
		}
	}
	return nil
}

func bindMonitoringFlags(v *viper.Viper, fs *pflag.FlagSet) error {
	for flag, key := range map[string]string{
		"monitoring-mode":          "monitoring.mode",
		"monitoring-interval":      "monitoring.interval",
		"monitoring-timeout":       "monitoring.timeout",
		"monitoring-internal-port": "monitoring.internal-port",
	} {
		if err := v.BindPFlag(key, fs.Lookup(flag)); err != nil {
			return fmt.Errorf("bind %s: %w", key, err)
		}
	}
	return nil
}
