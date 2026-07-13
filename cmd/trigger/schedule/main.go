package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ChiaYuChang/prism/internal/appconfig"
	"github.com/ChiaYuChang/prism/internal/infra"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/ChiaYuChang/prism/internal/repo/pg"
)

const TracerName = "prism.trigger.schedule"

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

	schedules, err := LoadScheduleDefinitions(config.SchedulesFile)
	if err != nil {
		logger.Error("failed to load schedule definitions", "path", config.SchedulesFile, "error", err)
		os.Exit(1)
	}

	repository, repositoryCloser, err := pg.NewRepositoryBuilder(config.Postgres).NewRepository(ctx)
	if err != nil {
		logger.Error("failed to initialize repository", "backend", "postgres", "host", config.Postgres.Host, "error", err)
		os.Exit(1)
	}
	defer func() { _ = repositoryCloser.Close() }()

	scheduleRepo := repository.Schedules()
	synced, err := scheduleRepo.SyncSchedules(ctx, schedules)
	if err != nil {
		logger.Error("failed to sync schedules", "error", err)
		os.Exit(1)
	}
	logger.Info("schedules synced", "count", len(synced), "path", config.SchedulesFile)

	materialize := func(ctx context.Context) {
		items, err := scheduleRepo.MaterializeDueSchedules(
			ctx, repoMaterializeParams(config.BatchSize, config.TraceIDPrefix),
		)
		if err != nil {
			logger.Error("schedule materialization failed", "error", err)
			return
		}
		if len(items) > 0 {
			logger.Info("schedules materialized", "count", len(items))
		}
	}

	if config.Once {
		materialize(ctx)
		return
	}

	monitor := obs.NewHealthMonitor()
	obs.StartHealthServer(ctx, config.HealthPort, monitor)
	go func() {
		<-ctx.Done()
		monitor.SetStatus(obs.LevelWarn, "shutting down")
	}()
	monitor.OK()
	materialize(ctx)
	ticker := time.NewTicker(config.Interval)
	defer ticker.Stop()
	logger.Info("schedule trigger started", "interval", config.Interval, "batch_size", config.BatchSize)

	for {
		select {
		case <-ctx.Done():
			logger.Info("shutting down schedule trigger")
			return
		case <-ticker.C:
			if ctx.Err() != nil {
				logger.Info("shutdown requested before schedule trigger tick")
				return
			}
			tickCtx, cancelTick := infra.NewDrainContext(config.ShutdownTimeout)
			materialize(tickCtx)
			cancelTick()
		}
	}
}

func repoMaterializeParams(limit int32, tracePrefix string) repo.MaterializeDueSchedulesParams {
	return repo.MaterializeDueSchedulesParams{Limit: limit, TraceIDPrefix: tracePrefix}
}
