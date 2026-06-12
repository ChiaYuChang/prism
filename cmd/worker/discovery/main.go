package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"github.com/ChiaYuChang/prism/internal/dev"
	"github.com/ChiaYuChang/prism/internal/discovery"
	scoutconfig "github.com/ChiaYuChang/prism/internal/discovery/scout/config"
	"github.com/ChiaYuChang/prism/internal/discovery/search/brave"
	searchconfig "github.com/ChiaYuChang/prism/internal/discovery/search/config"
	"github.com/ChiaYuChang/prism/internal/discovery/search/googlecse"
	"github.com/ChiaYuChang/prism/internal/discovery/search/serpapi"
	discoverysink "github.com/ChiaYuChang/prism/internal/discovery/sink"
	httpclient "github.com/ChiaYuChang/prism/internal/http/client"
	"github.com/ChiaYuChang/prism/internal/infra"
	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/internal/repo/pg"
	prismlogger "github.com/ChiaYuChang/prism/pkg/logger"
)

const (
	TracerName = "prism.worker.discovery"
)

func main() {
	config, err := LoadConfig(os.Args[1:])
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	handlers, logFile, shutdownLogger, err := obs.BuildLoggingHandlers(ctx, config.Logger)
	if err != nil {
		slog.Error("failed to initialize logger", "error", err)
		os.Exit(1)
	}
	logger := prismlogger.NewLoggerFromHandlers(handlers)
	slog.SetDefault(logger)
	defer func() {
		if err := shutdownLogger(context.Background()); err != nil {
			logger.Error("failed to shutdown logger", "error", err)
		}
	}()
	if logFile != nil {
		defer func() { _ = logFile.Close() }()
	}
	if err := config.Search.ResolveSecrets(logger); err != nil {
		logger.Error("failed to resolve search provider secrets", "error", err)
		os.Exit(1)
	}

	telemetry, err := obs.InitTelemetry(ctx, config.Telemetry)
	if err != nil {
		logger.Error("failed to initialize telemetry", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := telemetry.Shutdown(context.Background()); err != nil {
			logger.Error("failed to shutdown telemetry", "error", err)
		}
	}()
	tracer := telemetry.Tracer(TracerName)
	metrics, err := newMetrics(telemetry.Meter(TracerName))
	if err != nil {
		logger.Error("failed to initialize discovery metrics", "error", err)
		os.Exit(1)
	}
	infra.SetTracer(tracer)

	monitor := obs.NewHealthMonitor()
	obs.StartHealthServer(ctx, config.HealthPort, monitor)

	msgr, err := config.Messenger.NewMessenger(logger)
	if err != nil {
		logger.Error("failed to initialize messenger", "type", config.MessengerType, "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to initialize messenger")
		os.Exit(1)
	}
	defer func() {
		if err := msgr.Close(); err != nil {
			logger.Error("failed to close messenger", "error", err)
		}
	}()

	dbRepo, dbRepoCloser, err := pg.NewRepositoryBuilder(config.Postgres).NewRepository(ctx)
	if err != nil {
		logger.Error("failed to initialize repository", "backend", "postgres", "host", config.Postgres.Host, "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to connect to Postgres")
		os.Exit(1)
	}
	defer func() {
		if err := dbRepoCloser.Close(); err != nil {
			logger.Error("failed to close repository resources", "error", err)
		}
	}()

	scoutCfg, err := scoutconfig.ReadFile(config.ScoutConfigPath)
	if err != nil {
		logger.Error("failed to read scout config", "path", config.ScoutConfigPath, "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to read scout config")
		os.Exit(1)
	}
	scoutRepo, err := scoutconfig.New(scoutCfg)
	if err != nil {
		logger.Error("failed to initialize scout config repository", "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to initialize scout config repository")
		os.Exit(1)
	}

	httpClientOptions := []httpclient.Option(nil)
	if config.FixtureBase != "" {
		httpClientOptions = append(httpClientOptions, httpclient.WithPrivateNetworks())
	}
	httpClient, err := dev.WrapClientReplay(
		dev.WrapClient(httpclient.NewPublicClient(config.HTTPTimeout, httpClientOptions...), config.CaptureDir, logger),
		config.FixtureBase,
	)
	if err != nil {
		logger.Error("failed to wrap http client for replay", "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to wrap http client for replay")
		os.Exit(1)
	}

	scoutRegistry, err := scoutconfig.BuildRegistry(scoutRepo, logger, tracer, httpClient)
	if err != nil {
		logger.Error("failed to build scout registry", "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to build scout registry")
		os.Exit(1)
	}

	sink, err := discoverysink.NewPersistingCandidateSink(logger, tracer, dbRepo.Scout(), dbRepo.Tasks())
	if err != nil {
		logger.Error("failed to build candidate sink", "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to build candidate sink")
		os.Exit(1)
	}

	searchProviders, err := buildSearchProviders(config, httpClient, logger)
	if err != nil {
		logger.Error("failed to build search providers", "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to build search providers")
		os.Exit(1)
	}

	handler, err := NewHandler(
		logger,
		tracer,
		scoutRegistry,
		searchProviders,
		sink,
		dbRepo.Scout(),
		dbRepo.Scheduler(),
		metrics,
	)
	if err != nil {
		logger.Error("failed to build discovery handler", "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to build discovery handler")
		os.Exit(1)
	}

	messages, err := msgr.Subscribe(ctx, message.TaskTopic)
	if err != nil {
		logger.Error("failed to subscribe topic", "topic", message.TaskTopic, "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to subscribe task topic")
		os.Exit(1)
	}

	started := time.Now()
	logger.Info(
		"discovery worker started",
		"topic", message.TaskTopic,
		"messenger", config.MessengerType,
		"scout_config", config.ScoutConfigPath,
		"http_timeout", config.HTTPTimeout,
		"started_at", started,
	)
	defer func() {
		logger.Info("discovery worker stopped", "uptime", time.Since(started))
	}()
	monitor.OK()

	for {
		select {
		case <-ctx.Done():
			logger.Info("shutting down discovery worker")
			return
		case msg, ok := <-messages:
			if !ok {
				logger.Warn("message channel closed")
				return
			}

			ack, err := handler.HandleMessage(ctx, msg)
			if err != nil {
				logger.Error("failed to handle discovery task", "error", err)
			}

			if ack {
				msg.Ack()
				continue
			}
			msg.Nack()
		}
	}
}

func buildSearchProviders(config *Config, httpClient *http.Client, logger *slog.Logger) (map[string]discovery.SearchClient, error) {
	providers := map[string]discovery.SearchClient{}

	braveCfg := config.Search.Provider.Brave
	if braveCfg.Enable {
		if braveCfg.APIKey == "" {
			return nil, fmt.Errorf("search provider brave api_key is missing")
		}
		providers["brave"] = brave.NewClient(httpClient, braveCfg.APIKey, brave.Options{
			Count:                braveCfg.Count,
			Offset:               braveCfg.Offset,
			SearchLang:           braveCfg.SearchLang,
			UILang:               braveCfg.UILang,
			Country:              braveCfg.Country,
			Freshness:            braveCfg.Freshness,
			SafeSearch:           braveCfg.SafeSearch,
			Spellcheck:           braveCfg.Spellcheck,
			ExtraSnippets:        braveCfg.ExtraSnippets,
			Goggles:              braveCfg.Goggles,
			IncludeFetchMetadata: braveCfg.IncludeFetchMetadata,
			Operators:            braveCfg.Operators,
			APIVersion:           braveCfg.APIVersion,
			CacheControl:         braveCfg.CacheControl,
			UserAgent:            braveCfg.UserAgent,
		})
		logger.Info("search provider enabled", "provider", "brave")
	}

	googleCfg := config.Search.Provider.GoogleCSE
	if googleCfg.Enable {
		if googleCfg.APIKey == "" {
			return nil, fmt.Errorf("search provider google-cse api_key is missing")
		}
		if googleCfg.CX == "" {
			return nil, fmt.Errorf("search provider google-cse cx is missing")
		}
		providers["google-cse"] = googlecse.NewClient(httpClient, googleCfg.APIKey, googleCfg.CX, googlecse.Options{
			Count:            googleCfg.Count,
			Language:         googleCfg.Language,
			Country:          googleCfg.Country,
			GeoLocation:      googleCfg.GeoLocation,
			InterfaceLang:    googleCfg.InterfaceLang,
			DateRestrict:     googleCfg.DateRestrict,
			ExactTerms:       googleCfg.ExactTerms,
			ExcludeTerms:     googleCfg.ExcludeTerms,
			OrTerms:          googleCfg.OrTerms,
			HighQualityTerms: googleCfg.HighQualityTerms,
			Safe:             googleCfg.Safe,
			Sort:             googleCfg.Sort,
			Filter:           googleCfg.Filter,
			ChineseSearch:    googleCfg.ChineseSearch,
		})
		logger.Info("search provider enabled", "provider", "google-cse")
	}

	if err := addSerpAPIProviders(providers, config.Search.Provider.SerpAPI, httpClient, logger); err != nil {
		return nil, err
	}

	return providers, nil
}

func addSerpAPIProviders(providers map[string]discovery.SearchClient, cfg searchconfig.SerpAPIConfig, httpClient *http.Client, logger *slog.Logger) error {
	if !cfg.Enable {
		return nil
	}
	if cfg.APIKey == "" && (cfg.GoogleNews.Enable || cfg.BingNews.Enable || cfg.DuckDuckGo.Enable) {
		return fmt.Errorf("search provider serpapi api_key is missing")
	}

	if cfg.GoogleNews.Enable {
		for _, name := range sortedKeys(cfg.GoogleNews.Params) {
			params := cfg.GoogleNews.Params[name]
			if !params.Enable {
				continue
			}
			providerName := "serpapi-google-news-" + name
			providers[providerName] = serpapi.NewClient(httpClient, cfg.APIKey, serpapi.Options{
				Engine:    "google_news",
				Country:   params.Geolocation,
				Language:  params.HostLanguage,
				SortOrder: params.SortOrder,
				NoCache:   cfg.NoCache,
			})
			logger.Info("search provider enabled", "provider", providerName)
		}
	}
	if cfg.DuckDuckGo.Enable {
		for _, name := range sortedKeys(cfg.DuckDuckGo.Params) {
			params := cfg.DuckDuckGo.Params[name]
			if !params.Enable {
				continue
			}
			providerName := "serpapi-duckduckgo-news-" + name
			providers[providerName] = serpapi.NewClient(httpClient, cfg.APIKey, serpapi.Options{
				Engine:     "duckduckgo_news",
				Region:     params.RegionCode,
				Safe:       fmt.Sprintf("%d", params.SafeSearch),
				DateFilter: params.DateFilter,
				Start:      params.PaginationStart,
				MaxResults: params.PaginationCount,
				NoCache:    cfg.NoCache,
			})
			logger.Info("search provider enabled", "provider", providerName)
		}
	}
	if cfg.BingNews.Enable {
		for _, name := range sortedKeys(cfg.BingNews.Params) {
			params := cfg.BingNews.Params[name]
			if !params.Enable {
				continue
			}
			providerName := "serpapi-bing-news-" + name
			providers[providerName] = serpapi.NewClient(httpClient, cfg.APIKey, serpapi.Options{
				Engine:     "bing_news",
				Country:    params.CountryCode,
				Language:   params.MarketCode,
				Safe:       params.SafeSearch,
				Start:      params.PaginationFirst,
				MaxResults: params.PaginationCount,
				Filter:     params.QueryFilter,
				NoCache:    cfg.NoCache,
			})
			logger.Info("search provider enabled", "provider", providerName)
		}
	}
	return nil
}

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
