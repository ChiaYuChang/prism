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

const defaultEmbeddingDimension = 768

type EmbedderSettings struct {
	appconfig.LLMConfig `mapstructure:",squash"`
	Dimension           int `mapstructure:"dimension" validate:"required,min=1"`
	RetryMax            int `mapstructure:"retry-max" validate:"required,min=1"`
}

type Config struct {
	HealthPort      int                       `mapstructure:"health-port"      validate:"required,min=1024,max=65535"`
	ShutdownTimeout time.Duration             `mapstructure:"shutdown-timeout" validate:"required,min=1s"`
	Embedder        EmbedderSettings          `mapstructure:"embedder"`
	Logger          obs.LoggingConfig         `mapstructure:"logger"`
	Telemetry       obs.TelemetryConfig       `mapstructure:"telemetry"`
	Postgres        appconfig.PostgresConfig  `mapstructure:"postgres"`
	MessengerType   string                    `mapstructure:"messenger-type"   validate:"oneof=nats gochannel"`
	Messenger       appconfig.MessengerConfig `mapstructure:"-"`
}

func LoadConfig(args []string) (*Config, error) {
	v := viper.New()
	v.SetEnvPrefix("PRISM_EMBEDDER_WORKER")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	v.AutomaticEnv()

	fs := pflag.NewFlagSet("worker-embedder", pflag.ContinueOnError)
	fs.StringP("config", "c", "", "Path to the configuration file (YAML or JSON)")
	fs.Int("health-port", 8095, "The port for the health check server")
	fs.Duration("shutdown-timeout", 2*time.Minute, "Graceful shutdown drain timeout")
	fs.Int("retry-max", repo.DefaultTaskRetryMax, "Maximum total task attempts before terminal failure")
	fs.Int("embedding-dimension", defaultEmbeddingDimension, "Expected vector dimension")
	obs.RegisterLoggingFlags(fs, obs.DefaultLoggingConfig("prism.worker.embedder"))
	obs.RegisterTelemetryFlags(fs, obs.DefaultTelemetryConfig("prism.worker.embedder"))
	appconfig.RegisterMessengerFlags(fs, "embedder-worker")

	fs.String("pg-host", "localhost", "Postgres host")
	fs.Int("pg-port", 5432, "Postgres port")
	fs.String("pg-username", "postgres", "Postgres username")
	fs.String("pg-password", "postgres", "Postgres password")
	fs.String("pg-password-file", "", "Path to file containing the Postgres password")
	fs.String("pg-db", "prism", "Postgres database name")
	fs.String("pg-sslmode", "disable", "Postgres SSL mode")

	fs.String("llm-key", "", "LLM API key")
	fs.String("llm-key-file", "", "Path to a file containing the LLM API key")
	fs.String("llm-model", "", "Embedding model name")
	fs.Duration("llm-timeout", 2*time.Minute, "LLM request timeout")

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
	_ = v.BindPFlag("embedder.dimension", fs.Lookup("embedding-dimension"))
	_ = v.BindPFlag("embedder.retry-max", fs.Lookup("retry-max"))

	var config Config
	if err := config.Postgres.BindFlags(v, fs); err != nil {
		return nil, err
	}
	if err := config.Embedder.LLMConfig.BindFlags(v, fs); err != nil {
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
	loggerConfig, err := obs.LoadLoggingConfig(v)
	if err != nil {
		return nil, err
	}
	config.Logger = loggerConfig
	telemetryConfig, err := obs.LoadTelemetryConfig(v)
	if err != nil {
		return nil, err
	}
	config.Telemetry = telemetryConfig
	if err := config.Postgres.ResolveSecrets(); err != nil {
		return nil, fmt.Errorf("resolve Postgres secrets: %w", err)
	}
	if err := config.Embedder.LLMConfig.ResolveSecrets(); err != nil {
		return nil, fmt.Errorf("resolve LLM secrets: %w", err)
	}

	msgrConfig, err := appconfig.LoadMessengerConfig(v)
	if err != nil {
		return nil, err
	}
	config.Messenger = msgrConfig

	validate := validator.New()
	if err := validate.Struct(&config); err != nil {
		return nil, fmt.Errorf("config validation failed: %v", err)
	}
	if err := validate.Struct(config.Messenger); err != nil {
		return nil, fmt.Errorf("messenger config validation failed: %v", err)
	}
	return &config, nil
}
