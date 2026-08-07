package main

import (
	"fmt"
	"strings"
	"time"

	app "github.com/ChiaYuChang/prism/internal/appconfig"
	"github.com/ChiaYuChang/prism/internal/discovery/planner"
	searchconfig "github.com/ChiaYuChang/prism/internal/discovery/search/config"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/internal/prompt"
	"github.com/go-playground/validator/v10"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	DefaultConfigPath = "configs/worker/discovery/planner/config.yaml"
	DefaultPromptPath = "assets/runtime/worker/discovery/planner/extractor_v2.md"
)

type Config struct {
	Health           obs.HealthConfig    `mapstructure:"health"`
	ShutdownTimeout  time.Duration       `mapstructure:"shutdown-timeout" validate:"required,min=1s"`
	Logger           obs.LoggingConfig   `mapstructure:"logger"`
	Telemetry        obs.TelemetryConfig `mapstructure:"telemetry"`
	Postgres         app.PostgresConfig  `mapstructure:"postgres"`
	S3               app.S3Config        `mapstructure:"s3"`
	MessengerType    string              `mapstructure:"messenger-type" validate:"oneof=nats gochannel"`
	Messenger        app.MessengerConfig `mapstructure:"-"`
	LLM              app.LLMConfig       `mapstructure:"llm"`
	Prompt           prompt.Ref          `mapstructure:"prompt"`
	PromptStorageURI string              `mapstructure:"prompt-storage" validate:"required"`
	PromptPath       string              `mapstructure:"prompt-path"    validate:"required"`
	Search           searchconfig.Config `mapstructure:"search"`
	MaxSearchTasks   int                 `mapstructure:"max-search-tasks" validate:"required,min=1,max=1000"`
}

func LoadConfig(args []string) (*Config, error) {
	v := viper.New()
	v.SetEnvPrefix("PRISM_PLANNER_WORKER")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	v.AutomaticEnv()

	fs := pflag.NewFlagSet("worker-planner", pflag.ContinueOnError)
	fs.StringP("config", "c", "", "Path to the configuration file (YAML or JSON)")
	obs.RegisterHealthFlags(fs, obs.DefaultHealthConfig(8094))
	fs.Duration("shutdown-timeout", 3*time.Minute, "Graceful shutdown drain timeout")
	fs.Int("max-search-tasks", planner.DefaultMaxSearchTasks, "Maximum keyword-search tasks created per completed batch")

	obs.RegisterLoggingFlags(fs, obs.DefaultLoggingConfig("prism.worker.planner"))
	obs.RegisterTelemetryFlags(fs, obs.DefaultTelemetryConfig("prism.worker.planner"))
	app.RegisterMessengerFlags(fs, "planner-worker")

	fs.String("pg-host", "localhost", "Postgres host")
	fs.Int("pg-port", 5432, "Postgres port")
	fs.String("pg-username", "postgres", "Postgres username")
	fs.String("pg-password", "postgres", "Postgres password")
	fs.String("pg-db", "prism", "Postgres database name")
	fs.String("pg-sslmode", "disable", "Postgres SSL mode")

	fs.String("llm-key", "", "LLM API key")
	fs.String("llm-key-file", "", "Path to a file containing the LLM API key")
	fs.String("llm-model", "", "LLM model name")
	fs.Duration("llm-timeout", 2*time.Minute, "LLM request timeout")

	fs.String("prompt-path", DefaultPromptPath, "Path to the extractor prompt file")
	fs.String("prompt-storage", "file://runtime/prompts", "Storage URI containing hash-addressed prompt objects")
	fs.String("s3-endpoint", "", "S3 endpoint URL")
	fs.String("s3-region", "us-east-1", "S3 region")
	fs.String("s3-access-key", "", "S3 access key")
	fs.String("s3-secret-key", "", "S3 secret key")
	fs.String("s3-secret-key-file", "", "Path to file containing the S3 secret key")
	fs.Bool("s3-use-path-style", true, "Use path style addressing")
	fs.Bool("search-target-yahoo-enable", false, "Enable Yahoo News keyword-search target")
	fs.String("search-target-yahoo-source-abbr", "yahoo", "Yahoo News source abbreviation for search candidates")
	fs.String("search-target-yahoo-url", "https://tw.news.yahoo.com", "Yahoo News target URL")
	fs.String("search-target-yahoo-site", "tw.news.yahoo.com", "Yahoo News site filter")

	if err := fs.Parse(args); err != nil {
		return nil, fmt.Errorf("failed to parse flags: %w", err)
	}

	configPath, _ := fs.GetString("config")
	if configPath != "" {
		if err := app.ReadConfigFile(v, configPath); err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	}

	if err := v.BindPFlags(fs); err != nil {
		return nil, fmt.Errorf("failed to bind flags: %w", err)
	}
	if err := obs.BindHealthFlags(v, fs); err != nil {
		return nil, fmt.Errorf("failed to bind health flags: %w", err)
	}
	if err := bindSearchFlags(v, fs); err != nil {
		return nil, err
	}

	var config Config
	if err := config.Postgres.BindFlags(v, fs); err != nil {
		return nil, err
	}
	if err := obs.BindLoggingFlags(v, fs); err != nil {
		return nil, err
	}
	if err := obs.BindTelemetryFlags(v, fs); err != nil {
		return nil, err
	}
	if err := config.LLM.BindFlags(v, fs); err != nil {
		return nil, err
	}
	if err := config.S3.BindFlags(v, fs); err != nil {
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
	if err := config.LLM.ResolveSecrets(); err != nil {
		return nil, fmt.Errorf("resolve LLM secrets: %w", err)
	}
	if err := config.S3.ResolveSecrets(); err != nil {
		return nil, fmt.Errorf("resolve S3 secrets: %w", err)
	}

	msgrCfg, err := app.LoadMessengerConfig(v)
	if err != nil {
		return nil, err
	}
	config.Messenger = msgrCfg

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

func bindSearchFlags(v *viper.Viper, fs *pflag.FlagSet) error {
	bindings := map[string]string{
		"search.targets.yahoo.enable":      "search-target-yahoo-enable",
		"search.targets.yahoo.source_abbr": "search-target-yahoo-source-abbr",
		"search.targets.yahoo.url":         "search-target-yahoo-url",
		"search.targets.yahoo.site":        "search-target-yahoo-site",
	}
	for key, flag := range bindings {
		if err := v.BindPFlag(key, fs.Lookup(flag)); err != nil {
			return fmt.Errorf("failed to bind %s: %w", flag, err)
		}
	}
	return nil
}
