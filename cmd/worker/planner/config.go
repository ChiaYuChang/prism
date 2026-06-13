package main

import (
	"fmt"
	"strings"
	"time"

	app "github.com/ChiaYuChang/prism/internal/appconfig"
	searchconfig "github.com/ChiaYuChang/prism/internal/discovery/search/config"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/go-playground/validator/v10"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	DefaultPromptPath = "assets/worker/planner/prompts/analysis/extractor.md"
)

type Config struct {
	HealthPort    int                 `mapstructure:"health-port"    validate:"required,min=1024,max=65535"`
	Logger        obs.LoggingConfig   `mapstructure:"logger"`
	Telemetry     obs.TelemetryConfig `mapstructure:"telemetry"`
	Postgres      app.PostgresConfig  `mapstructure:"postgres"`
	MessengerType string              `mapstructure:"messenger-type" validate:"oneof=nats gochannel"`
	Messenger     app.MessengerConfig `mapstructure:"-"`
	LLM           app.LLMConfig       `mapstructure:"llm"`
	PromptPath    string              `mapstructure:"prompt-path"    validate:"required"`
	Search        searchconfig.Config `mapstructure:"search"`
}

func LoadConfig(args []string) (*Config, error) {
	v := viper.New()
	v.SetEnvPrefix("PRISM_PLANNER_WORKER")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	v.AutomaticEnv()

	fs := pflag.NewFlagSet("worker-planner", pflag.ContinueOnError)
	fs.StringP("config", "c", "", "Path to the configuration file (YAML or JSON)")
	fs.Int("health-port", 8094, "The port for the health check server")

	obs.RegisterLoggingFlags(fs, obs.DefaultLoggingConfig("prism.worker.planner"))
	obs.RegisterTelemetryFlags(fs, obs.DefaultTelemetryConfig("prism.worker.planner"))

	fs.String("pg-host", "localhost", "Postgres host")
	fs.Int("pg-port", 5432, "Postgres port")
	fs.String("pg-username", "postgres", "Postgres username")
	fs.String("pg-password", "postgres", "Postgres password")
	fs.String("pg-db", "prism", "Postgres database name")
	fs.String("pg-sslmode", "disable", "Postgres SSL mode")

	fs.String("messenger-type", "nats", "The messenger backend type (nats, gochannel)")
	fs.String("nats-host", "localhost", "The NATS server host")
	fs.Int("nats-port", 4222, "The NATS server port")
	fs.String("nats-token", "", "The NATS server auth token")
	fs.String("queue-group", "planner-worker", "Queue group for worker subscriptions")
	fs.Int("subscribers-count", 1, "How many subscriber goroutines to run")
	fs.Duration("ack-wait-timeout", 30*time.Second, "Ack wait timeout for NATS subscriber")
	fs.Int64("channel-buffer", 100, "GoChannel output buffer size")
	fs.Bool("persistent", true, "Whether GoChannel should persist messages in memory")

	fs.String("llm-provider", "gemini", "LLM provider (gemini, openai, ollama)")
	fs.String("llm-key", "", "LLM API key")
	fs.String("llm-model", "", "LLM model name (e.g. gemini-2.0-flash)")
	fs.Duration("llm-timeout", 30*time.Second, "LLM request timeout")

	fs.String("prompt-path", DefaultPromptPath, "Path to the extractor prompt file")
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

	switch config.MessengerType {
	case "nats":
		var natsCfg app.NatsConfig
		if err := v.Unmarshal(&natsCfg); err != nil {
			return nil, fmt.Errorf("failed to unmarshal nats config: %w", err)
		}
		if natsCfg.SubscribersCount == 0 {
			natsCfg.SubscribersCount = 1
		}
		if natsCfg.AckWaitTimeout == 0 {
			natsCfg.AckWaitTimeout = 30 * time.Second
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
