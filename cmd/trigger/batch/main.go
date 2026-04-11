package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ChiaYuChang/prism/internal/infra"
	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/internal/repo/pg"
	triggerbatch "github.com/ChiaYuChang/prism/internal/trigger/batch"
	"github.com/redis/go-redis/v9"
)

const (
	TracerName      = "prism.trigger.batch"
	CompletedKeyFmt = "prism:trigger:batch:completed:%s"
)

func main() {
	config, err := LoadConfig(os.Args[1:])
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	logger, logFile, err := obs.InitLogger(config.Logger.Path, config.Logger.GetLogLevel())
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
	defer func() { _ = msgr.Close() }()

	repository, repositoryCloser, err := pg.NewFactory(config.Postgres).NewRepository(ctx)
	if err != nil {
		logger.Error("failed to initialize repository", "backend", "postgres", "host", config.Postgres.Host, "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to connect to Postgres")
		os.Exit(1)
	}
	defer func() { _ = repositoryCloser.Close() }()

	valkey, err := infra.NewValkeyClient(ctx, &redis.Options{
		Addr:     config.Valkey.Addr(),
		Username: config.Valkey.Username,
		Password: config.Valkey.Password,
		DB:       config.Valkey.DB,
	})
	if err != nil {
		logger.Error("failed to connect to Valkey", "addr", config.Valkey.Addr(), "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to connect to Valkey")
		os.Exit(1)
	}
	defer func() { _ = valkey.Close() }()

	publisher, err := message.NewWatermillBatchCompletedPublisher(msgr)
	if err != nil {
		logger.Error("failed to build batch completed publisher", "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to build batch completed publisher")
		os.Exit(1)
	}

	trigger, err := triggerbatch.New(logger, tracer, repository.BatchTrigger())
	if err != nil {
		logger.Error("failed to build batch trigger", "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to build batch trigger")
		os.Exit(1)
	}

	ticker := time.NewTicker(config.Interval)
	defer ticker.Stop()
	monitor.OK()
	logger.Info("batch trigger started", "interval", config.Interval, "recent_limit", config.RecentLimit)

	for {
		select {
		case <-ctx.Done():
			logger.Info("shutting down batch trigger")
			return
		case <-ticker.C:
			if err := runOnce(ctx, valkey, publisher, trigger, config.RecentLimit); err != nil {
				logger.Error("batch trigger tick failed", "error", err)
			}
		}
	}
}

func runOnce(
	ctx context.Context,
	valkey *redis.Client,
	publisher message.BatchCompletedPublisher,
	trigger *triggerbatch.Trigger,
	recentLimit int32,
) error {
	completed, err := trigger.ScanCompletedBatches(ctx, recentLimit)
	if err != nil {
		return err
	}

	for _, batch := range completed {
		key := fmt.Sprintf(CompletedKeyFmt, batch.BatchID.String())
		err := valkey.SetArgs(ctx, key, batch.TraceID, redis.SetArgs{Mode: "NX"}).Err()
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil {
			return fmt.Errorf("mark batch %s in valkey: %w", batch.BatchID, err)
		}
		if err := publisher.PublishBatchCompleted(ctx, &message.BatchCompletedSignal{
			BatchID:    batch.BatchID,
			SourceType: batch.SourceType,
			TraceID:    batch.TraceID,
			SentAt:     time.Now(),
		}); err != nil {
			return fmt.Errorf("publish batch %s completed: %w", batch.BatchID, err)
		}
	}

	return nil
}
