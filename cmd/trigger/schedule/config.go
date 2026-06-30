package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	app "github.com/ChiaYuChang/prism/internal/appconfig"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Interval        time.Duration       `mapstructure:"interval"         validate:"required,min=10s"`
	ShutdownTimeout time.Duration       `mapstructure:"shutdown-timeout" validate:"required,min=1s"`
	Once            bool                `mapstructure:"once"`
	BatchSize       int32               `mapstructure:"batch-size"       validate:"required,min=1,max=200"`
	HealthPort      int                 `mapstructure:"health-port"      validate:"required,min=1024,max=65535"`
	SchedulesFile   string              `mapstructure:"schedules-file"   validate:"required"`
	TraceIDPrefix   string              `mapstructure:"trace-id-prefix"  validate:"required"`
	Logger          obs.LoggingConfig   `mapstructure:"logger"`
	Telemetry       obs.TelemetryConfig `mapstructure:"telemetry"`
	Postgres        app.PostgresConfig  `mapstructure:"postgres"`
}

type scheduleFile struct {
	Version   int                `yaml:"version"`
	Schedules []scheduleYAMLItem `yaml:"schedules"`
}

type scheduleYAMLItem struct {
	ID          string          `yaml:"id"            json:"id"            mapstructure:"id"`
	Name        string          `yaml:"name"          json:"name"          mapstructure:"name"`
	Enabled     bool            `yaml:"enabled"       json:"enabled"       mapstructure:"enabled"`
	SourceType  string          `yaml:"source_type"   json:"source_type"   mapstructure:"source_type"`
	SourceAbbr  string          `yaml:"source_abbr"   json:"source_abbr"   mapstructure:"source_abbr"`
	Kind        string          `yaml:"kind"          json:"kind"          mapstructure:"kind"`
	URL         string          `yaml:"url"           json:"url"           mapstructure:"url"`
	Frequency   string          `yaml:"frequency"     json:"frequency"     mapstructure:"frequency"`
	RunOnInsert bool            `yaml:"run_on_insert" json:"run_on_insert" mapstructure:"run_on_insert"`
	Payload     json.RawMessage `yaml:"payload"       json:"payload"       mapstructure:"payload"`
	Meta        json.RawMessage `yaml:"meta"          json:"meta"          mapstructure:"meta"`
}

func LoadConfig(args []string) (*Config, error) {
	v := viper.New()
	v.SetEnvPrefix("PRISM_SCHEDULE_TRIGGER")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	v.AutomaticEnv()

	fs := pflag.NewFlagSet("schedule-trigger", pflag.ContinueOnError)
	fs.StringP("config", "c", "", "Path to the configuration file (YAML or JSON)")
	fs.Duration("interval", time.Minute, "Polling interval for due schedule checks")
	fs.Duration("shutdown-timeout", 30*time.Second, "Graceful shutdown drain timeout")
	fs.Bool("once", false, "Execute one materialization tick and exit")
	fs.Int32("batch-size", 20, "Maximum due schedules to materialize per tick")
	fs.Int("health-port", 8085, "The port for the health check server")
	fs.String("schedules-file", "configs/trigger/schedule/schedules.yaml", "Path to schedules YAML")
	fs.String("trace-id-prefix", "schedule", "Trace ID prefix for materialized tasks")

	obs.RegisterLoggingFlags(fs, obs.DefaultLoggingConfig("prism.trigger.schedule"))
	obs.RegisterTelemetryFlags(fs, obs.DefaultTelemetryConfig("prism.trigger.schedule"))

	fs.String("pg-host", "localhost", "Postgres host")
	fs.Int("pg-port", 5432, "Postgres port")
	fs.String("pg-username", "postgres", "Postgres username")
	fs.String("pg-password", "postgres", "Postgres password")
	fs.String("pg-password-file", "", "Path to file containing the Postgres password")
	fs.String("pg-db", "prism", "Postgres database name")
	fs.String("pg-sslmode", "disable", "Postgres SSL mode")

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
	if err := cfg.Postgres.ResolveSecrets(); err != nil {
		return nil, fmt.Errorf("postgres secrets: %w", err)
	}
	validate := validator.New()
	if err := validate.Struct(&cfg); err != nil {
		return nil, fmt.Errorf("config validation failed: %v", err)
	}
	return &cfg, nil
}

func LoadScheduleDefinitions(path string) ([]repo.UpsertScheduleParams, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read schedules file %q: %w", path, err)
	}
	var cfg scheduleFile
	if err := yaml.Unmarshal(body, &cfg); err != nil {
		return nil, fmt.Errorf("parse schedules file %q: %w", path, err)
	}
	if cfg.Version != 1 {
		return nil, fmt.Errorf("unsupported schedules version %d", cfg.Version)
	}
	out := make([]repo.UpsertScheduleParams, len(cfg.Schedules))
	seen := make(map[uuid.UUID]struct{}, len(cfg.Schedules))
	for i, item := range cfg.Schedules {
		parsedID, err := uuid.Parse(item.ID)
		if err != nil {
			return nil, fmt.Errorf("schedule %q id: %w", item.Name, err)
		}
		if parsedID.Version() != 7 {
			return nil, fmt.Errorf("schedule %q id must be UUIDv7", item.Name)
		}
		if _, ok := seen[parsedID]; ok {
			return nil, fmt.Errorf("duplicate schedule id %s", parsedID)
		}
		seen[parsedID] = struct{}{}
		frequency, err := time.ParseDuration(item.Frequency)
		if err != nil {
			return nil, fmt.Errorf("schedule %q frequency: %w", item.Name, err)
		}
		payload := item.Payload
		if len(payload) == 0 {
			payload = json.RawMessage(`{}`)
		}
		out[i] = repo.UpsertScheduleParams{
			ID:          parsedID,
			Name:        item.Name,
			Enabled:     item.Enabled,
			ConfigHash:  scheduleConfigHash(item),
			Kind:        item.Kind,
			SourceType:  item.SourceType,
			SourceAbbr:  item.SourceAbbr,
			URL:         item.URL,
			Payload:     payload,
			Meta:        item.Meta,
			Frequency:   frequency,
			RunOnInsert: item.RunOnInsert,
		}
	}
	return out, nil
}

func scheduleConfigHash(item scheduleYAMLItem) string {
	body, _ := json.Marshal(item)
	sum := sha256.Sum256(body)
	return fmt.Sprintf("%x", sum[:])
}
