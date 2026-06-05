package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ChiaYuChang/prism/internal/discovery/extractor"
	"github.com/ChiaYuChang/prism/internal/discovery/planner"
	"github.com/ChiaYuChang/prism/internal/infra"
	llmfactory "github.com/ChiaYuChang/prism/internal/llm/factory"
	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/internal/repo/pg"
	prismlogger "github.com/ChiaYuChang/prism/pkg/logger"
)

const (
	TracerName = "prism.worker.planner"
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
	defer func() { _ = msgr.Close() }()

	dbRepo, dbRepoCloser, err := pg.NewRepositoryBuilder(config.Postgres).NewRepository(ctx)
	if err != nil {
		logger.Error("failed to initialize repository", "backend", "postgres", "host", config.Postgres.Host, "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to connect to Postgres")
		os.Exit(1)
	}
	defer func() { _ = dbRepoCloser.Close() }()

	generator, err := llmfactory.NewGenerator(ctx, config.LLM, logger)
	if err != nil {
		logger.Error("failed to initialize LLM generator", "provider", config.LLM.Provider, "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to initialize LLM generator")
		os.Exit(1)
	}

	prompt, err := os.ReadFile(config.PromptPath)
	if err != nil {
		logger.Error("failed to read prompt file", "path", config.PromptPath, "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to read prompt file")
		os.Exit(1)
	}

	ext, err := extractor.NewExtractor(generator, logger, tracer, config.LLM.Model, string(prompt))
	if err != nil {
		logger.Error("failed to initialize extractor", "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to initialize extractor")
		os.Exit(1)
	}

	plan, err := planner.New(logger, tracer, ext, dbRepo.Tasks(), dbRepo.Pipeline())
	if err != nil {
		logger.Error("failed to initialize planner", "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to initialize planner")
		os.Exit(1)
	}

	targets := config.Search.EnabledTargets()
	handler, err := NewHandler(logger, tracer, plan, targets)
	if err != nil {
		logger.Error("failed to initialize handler", "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to initialize handler")
		os.Exit(1)
	}

	messages, err := msgr.Subscribe(ctx, message.BatchCompletedTopic)
	if err != nil {
		logger.Error("failed to subscribe topic", "topic", message.BatchCompletedTopic, "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to subscribe batch completed topic")
		os.Exit(1)
	}

	logger.Info("planner worker started",
		"topic", message.BatchCompletedTopic,
		"messenger", config.MessengerType,
		"llm_provider", config.LLM.Provider,
		"llm_model", config.LLM.Model,
		"prompt_path", config.PromptPath,
	)
	monitor.OK()

	for {
		select {
		case <-ctx.Done():
			logger.Info("shutting down planner worker")
			return
		case msg, ok := <-messages:
			if !ok {
				logger.Warn("message channel closed")
				return
			}

			ack, err := handler.HandleMessage(ctx, msg)
			if err != nil {
				logger.Error("failed to handle batch completed signal", "error", err)
			}

			if ack {
				msg.Ack()
				continue
			}
			msg.Nack()
		}
	}
}
