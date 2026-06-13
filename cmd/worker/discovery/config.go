package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/ChiaYuChang/prism/internal/appconfig"
	searchconfig "github.com/ChiaYuChang/prism/internal/discovery/search/config"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/go-playground/validator/v10"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	DefaultScoutConfigPath = "configs/worker/discovery/scouts.yaml"
)

type Config struct {
	HealthPort      int                       `mapstructure:"health-port"    validate:"required,min=1024,max=65535"`
	Logger          obs.LoggingConfig         `mapstructure:"logger"`
	Telemetry       obs.TelemetryConfig       `mapstructure:"telemetry"`
	ScoutConfigPath string                    `mapstructure:"scout-config"   validate:"required"`
	HTTPTimeout     time.Duration             `mapstructure:"http-timeout"   validate:"required,min=1s"`
	Postgres        appconfig.PostgresConfig  `mapstructure:"postgres"`
	MessengerType   string                    `mapstructure:"messenger-type" validate:"oneof=nats gochannel"`
	Messenger       appconfig.MessengerConfig `mapstructure:"-"`
	Search          searchconfig.Config       `mapstructure:"search"`

	// CaptureDir, when non-empty, tees successful HTTP response bodies into
	// <dir>/<host>/<path>. Dev-only; used to build local fixtures during
	// the integration test plan Phase 1 real-site run.
	CaptureDir string `mapstructure:"capture-dir"`

	// FixtureBase, when non-empty, rewrites outbound HTTP requests to the
	// fixture-server at this URL (e.g. http://localhost:9999) so the worker
	// runs against captured fixtures without touching real sites. Mutually
	// exclusive with CaptureDir; integration test plan Phase 2.
	FixtureBase string `mapstructure:"fixture-base"`
}

func LoadConfig(args []string) (*Config, error) {
	v := viper.New()
	v.SetEnvPrefix("PRISM_DISCOVERY_WORKER")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	v.AutomaticEnv()

	fs := pflag.NewFlagSet("worker-discovery", pflag.ContinueOnError)
	fs.StringP("config", "c", "", "Path to the configuration file (YAML or JSON)")
	fs.Int("health-port", 8092, "The port for the health check server")
	obs.RegisterLoggingFlags(fs, obs.DefaultLoggingConfig("prism.worker.discovery"))
	obs.RegisterTelemetryFlags(fs, obs.DefaultTelemetryConfig("prism.worker.discovery"))
	fs.String("messenger-type", "nats", "The messenger backend type (nats, gochannel)")
	fs.String("scout-config", DefaultScoutConfigPath, "path to scout config file")
	fs.Duration("http-timeout", 30*time.Second, "HTTP timeout for outbound discovery requests")
	fs.Bool("search-provider-brave-enable", false, "Enable Brave Search provider for KEYWORD_SEARCH")
	fs.String("search-provider-brave-api-key", "", "Brave Search API subscription token")
	fs.String("search-provider-brave-api-key-file", "", "Path to file containing the Brave Search API subscription token")
	fs.Int("search-provider-brave-count", 20, "Brave Search results per request")
	fs.Int("search-provider-brave-offset", 0, "Brave Search zero-based page offset")
	fs.String("search-provider-brave-search-lang", "zh-hant", "Brave Search language")
	fs.String("search-provider-brave-ui-lang", "", "Brave Search UI language, e.g. zh-TW")
	fs.String("search-provider-brave-country", "TW", "Brave Search country")
	fs.String("search-provider-brave-freshness", "pw", "Brave Search freshness window")
	fs.String("search-provider-brave-safesearch", "", "Brave Search safesearch: off, moderate, strict")
	fs.String("search-provider-brave-extra-snippets", "", "Brave Search extra_snippets parameter")
	fs.String("search-provider-brave-goggles", "", "Brave Search goggles parameter")
	fs.String("search-provider-brave-api-version", "", "Brave Search api-version header")
	fs.String("search-provider-brave-cache-control", "", "Brave Search cache-control header")
	fs.String("search-provider-brave-user-agent", "", "Brave Search user-agent header")
	fs.Bool("search-provider-google-cse-enable", false, "Enable Google Custom Search provider for KEYWORD_SEARCH")
	fs.String("search-provider-google-cse-api-key", "", "Google Custom Search API key")
	fs.String("search-provider-google-cse-api-key-file", "", "Path to file containing the Google Custom Search API key")
	fs.String("search-provider-google-cse-cx", "", "Google Custom Search engine ID")
	fs.Int("search-provider-google-cse-count", 10, "Google Custom Search results per request")
	fs.String("search-provider-google-cse-language", "lang_zh-TW", "Google Custom Search language restriction")
	fs.String("search-provider-google-cse-country", "countryTW", "Google Custom Search country restriction, e.g. countryTW")
	fs.String("search-provider-google-cse-geo-location", "tw", "Google Custom Search user geolocation boost, e.g. tw")
	fs.String("search-provider-google-cse-interface-lang", "zh-TW", "Google Custom Search interface language")
	fs.String("search-provider-google-cse-date-restrict", "", "Google Custom Search date restriction, e.g. d7, w1, m1")
	fs.String("search-provider-google-cse-exact-terms", "", "Google Custom Search exactTerms parameter")
	fs.String("search-provider-google-cse-exclude-terms", "", "Google Custom Search excludeTerms parameter")
	fs.String("search-provider-google-cse-or-terms", "", "Google Custom Search orTerms parameter")
	fs.String("search-provider-google-cse-high-quality-terms", "", "Google Custom Search hq parameter")
	fs.String("search-provider-google-cse-safe", "", "Google Custom Search safe parameter: active or off")
	fs.String("search-provider-google-cse-sort", "", "Google Custom Search sort expression, e.g. date")
	fs.String("search-provider-google-cse-filter", "", "Google Custom Search duplicate filter: 0 or 1")
	fs.String("search-provider-google-cse-chinese-search", "", "Google Custom Search c2coff parameter: 0 enabled, 1 disabled")
	fs.Bool("search-provider-serpapi-enable", false, "Enable SerpAPI provider for KEYWORD_SEARCH")
	fs.Bool("search-provider-serpapi-google-news-enable", false, "Enable SerpAPI Google News engine for KEYWORD_SEARCH")
	fs.Bool("search-provider-serpapi-google-news-default-enable", false, "Enable default SerpAPI Google News params for KEYWORD_SEARCH")
	fs.String("search-provider-serpapi-api-key", "", "SerpAPI private key")
	fs.String("search-provider-serpapi-api-key-file", "", "Path to file containing the SerpAPI private key")
	fs.String("search-provider-serpapi-google-news-geolocation", "tw", "SerpAPI Google News geolocation, e.g. tw")
	fs.String("search-provider-serpapi-google-news-host-language", "zh-tw", "SerpAPI Google News host language, e.g. zh-tw")
	fs.Int("search-provider-serpapi-google-news-sort-order", 0, "SerpAPI Google News sort: 0 relevance, 1 date")
	fs.String("capture-dir", "", "Dev-only: tee successful response bodies to <dir>/<host>/<path> for fixture capture")
	fs.String("fixture-base", "", "Dev-only: rewrite outbound requests to this fixture-server URL (mutually exclusive with --capture-dir)")

	fs.String("pg-host", "localhost", "Postgres host")
	fs.Int("pg-port", 5432, "Postgres port")
	fs.String("pg-username", "postgres", "Postgres username")
	fs.String("pg-password", "postgres", "Postgres password")
	fs.String("pg-password-file", "", "Path to file containing the Postgres password (overrides --pg-password and the env var)")
	fs.String("pg-db", "prism", "Postgres database name")
	fs.String("pg-sslmode", "disable", "Postgres SSL mode")

	fs.String("nats-host", "localhost", "The NATS server host")
	fs.Int("nats-port", 4222, "The NATS server port")
	fs.String("nats-token", "", "The NATS server auth token")
	fs.String("nats-token-file", "", "Path to file containing the NATS auth token (overrides --nats-token and the env var)")
	fs.String("nats-password", "", "The NATS server password")
	fs.String("nats-password-file", "", "Path to file containing the NATS password (overrides --nats-password and the env var)")
	fs.String("queue-group", "discovery-worker", "Queue group for worker subscriptions")
	fs.Int("subscribers-count", 1, "How many subscriber goroutines to run")
	fs.Duration("ack-wait-timeout", 30*time.Second, "Ack wait timeout for NATS subscriber")
	fs.Int64("channel-buffer", 100, "GoChannel output buffer size")
	fs.Bool("persistent", true, "Whether GoChannel should persist messages in memory")

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

func bindSearchFlags(v *viper.Viper, fs *pflag.FlagSet) error {
	bindings := map[string]string{
		"search.provider.brave.enable":                                     "search-provider-brave-enable",
		"search.provider.brave.api_" + "key":                               "search-provider-brave-api-key",
		"search.provider.brave.api_" + "key_file":                          "search-provider-brave-api-key-file",
		"search.provider.brave.count":                                      "search-provider-brave-count",
		"search.provider.brave.offset":                                     "search-provider-brave-offset",
		"search.provider.brave.search_lang":                                "search-provider-brave-search-lang",
		"search.provider.brave.ui_lang":                                    "search-provider-brave-ui-lang",
		"search.provider.brave.country":                                    "search-provider-brave-country",
		"search.provider.brave.freshness":                                  "search-provider-brave-freshness",
		"search.provider.brave.safesearch":                                 "search-provider-brave-safesearch",
		"search.provider.brave.extra_snippets":                             "search-provider-brave-extra-snippets",
		"search.provider.brave.goggles":                                    "search-provider-brave-goggles",
		"search.provider.brave.api_version":                                "search-provider-brave-api-version",
		"search.provider.brave.cache_control":                              "search-provider-brave-cache-control",
		"search.provider.brave.user_agent":                                 "search-provider-brave-user-agent",
		"search.provider.google-cse.enable":                                "search-provider-google-cse-enable",
		"search.provider.google-cse.api_" + "key":                          "search-provider-google-cse-api-key",
		"search.provider.google-cse.api_" + "key_file":                     "search-provider-google-cse-api-key-file",
		"search.provider.google-cse.cx":                                    "search-provider-google-cse-cx",
		"search.provider.google-cse.count":                                 "search-provider-google-cse-count",
		"search.provider.google-cse.language":                              "search-provider-google-cse-language",
		"search.provider.google-cse.country":                               "search-provider-google-cse-country",
		"search.provider.google-cse.geo_location":                          "search-provider-google-cse-geo-location",
		"search.provider.google-cse.interface_lang":                        "search-provider-google-cse-interface-lang",
		"search.provider.google-cse.date_restrict":                         "search-provider-google-cse-date-restrict",
		"search.provider.google-cse.exact_terms":                           "search-provider-google-cse-exact-terms",
		"search.provider.google-cse.exclude_terms":                         "search-provider-google-cse-exclude-terms",
		"search.provider.google-cse.or_terms":                              "search-provider-google-cse-or-terms",
		"search.provider.google-cse.high_quality_terms":                    "search-provider-google-cse-high-quality-terms",
		"search.provider.google-cse.safe":                                  "search-provider-google-cse-safe",
		"search.provider.google-cse.sort":                                  "search-provider-google-cse-sort",
		"search.provider.google-cse.filter":                                "search-provider-google-cse-filter",
		"search.provider.google-cse.chinese_search":                        "search-provider-google-cse-chinese-search",
		"search.provider.serpapi.enable":                                   "search-provider-serpapi-enable",
		"search.provider.serpapi.api_" + "key":                             "search-provider-serpapi-api-key",
		"search.provider.serpapi.api_" + "key_file":                        "search-provider-serpapi-api-key-file",
		"search.provider.serpapi.google_news.enable":                       "search-provider-serpapi-google-news-enable",
		"search.provider.serpapi.google_news.params.default.enable":        "search-provider-serpapi-google-news-default-enable",
		"search.provider.serpapi.google_news.params.default.geolocation":   "search-provider-serpapi-google-news-geolocation",
		"search.provider.serpapi.google_news.params.default.host_language": "search-provider-serpapi-google-news-host-language",
		"search.provider.serpapi.google_news.params.default.sort_order":    "search-provider-serpapi-google-news-sort-order",
	}
	for key, flag := range bindings {
		if err := v.BindPFlag(key, fs.Lookup(flag)); err != nil {
			return fmt.Errorf("failed to bind %s: %w", flag, err)
		}
	}
	return nil
}
