package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ChiaYuChang/prism/internal/batch"
	"github.com/ChiaYuChang/prism/internal/infra"
	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/internal/repo/pg"
	prismlogger "github.com/ChiaYuChang/prism/pkg/logger"
)

const (
	TracerName = "prism.batch.publisher"
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

	msgr, err := config.Messenger.NewMessenger(logger)
	if err != nil {
		logger.Error("failed to initialize messenger", "type", config.MessengerType, "error", err)
		os.Exit(1)
	}
	defer func() { _ = msgr.Close() }()

	repository, repositoryCloser, err := pg.NewRepositoryBuilder(config.Postgres).NewRepository(ctx)
	if err != nil {
		logger.Error("failed to initialize repository", "backend", "postgres", "host", config.Postgres.Host, "error", err)
		os.Exit(1)
	}
	defer func() { _ = repositoryCloser.Close() }()

	bcPublisher, err := message.NewWatermillBatchCompletedPublisher(msgr)
	if err != nil {
		logger.Error("failed to build batch completed publisher", "error", err)
		os.Exit(1)
	}

	publisher, err := batch.NewPublisher(logger, tracer, repository.BatchTrigger(), bcPublisher)
	if err != nil {
		logger.Error("failed to build batch publisher", "error", err)
		os.Exit(1)
	}

	if config.Once {
		logger.Info("running batch publisher once")
		if _, err := publisher.Publish(ctx, config.RecentLimit); err != nil {
			logger.Error("batch publisher failed", "error", err)
			os.Exit(1)
		}
		return
	}

	obs.StartHealthServer(ctx, config.HealthPort, monitor)
	ticker := time.NewTicker(config.Interval)
	defer ticker.Stop()
	monitor.OK()
	logger.Info("batch publisher started", "interval", config.Interval, "recent_limit", config.RecentLimit)

	for {
		select {
		case <-ctx.Done():
			logger.Info("shutting down batch publisher")
			return
		case <-ticker.C:
			if _, err := publisher.Publish(ctx, config.RecentLimit); err != nil {
				logger.Error("batch publisher tick failed", "error", err)
			}
		}
	}
}
