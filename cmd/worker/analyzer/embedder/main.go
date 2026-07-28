package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ChiaYuChang/prism/internal/appconfig"
	"github.com/ChiaYuChang/prism/internal/infra"
	llmfactory "github.com/ChiaYuChang/prism/internal/llm/factory"
	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/internal/repo/pg"
)

const (
	WorkerTracerName    = "prism.worker.embedder"
	MessagingTracerName = "prism.messaging"
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
	defer func() { _ = shutdownLogger(context.Background()) }()
	if logFile != nil {
		defer func() { _ = logFile.Close() }()
	}

	telemetry, err := obs.InitTelemetry(ctx, config.Telemetry)
	if err != nil {
		logger.Error("failed to initialize telemetry", "error", err)
		os.Exit(1)
	}
	defer func() { _ = telemetry.Shutdown(context.Background()) }()
	tracer := telemetry.Tracer(WorkerTracerName)
	infra.SetTracer(tracer)
	metrics, err := newMetrics(telemetry.Meter(WorkerTracerName))
	if err != nil {
		logger.Error("failed to initialize embedder metrics", "error", err)
		os.Exit(1)
	}

	monitor := obs.NewHealthMonitor()
	obs.StartHealthServer(ctx, config.Health, monitor)
	go func() {
		<-ctx.Done()
		monitor.SetStatus(obs.LevelWarn, "shutting down")
	}()

	msgr, err := config.Messenger.NewMessenger(logger,
		&infra.MessagingTelemetry{
			Tracer: telemetry.Tracer(MessagingTracerName),
			Meter:  telemetry.Meter(MessagingTracerName),
		})
	if err != nil {
		logger.Error("failed to initialize messenger",
			"error", err,
			"tracer", MessagingTracerName,
			"meter", MessagingTracerName,
		)
		monitor.SetStatus(obs.LevelError, "Failed to initialize messenger")
		os.Exit(1)
	}
	defer func() { _ = msgr.Close() }()

	dbRepo, dbCloser, err := pg.NewRepositoryBuilder(config.Postgres).NewRepository(ctx)
	if err != nil {
		logger.Error("failed to initialize repository", "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to connect to Postgres")
		os.Exit(1)
	}
	defer func() { _ = dbCloser.Close() }()

	model, err := dbRepo.Models().GetEmbedderByName(ctx, config.Embedder.Model)
	if err != nil {
		logger.Error("configured embedding model is not registered", "model", config.Embedder.Model, "error", err)
		monitor.SetStatus(obs.LevelError, "Configured embedding model is not registered")
		os.Exit(1)
	}
	embedder, err := llmfactory.NewEmbedder(ctx, config.Embedder.LLMConfig, logger)
	if err != nil {
		logger.Error("failed to initialize LLM embedder", "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to initialize LLM embedder")
		os.Exit(1)
	}
	handler, err := NewHandler(HandlerConfig{
		Logger: logger,
		Tracer: tracer,
		Embedder: EmbedderConfig{
			Embedder:  embedder,
			ModelID:   model.ID,
			ModelName: config.Embedder.Model,
			Dimension: config.Embedder.Dimension,
			RetryMax:  config.Embedder.RetryMax,
		},
		Store: Store{
			Scout:      dbRepo.Scout(),
			Pipeline:   dbRepo.Pipeline(),
			Embeddings: dbRepo.Embedding(),
			Reporter:   dbRepo.Scheduler(),
			Tasks:      dbRepo.Tasks(),
		},
		Metrics: metrics,
	})
	if err != nil {
		logger.Error("failed to initialize embedder handler", "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to initialize embedder handler")
		os.Exit(1)
	}

	messages, err := msgr.Subscribe(ctx, message.TaskTopic)
	if err != nil {
		logger.Error("failed to subscribe task topic", "topic", message.TaskTopic, "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to subscribe task topic")
		os.Exit(1)
	}
	monitor.OK()
	logger.Info("embedder worker started",
		"topic", message.TaskTopic,
		"messenger", config.MessengerType,
		"model", config.Embedder.Model,
		"model_id", model.ID,
		"dimension", config.Embedder.Dimension,
	)

	for {
		select {
		case <-ctx.Done():
			logger.Info("shutting down embedder worker")
			return
		case msg, ok := <-messages:
			if !ok {
				logger.Warn("message channel closed")
				return
			}
			if ctx.Err() != nil {
				msg.Nack()
				return
			}
			msgCtx, cancel := infra.NewDrainContext(config.ShutdownTimeout)
			ack, handleErr := handler.HandleMessage(msgCtx, msg)
			cancel()
			if handleErr != nil {
				logger.Error("failed to handle embedding task", "error", handleErr)
			}
			if ack {
				msg.Ack()
			} else {
				msg.Nack()
			}
		}
	}
}
