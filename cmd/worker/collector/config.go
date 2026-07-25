package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/ChiaYuChang/prism/internal/appconfig"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/go-playground/validator/v10"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

type Config struct {
	Health            obs.HealthConfig          `mapstructure:"health"`
	ShutdownTimeout   time.Duration             `mapstructure:"shutdown-timeout"    validate:"required,min=1s"`
	Logger            obs.LoggingConfig         `mapstructure:"logger"`
	Telemetry         obs.TelemetryConfig       `mapstructure:"telemetry"`
	HTTPTimeout       time.Duration             `mapstructure:"http-timeout"        validate:"required,min=1s"`
	MaxProcessingTime time.Duration             `mapstructure:"max-processing-time" validate:"required,min=1s"`
	RetryMax          int                       `mapstructure:"retry-max"           validate:"required,min=1"`
	Postgres          appconfig.PostgresConfig  `mapstructure:"postgres"`
	S3                appconfig.S3Config        `mapstructure:"s3"`
	MessengerType     string                    `mapstructure:"messenger-type"      validate:"oneof=nats gochannel"`
	Messenger         appconfig.MessengerConfig `mapstructure:"-"`

	// Archive is the archive destination URI: "file:///path" for local or
	// "s3://bucket/prefix" for S3. When empty, archiving on Minify failure is disabled.
	Archive           string `mapstructure:"archive"`
	ParsersConfigPath string `mapstructure:"parsers-config"`

	// Prompt, when non-empty, overrides the parsers.yaml
	// fallback.prompt_file path. Useful for one-off operator overrides
	// without editing the baked parsers.yaml.
	Prompt string `mapstructure:"prompt"`

	// PromptStorage is the URI for DB-managed fallback prompt objects.
	PromptStorage string `mapstructure:"prompt-storage" validate:"required"`

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
	obs.RegisterHealthFlags(fs, obs.DefaultHealthConfig(8093))
	fs.Duration("shutdown-timeout", 2*time.Minute, "Graceful shutdown drain timeout")
	obs.RegisterLoggingFlags(fs, obs.DefaultLoggingConfig("prism.worker.collector"))
	obs.RegisterTelemetryFlags(fs, obs.DefaultTelemetryConfig("prism.worker.collector"))
	appconfig.RegisterMessengerFlags(fs, "collector-worker")

	fs.Duration("http-timeout", 30*time.Second, "HTTP timeout for page fetch requests")
	fs.Duration("max-processing-time", 2*time.Minute, "Maximum wall-clock time for handling a single message (ctx timeout passed to handler)")
	fs.Int("retry-max", repo.DefaultTaskRetryMax, "Maximum total task attempts before terminal failure")
	fs.String("archive", "", "Archive URI for error payloads (file:///path or s3://bucket/prefix); empty disables archiving")
	fs.String("parsers-config", "configs/worker/collector/parsers.yaml", "Path to the parsers configuration file (YAML)")
	fs.String("prompt", "", "Override path to the LLM fallback system-instruction file (defaults to fallback.prompt_file in parsers.yaml)")
	fs.String("prompt-storage", "file://runtime/prompts", "Storage URI for DB-managed fallback prompt objects")
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
	fs.String("s3-secret-key-file", "", "Path to file containing the S3 secret key")
	fs.Bool("s3-use-path-style", true, "Use path style addressing (required for SeaweedFS/MinIO)")

	if err := fs.Parse(args); err != nil {
		return nil, fmt.Errorf("failed to parse flags: %w", err)
	}

	configPath, _ := fs.GetString("config")
	if configPath != "" {
		if err := appconfig.ReadConfigFile(v, configPath); err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	}

	if err := v.BindPFlags(fs); err != nil {
		return nil, fmt.Errorf("failed to bind flags: %w", err)
	}
	if err := obs.BindHealthFlags(v, fs); err != nil {
		return nil, fmt.Errorf("failed to bind health flags: %w", err)
	}
	var config Config
	if err := config.Postgres.BindFlags(v, fs); err != nil {
		return nil, err
	}
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
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
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

	if err := config.Postgres.ResolveSecrets(); err != nil {
		return nil, fmt.Errorf("postgres secrets: %w", err)
	}
	if err := config.S3.ResolveSecrets(); err != nil {
		return nil, fmt.Errorf("s3 secrets: %w", err)
	}

	msgrCfg, err := appconfig.LoadMessengerConfig(v)
	if err != nil {
		return nil, err
	}
	config.Messenger = msgrCfg

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
