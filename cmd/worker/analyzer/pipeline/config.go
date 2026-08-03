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
	Health           obs.HealthConfig          `mapstructure:"health"`
	ShutdownTimeout  time.Duration             `mapstructure:"shutdown-timeout" validate:"required,min=1s"`
	RetryMax         int                       `mapstructure:"retry-max" validate:"required,min=1"`
	PipelineFile     string                    `mapstructure:"pipeline-file" validate:"required"`
	ReportStorageURI string                    `mapstructure:"report-storage-uri" validate:"required"`
	ReportCacheTTL   time.Duration             `mapstructure:"report-cache-ttl" validate:"required,min=1s"`
	Logger           obs.LoggingConfig         `mapstructure:"logger"`
	Telemetry        obs.TelemetryConfig       `mapstructure:"telemetry"`
	Postgres         appconfig.PostgresConfig  `mapstructure:"postgres"`
	S3               appconfig.S3Config        `mapstructure:"s3"`
	MessengerType    string                    `mapstructure:"messenger-type" validate:"oneof=nats gochannel"`
	Messenger        appconfig.MessengerConfig `mapstructure:"-"`
}

const deployedPipelineFile = "configs/llm_pipeline.yaml"

func LoadConfig(args []string) (*Config, error) {
	v := viper.New()
	v.SetEnvPrefix("PRISM_ANALYZER_PIPELINE_WORKER")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	v.AutomaticEnv()

	fs := pflag.NewFlagSet("worker-analyzer-pipeline", pflag.ContinueOnError)
	fs.StringP("config", "c", "", "Path to the configuration file (YAML or JSON)")
	obs.RegisterHealthFlags(fs, obs.DefaultHealthConfig(8097))
	fs.Duration("shutdown-timeout", 2*time.Minute, "Graceful shutdown drain timeout")
	fs.Int("retry-max", repo.DefaultTaskRetryMax, "Maximum total task attempts before terminal failure")
	fs.String("pipeline-file", "configs/llm_pipeline.yaml", "Pipeline definition file")
	fs.String("report-storage-uri", "file://runtime/reports", "Storage URI for generated report artifacts")
	fs.Duration("report-cache-ttl", 24*time.Hour, "Report artifact cache lifetime")
	obs.RegisterLoggingFlags(fs, obs.DefaultLoggingConfig("prism.worker.analyzer.pipeline"))
	obs.RegisterTelemetryFlags(fs, obs.DefaultTelemetryConfig("prism.worker.analyzer.pipeline"))
	appconfig.RegisterMessengerFlags(fs, "analyzer-pipeline-worker")
	fs.String("pg-host", "localhost", "Postgres host")
	fs.Int("pg-port", 5432, "Postgres port")
	fs.String("pg-username", "postgres", "Postgres username")
	fs.String("pg-password", "postgres", "Postgres password")
	fs.String("pg-password-file", "", "Path to file containing the Postgres password")
	fs.String("pg-db", "prism", "Postgres database name")
	fs.String("pg-sslmode", "disable", "Postgres SSL mode")
	fs.String("s3-endpoint", "", "S3 endpoint URL")
	fs.String("s3-region", "us-east-1", "S3 region")
	fs.String("s3-access-key", "", "S3 access key")
	fs.String("s3-secret-key", "", "S3 secret key")
	fs.String("s3-secret-key-file", "", "Path to file containing the S3 secret key")
	fs.Bool("s3-use-path-style", true, "Use path style addressing")

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
		return nil, err
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
	var err error
	if config.Logger, err = obs.LoadLoggingConfig(v); err != nil {
		return nil, err
	}
	if config.Telemetry, err = obs.LoadTelemetryConfig(v); err != nil {
		return nil, err
	}
	if err := config.Postgres.ResolveSecrets(); err != nil {
		return nil, fmt.Errorf("resolve Postgres secrets: %w", err)
	}
	if err := config.S3.ResolveSecrets(); err != nil {
		return nil, fmt.Errorf("resolve S3 secrets: %w", err)
	}
	config.Messenger, err = appconfig.LoadMessengerConfig(v)
	if err != nil {
		return nil, err
	}
	validate := validator.New()
	if err := validate.Struct(&config); err != nil {
		return nil, fmt.Errorf("config validation failed: %v", err)
	}
	if err := validate.Struct(config.Messenger); err != nil {
		return nil, fmt.Errorf("messenger config validation failed: %v", err)
	}
	if config.PipelineFile != deployedPipelineFile {
		return nil, fmt.Errorf("pipeline-file must be %q", deployedPipelineFile)
	}
	return &config, nil
}
