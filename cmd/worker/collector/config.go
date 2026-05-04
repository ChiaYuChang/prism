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

type Config struct {
	HealthPort        int                       `mapstructure:"health-port"         validate:"required,min=1024,max=65535"`
	Logger            appconfig.LoggerConfig    `mapstructure:"logger"`
	HTTPTimeout       time.Duration             `mapstructure:"http-timeout"        validate:"required,min=1s"`
	MaxProcessingTime time.Duration             `mapstructure:"max-processing-time" validate:"required,min=1s"`
	Postgres          appconfig.PostgresConfig  `mapstructure:"postgres"`
	S3                appconfig.S3Config        `mapstructure:"s3"`
	MessengerType     string                    `mapstructure:"messenger-type"      validate:"oneof=nats gochannel"`
	Messenger         appconfig.MessengerConfig `mapstructure:"-"`

	// Archive is the archive destination URI: "file:///path" for local or
	// "s3://bucket/prefix" for S3. When empty, archiving on Minify failure is disabled.
	Archive           string `mapstructure:"archive"`
	ParsersConfigPath string `mapstructure:"parsers-config"`

	// CaptureDir, when non-empty, tees successful HTTP response bodies into
	// <dir>/<host>/<path>. Dev-only; used to build local fixtures during
	// the integration test plan Phase 1 real-site run.
	CaptureDir string `mapstructure:"capture-dir"`

	// FixtureBase, when non-empty, rewrites outbound HTTP requests to the
	// fixture-server at this URL (e.g. http://localhost:9999) so the worker
	// runs against captured fixtures without touching real sites. Mutually
	// exclusive with CaptureDir; integration test plan Phase 2.
	FixtureBase string `mapstructure:"fixture-base"`

	// ForceMinifyError, when true, replaces the real minifier with a shim
	// that always errors. Dev-only; integration test plan Phase 3 — exercises
	// the errorSaver / cmd/recover replay path.
	ForceMinifyError bool `mapstructure:"force-minify-error"`
}

func LoadConfig(args []string) (*Config, error) {
	v := viper.New()
	v.SetEnvPrefix("PRISM_COLLECTOR_WORKER")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	v.AutomaticEnv()

	fs := pflag.NewFlagSet("worker-collector", pflag.ContinueOnError)
	fs.StringP("config", "c", "", "Path to the configuration file (YAML or JSON)")
	fs.Int("health-port", 8093, "The port for the health check server")
	fs.String("log-path", "", "The file path for logs (empty for stdout)")
	fs.String("log-level", "info", "The log level (debug, info, warn, error)")
	fs.String("messenger-type", "nats", "The messenger backend type (nats, gochannel)")
	fs.Duration("http-timeout", 30*time.Second, "HTTP timeout for page fetch requests")
	fs.Duration("max-processing-time", 2*time.Minute, "Maximum wall-clock time for handling a single message (ctx timeout passed to handler)")
	fs.String("archive", "", "Archive URI for error payloads (file:///path or s3://bucket/prefix); empty disables archiving")
	fs.String("parsers-config", "internal/collector/parser/config/parsers.yaml", "Path to the parsers configuration file (YAML)")
	fs.String("capture-dir", "", "Dev-only: tee successful response bodies to <dir>/<host>/<path> for fixture capture")
	fs.String("fixture-base", "", "Dev-only: rewrite outbound requests to this fixture-server URL (mutually exclusive with --capture-dir)")
	fs.Bool("force-minify-error", false, "Dev-only: replace minifier with always-failing shim to exercise errorSaver / cmd/recover (Phase 3)")

	fs.String("pg-host", "localhost", "Postgres host")
	fs.Int("pg-port", 5432, "Postgres port")
	fs.String("pg-username", "postgres", "Postgres username")
	fs.String("pg-password", "postgres", "Postgres password")
	fs.String("pg-password-file", "", "Path to file containing the Postgres password (overrides --pg-password and the env var)")
	fs.String("pg-db", "prism", "Postgres database name")
	fs.String("pg-sslmode", "disable", "Postgres SSL mode")

	fs.String("s3-endpoint", "", "S3 endpoint URL (leave empty for AWS; set for SeaweedFS/MinIO e.g. http://localhost:8333)")
	fs.String("s3-region", "us-east-1", "S3 region")
	fs.String("s3-access-key", "", "S3 access key (empty uses AWS SDK default credential chain)")
	fs.String("s3-secret-key", "", "S3 secret key (empty uses AWS SDK default credential chain)")
	fs.Bool("s3-use-path-style", true, "Use path style addressing (required for SeaweedFS/MinIO)")

	fs.String("nats-host", "localhost", "The NATS server host")
	fs.Int("nats-port", 4222, "The NATS server port")
	fs.String("nats-token", "", "The NATS server auth token")
	fs.String("nats-token-file", "", "Path to file containing the NATS auth token (overrides --nats-token and the env var)")
	fs.String("nats-password", "", "The NATS server password")
	fs.String("nats-password-file", "", "Path to file containing the NATS password (overrides --nats-password and the env var)")
	fs.String("queue-group", "collector-worker", "Queue group for worker subscriptions")
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
	if err := config.S3.BindFlags(v, fs); err != nil {
		return nil, err
	}
	if err := config.Logger.BindFlags(v, fs); err != nil {
		return nil, err
	}
	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if err := config.Postgres.ResolveSecrets(); err != nil {
		return nil, fmt.Errorf("postgres secrets: %w", err)
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
		if err := natsCfg.ResolveSecrets(); err != nil {
			return nil, fmt.Errorf("nats secrets: %w", err)
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

	if config.CaptureDir != "" && config.FixtureBase != "" {
		return nil, fmt.Errorf("--capture-dir and --fixture-base are mutually exclusive")
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
