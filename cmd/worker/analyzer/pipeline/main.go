package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	pipeline "github.com/ChiaYuChang/prism/internal/analyzer/pipeline"
	"github.com/ChiaYuChang/prism/internal/appconfig"
	"github.com/ChiaYuChang/prism/internal/infra"
	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/internal/repo/pg"
	"github.com/ChiaYuChang/prism/internal/storage"
	wm "github.com/ThreeDotsLabs/watermill/message"
)

const (
	workerTracerName    = "prism.worker.analyzer.pipeline"
	messagingTracerName = "prism.messaging"
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
	tracer := telemetry.Tracer(workerTracerName)
	infra.SetTracer(tracer)
	monitor := obs.NewHealthMonitor()
	obs.StartHealthServer(ctx, config.Health, monitor)
	msgr, err := config.Messenger.NewMessenger(logger, &infra.MessagingTelemetry{
		Tracer: telemetry.Tracer(messagingTracerName), Meter: telemetry.Meter(messagingTracerName),
	})
	if err != nil {
		logger.Error("failed to initialize messenger", "error", err)
		os.Exit(1)
	}
	defer func() { _ = msgr.Close() }()
	dbRepo, dbCloser, err := pg.NewRepositoryBuilder(config.Postgres).NewRepository(ctx)
	if err != nil {
		logger.Error("failed to initialize repository", "error", err)
		os.Exit(1)
	}
	defer func() { _ = dbCloser.Close() }()
	spec, err := pipeline.LoadFile(config.PipelineFile)
	if err != nil {
		logger.Error("failed to load pipeline definition", "error", err)
		os.Exit(1)
	}
	pipelineData, err := os.ReadFile(config.PipelineFile)
	if err != nil {
		logger.Error("failed to read pipeline definition for hashing", "error", err)
		os.Exit(1)
	}
	definitionHash := fmt.Sprintf("%x", sha256.Sum256(pipelineData))
	registry, err := pipeline.NewRegistry(pipeline.EmbedCandidateBuilder{}, pipeline.EmbedContentBuilder{})
	if err != nil {
		logger.Error("failed to create pipeline work registry", "error", err)
		os.Exit(1)
	}
	reportStore, err := appconfig.NewStorage(ctx, config.ReportStorageURI, config.S3)
	if err != nil {
		logger.Error("failed to initialize report storage", "error", err)
		os.Exit(1)
	}
	immutableStore, ok := reportStore.(storage.ImmutableStore)
	if !ok {
		logger.Error("report storage does not support immutable writes")
		os.Exit(1)
	}
	reportWriter, err := pipeline.NewMarkdownReportWriter(immutableStore, dbRepo.PipelineRuntime(), config.ReportCacheTTL)
	if err != nil {
		logger.Error("failed to create report writer", "error", err)
		os.Exit(1)
	}
	coordinator, err := pipeline.NewCoordinator(dbRepo.Tasks(), dbRepo.PipelineRuntime(), dbRepo.Scheduler(), registry)
	if err != nil {
		logger.Error("failed to create pipeline coordinator", "error", err)
		os.Exit(1)
	}
	handler, err := pipeline.NewHandler(dbRepo.Tasks(), dbRepo.Scheduler(), dbRepo.PipelineRuntime(), coordinator, config.RetryMax, definitionHash)
	if err != nil {
		logger.Error("failed to create pipeline handler", "error", err)
		os.Exit(1)
	}
	handler.SetReportWriter(reportWriter)
	taskMessages, err := msgr.Subscribe(ctx, message.TaskTopic)
	if err != nil {
		logger.Error("failed to subscribe task topic", "error", err)
		os.Exit(1)
	}
	finishedMessages, err := msgr.Subscribe(ctx, message.PipelineBatchFinishedTopic)
	if err != nil {
		logger.Error("failed to subscribe pipeline completion topic", "error", err)
		os.Exit(1)
	}
	monitor.OK()
	logger.Info("analyzer pipeline worker started", "task_topic", message.TaskTopic, "completion_topic", message.PipelineBatchFinishedTopic)
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-taskMessages:
			if !ok {
				return
			}
			ack, handleErr := handleTaskMessage(ctx, msg, handler, spec)
			if handleErr != nil {
				logger.Error("failed to handle pipeline task message", "error", handleErr)
			}
			ackMessage(msg, ack)
		case msg, ok := <-finishedMessages:
			if !ok {
				return
			}
			ack, handleErr := handleFinishedMessage(ctx, msg, handler)
			if handleErr != nil {
				logger.Error("failed to handle pipeline completion message", "error", handleErr)
			}
			ackMessage(msg, ack)
		}
	}
}

func handleTaskMessage(ctx context.Context, msg *wm.Message, handler *pipeline.Handler, spec pipeline.PipelineSpec) (bool, error) {
	var signal message.TaskSignal
	if err := json.Unmarshal(msg.Payload, &signal); err != nil {
		return true, err
	}
	return handler.HandleTaskSignal(ctx, signal, spec)
}

func handleFinishedMessage(ctx context.Context, msg *wm.Message, handler *pipeline.Handler) (bool, error) {
	var signal message.PipelineBatchFinishedSignal
	if err := signal.Unmarshal(msg.Payload); err != nil {
		return true, err
	}
	return handler.HandleBatchFinished(ctx, signal)
}

func ackMessage(msg *wm.Message, ack bool) {
	if ack {
		msg.Ack()
		return
	}
	msg.Nack()
}
