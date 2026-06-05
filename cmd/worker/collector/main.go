package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ChiaYuChang/prism/internal/collector"
	"github.com/ChiaYuChang/prism/internal/collector/fetcher"
	"github.com/ChiaYuChang/prism/internal/collector/minifier"
	parserconfig "github.com/ChiaYuChang/prism/internal/collector/parser/config"
	parserllm "github.com/ChiaYuChang/prism/internal/collector/parser/llm"
	"github.com/ChiaYuChang/prism/internal/collector/transformer"
	"github.com/ChiaYuChang/prism/internal/dev"
	"github.com/ChiaYuChang/prism/internal/infra"
	llmfactory "github.com/ChiaYuChang/prism/internal/llm/factory"
	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/internal/repo/pg"
	prismlogger "github.com/ChiaYuChang/prism/pkg/logger"
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

	shutdownTracer := infra.InitAndSetTracer(TracerName)
	defer func() {
		if err := shutdownTracer(context.Background()); err != nil {
			logger.Error("failed to shutdown tracer", "error", err)
		}
	}()
	tracer := infra.Tracer()

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

	httpClient, err := dev.WrapClientReplay(
		dev.WrapClient(&http.Client{Timeout: config.HTTPTimeout}, config.CaptureDir, logger),
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

	// Wire Archiver as errorSaver when Archive URI is set.
	// When empty, raw content is not archived on Minify failures.
	var errorSaver collector.Saver
	if config.Archive != "" {
		arch, err := openArchiver(ctx, config.Archive, config.S3, logger)
		if err != nil {
			logger.Error("failed to initialize archiver", "archive", config.Archive, "error", err)
			monitor.SetStatus(obs.LevelError, "Failed to initialize archiver")
			os.Exit(1)
		}
		errorSaver = arch
		logger.Info("archive enabled", "archive", config.Archive)
	}

	pCfg, err := parserconfig.LoadConfig(config.ParsersConfigPath)
	if err != nil {
		logger.Error("failed to load parsers config", "path", config.ParsersConfigPath, "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to load parsers config")
		os.Exit(1)
	}

	var llmFactory parserconfig.LLMFactory
	if pCfg.Fallback.Enable {
		if config.Prompt != "" {
			pCfg.Fallback.PromptFile = config.Prompt
		}
		prompt, perr := parserconfig.LoadFallbackPrompt(pCfg.Fallback)
		if perr != nil {
			logger.Error("failed to load fallback prompt",
				"path", pCfg.Fallback.PromptFile, "error", perr)
			monitor.SetStatus(obs.LevelError, "Failed to load fallback prompt")
			os.Exit(1)
		}
		gen, gerr := llmfactory.NewGenerator(ctx, pCfg.Fallback.LLM, logger)
		if gerr != nil {
			logger.Error("failed to initialize fallback LLM generator",
				"provider", pCfg.Fallback.LLM.Provider, "error", gerr)
			monitor.SetStatus(obs.LevelError, "Failed to initialize fallback LLM generator")
			os.Exit(1)
		}
		model := pCfg.Fallback.LLM.Model
		llmFactory = func() (collector.Parser, error) {
			return parserllm.NewParser(gen, logger, model, prompt)
		}
		logger.Info("parser fallback enabled",
			"provider", pCfg.Fallback.LLM.Provider, "model", model,
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
		errorSaver,
		msgr, // archivePublisher wired up to send messages to the archive topic
		dbRepo.Pipeline(),
		dbRepo.Scheduler(),
	)
	if err != nil {
		logger.Error("failed to build collector handler", "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to build collector handler")
		os.Exit(1)
	}

	messages, err := msgr.Subscribe(ctx, message.TaskTopic)
	if err != nil {
		logger.Error("failed to subscribe topic", "topic", message.TaskTopic, "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to subscribe task topic")
		os.Exit(1)
	}

	logger.Info("collector worker started",
		"topic", message.TaskTopic,
		"messenger", config.MessengerType,
		"http_timeout", config.HTTPTimeout,
	)
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

			msgCtx, cancel := context.WithTimeout(ctx, config.MaxProcessingTime)
			ack, err := handler.HandleMessage(msgCtx, msg)
			cancel()
			if err != nil {
				logger.Error("failed to handle collector task", "error", err)
			}

			if ack {
				msg.Ack()
				continue
			}
			msg.Nack()
		}
	}
}
