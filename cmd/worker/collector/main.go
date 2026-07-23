package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ChiaYuChang/prism/internal/appconfig"
	"github.com/ChiaYuChang/prism/internal/collector"
	"github.com/ChiaYuChang/prism/internal/collector/fetcher"
	"github.com/ChiaYuChang/prism/internal/collector/minifier"
	parserconfig "github.com/ChiaYuChang/prism/internal/collector/parser/config"
	parserllm "github.com/ChiaYuChang/prism/internal/collector/parser/llm"
	"github.com/ChiaYuChang/prism/internal/collector/transformer"
	"github.com/ChiaYuChang/prism/internal/dev"
	httpclient "github.com/ChiaYuChang/prism/internal/http/client"
	"github.com/ChiaYuChang/prism/internal/infra"
	llmfactory "github.com/ChiaYuChang/prism/internal/llm/factory"
	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/internal/prompt"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/ChiaYuChang/prism/internal/repo/pg"
)

const (
	TracerName = "prism.worker.collector"
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
	logger := obs.NewLoggerFromHandlers(handlers)
	slog.SetDefault(logger)
	appconfig.FlushPendingLogs()
	defer func() {
		if err := shutdownLogger(context.Background()); err != nil {
			logger.Error("failed to shutdown logger", "error", err)
		}
	}()
	if logFile != nil {
		defer func() { _ = logFile.Close() }()
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
		logger.Error("failed to initialize collector metrics", "error", err)
		os.Exit(1)
	}
	infra.SetTracer(tracer)

	monitor := obs.NewHealthMonitor()

	obs.StartHealthServer(ctx, config.HealthPort, monitor)
	go func() {
		<-ctx.Done()
		monitor.SetStatus(obs.LevelWarn, "shutting down")
	}()

	msgr, err := config.Messenger.NewMessenger(logger, &infra.MessagingTelemetry{
		Tracer: telemetry.Tracer("prism.messaging"),
		Meter:  telemetry.Meter("prism.messaging"),
	})
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
		logger.ErrorContext(
			ctx,
			"failed to initialize repository",
			"backend", "postgres",
			"host", config.Postgres.Host,
			"error", err,
		)
		monitor.SetStatus(obs.LevelError, "Failed to connect to Postgres")
		os.Exit(1)
	}
	defer func() {
		if err := dbRepoCloser.Close(); err != nil {
			logger.Error("failed to close repository resources", "error", err)
		}
	}()

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
	pageFetcher := fetcher.NewRetryFetcher(
		fetcher.NewHTTPFetcher(httpClient), 3, time.Second,
	)
	pageFetcher.
		Handle(http.StatusNotFound, fetcher.FailFastHandler).
		Handle(http.StatusForbidden, fetcher.FailFastHandler).
		Handle(http.StatusUnauthorized, fetcher.FailFastHandler)

	// Wire Archiver as errSaver when Archive URI is set.
	// When empty, raw content is not archived on Minify failures.
	var errSaver collector.Saver
	if config.Archive != "" {
		arch, err := openArchiver(ctx, config.Archive, config.S3, logger)
		if err != nil {
			logger.Error("failed to initialize archiver", "archive", config.Archive, "error", err)
			monitor.SetStatus(obs.LevelError, "Failed to initialize archiver")
			os.Exit(1)
		}
		errSaver = arch
		logger.Info("archive enabled", "archive", config.Archive)
	}

	pCfg, err := parserconfig.LoadConfig(config.ParsersConfigPath)
	if err != nil {
		logger.Error(
			"failed to load parsers config",
			"path", config.ParsersConfigPath,
			"error", err,
		)
		monitor.SetStatus(obs.LevelError, "Failed to load parsers config")
		os.Exit(1)
	}

	var llmFactory parserconfig.LLMFactory
	if pCfg.Fallback.Enable {
		if config.Prompt != "" {
			pCfg.Fallback.PromptFile = config.Prompt
		}
		promptText, promptAttrs, perr := loadCollectorFallbackPrompt(ctx, dbRepo.Prompts(), pCfg.Fallback, config.PromptStorage, config.S3)
		if perr != nil {
			logger.Error(
				"failed to load fallback prompt",
				"path", pCfg.Fallback.PromptFile,
				"error", perr,
			)
			monitor.SetStatus(obs.LevelError, "Failed to load fallback prompt")
			os.Exit(1)
		}
		providerName, perr := pCfg.Fallback.LLM.ProviderName()
		if perr != nil {
			logger.Error("failed to resolve fallback LLM provider", "error", perr)
			monitor.SetStatus(obs.LevelError, "Failed to resolve fallback LLM provider")
			os.Exit(1)
		}
		gen, gerr := llmfactory.NewGenerator(ctx, pCfg.Fallback.LLM, logger)
		if gerr != nil {
			logger.Error(
				"failed to initialize fallback LLM generator",
				"provider", providerName,
				"error", gerr,
			)
			monitor.SetStatus(obs.LevelError, "Failed to initialize fallback LLM generator")
			os.Exit(1)
		}
		model := pCfg.Fallback.LLM.Model
		llmFactory = func() (collector.Parser, error) {
			return parserllm.NewParser(gen, logger, model, promptText)
		}
		logger.Info("collector fallback prompt loaded", promptAttrs...)
		logger.Info("parser fallback enabled",
			"provider", providerName, "model", model,
			"prompt_file", pCfg.Fallback.PromptFile)
	}

	registry, err := parserconfig.BuildRegistry(pCfg, logger, tracer, llmFactory)
	if err != nil {
		logger.Error("failed to build parser registry", "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to build parser registry")
		os.Exit(1)
	}

	var pageMinifier collector.Transformer = minifier.New()
	if config.ForceMinifyError {
		logger.Warn("minify error injection enabled, DEV ONLY — every page will fail Minify and route to errorSaver")
		pageMinifier = dev.FailingMinifier{}
	}

	pipelineRegistry := collector.NewPipelineRegistry(collector.Pipeline{
		Fetcher:      pageFetcher,
		Minifier:     pageMinifier,
		Transformers: []collector.Transformer{transformer.NewNoOpTransformer()},
		Parser:       registry,
	})

	dispatcher, err := collector.NewDispatcher(logger, tracer, pipelineRegistry)
	if err != nil {
		logger.Error("failed to build collector dispatcher", "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to build collector dispatcher")
		os.Exit(1)
	}

	handler, err := NewHandler(
		logger,
		tracer,
		dispatcher,
		errSaver,
		msgr, // archivePublisher wired up to send messages to the archive topic
		dbRepo.Pipeline(),
		dbRepo.Scheduler(),
		metrics,
		config.RetryMax,
	)
	if err != nil {
		logger.Error("failed to build collector handler", "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to build collector handler")
		os.Exit(1)
	}
	handler.taskReader = dbRepo.Tasks()
	handler.tasks = dbRepo.Tasks()

	messages, err := msgr.Subscribe(ctx, message.TaskTopic)
	if err != nil {
		logger.Error("failed to subscribe topic", "topic", message.TaskTopic, "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to subscribe task topic")
		os.Exit(1)
	}

	started := time.Now()
	logger.Info("collector worker started",
		"topic", message.TaskTopic,
		"messenger", config.MessengerType,
		"health_port", config.HealthPort,
		"http_timeout", config.HTTPTimeout,
		"started", started,
	)
	defer func() {
		logger.Info(
			"collector worker stopped",
			"started", started,
			"uptime", time.Since(started),
		)
	}()

	monitor.OK()

	for {
		select {
		case <-ctx.Done():
			logger.Info("shutting down collector worker")
			return
		case msg, ok := <-messages:
			if !ok {
				logger.Warn("message channel closed")
				return
			}
			if ctx.Err() != nil {
				logger.Info("returning unaccepted collector task during shutdown")
				msg.Nack()
				return
			}

			msgCtx, cancel := infra.NewDrainContext(minDuration(config.MaxProcessingTime, config.ShutdownTimeout))
			ack, err := handler.HandleMessage(msgCtx, msg)
			cancel()
			if err != nil {
				logger.Error("failed to handle collector task", "error", err)
			}

			if ack {
				msg.Ack()
			} else {
				msg.Nack()
			}
		}
	}
}

func minDuration(a, b time.Duration) time.Duration {
	if a <= 0 {
		return b
	}
	if b <= 0 || a < b {
		return a
	}
	return b
}

func loadCollectorFallbackPrompt(ctx context.Context, prompts repo.Prompts, cfg parserconfig.FallbackConfig, storageURI string, s3cfg appconfig.S3Config) (string, []any, error) {
	if cfg.Prompt.Enabled() {
		store, err := appconfig.NewStorage(ctx, storageURI, s3cfg)
		if err != nil {
			return "", nil, err
		}
		body, version, err := prompt.Resolve(ctx, prompts, store, cfg.Prompt)
		if err != nil {
			return "", nil, err
		}
		return strings.TrimRight(string(body), " \t\n\r"), []any{
			"prompt_id", version.ID.String(),
			"prompt_name", version.Name,
			"prompt_version", version.Version,
			"prompt_hash", version.Hash,
			"prompt_storage", storageURI,
		}, nil
	}
	body, err := parserconfig.LoadFallbackPrompt(cfg)
	if err != nil {
		return "", nil, err
	}
	return body, []any{"prompt_file", cfg.PromptFile}, nil
}
