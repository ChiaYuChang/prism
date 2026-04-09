package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	scoutconfig "github.com/ChiaYuChang/prism/internal/discovery/scout/config"
	discoverysink "github.com/ChiaYuChang/prism/internal/discovery/sink"
	"github.com/ChiaYuChang/prism/internal/infra"
	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/internal/repo/pg"
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

	logger, logFile, err := obs.InitLogger(config.LogPath, config.GetLogLevel())
	if err != nil {
		slog.Error("failed to initialize logger", "error", err)
		os.Exit(1)
	}
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
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

	dbRepo, dbRepoCloser, err := pg.NewFactory(config.Postgres).NewRepository(ctx)
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

	scoutRegistry, err := scoutconfig.BuildRegistry(
		scoutRepo,
		logger,
		tracer,
		&http.Client{Timeout: config.HTTPTimeout})
	if err != nil {
		logger.Error("failed to build scout registry", "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to build scout registry")
		os.Exit(1)
	}

	pageFetchPublisher, err := message.NewWatermillPageFetchPublisher(msgr)
	if err != nil {
		logger.Error("failed to build page fetch publisher", "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to build page fetch publisher")
		os.Exit(1)
	}

	sink, err := discoverysink.NewPersistingCandidateSink(logger, tracer, dbRepo.Scout(), pageFetchPublisher)
	if err != nil {
		logger.Error("failed to build candidate sink", "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to build candidate sink")
		os.Exit(1)
	}

	handler, err := NewHandler(
		logger,
		tracer,
		scoutRegistry,
		sink,
		dbRepo.Scout(),
		dbRepo.Scheduler(),
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

	logger.Info(
		"discovery worker started",
		"topic", message.TaskTopic,
		"messenger", config.MessengerType,
		"scout_config", config.ScoutConfigPath,
		"http_timeout", config.HTTPTimeout,
	)
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
