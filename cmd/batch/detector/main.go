package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ChiaYuChang/prism/internal/appconfig"
	"github.com/ChiaYuChang/prism/internal/batch"
	"github.com/ChiaYuChang/prism/internal/infra"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/internal/repo/pg"
)

const (
	TracerName = "prism.batch.detector"
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
	infra.SetTracer(tracer)

	monitor := obs.NewHealthMonitor()

	repository, repositoryCloser, err := pg.NewRepositoryBuilder(config.Postgres).NewRepository(ctx)
	if err != nil {
		logger.Error("failed to initialize repository", "backend", "postgres", "host", config.Postgres.Host, "error", err)
		os.Exit(1)
	}
	defer func() { _ = repositoryCloser.Close() }()

	detector, err := batch.NewDetector(logger, tracer, repository.BatchTrigger())
	if err != nil {
		logger.Error("failed to build batch detector", "error", err)
		os.Exit(1)
	}

	if config.Once {
		logger.Info("running batch detector once")
		if _, err := detector.Detect(ctx, config.RecentLimit); err != nil {
			logger.Error("batch detector failed", "error", err)
			os.Exit(1)
		}
		return
	}

	obs.StartHealthServer(ctx, config.HealthPort, monitor)
	ticker := time.NewTicker(config.Interval)
	defer ticker.Stop()
	monitor.OK()
	logger.Info("batch detector started", "interval", config.Interval, "recent_limit", config.RecentLimit)

	for {
		select {
		case <-ctx.Done():
			logger.Info("shutting down batch detector")
			return
		case <-ticker.C:
			if _, err := detector.Detect(ctx, config.RecentLimit); err != nil {
				logger.Error("batch detector tick failed", "error", err)
			}
		}
	}
}
