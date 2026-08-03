package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/ChiaYuChang/prism/internal/appconfig"
	"github.com/ChiaYuChang/prism/internal/infra"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/internal/repo/pg"
	"github.com/ChiaYuChang/prism/internal/scheduler"
	lg "github.com/ChiaYuChang/prism/pkg/logger"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// deriveLockKey builds a deterministic Valkey lock key from the sorted kinds
// list so that different scheduler instances never share the same lock.
func deriveLockKey(kinds []string) string {
	sorted := make([]string, len(kinds))
	copy(sorted, kinds)
	sort.Strings(sorted)
	return "prism:scheduler:" + strings.Join(sorted, "+") + ":lock"
}

func main() {
	config, err := LoadConfig(os.Args[1:])
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	if config.LockKey == "" {
		config.LockKey = deriveLockKey(config.Kinds)
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
		defer func() {
			if err := logFile.Close(); err != nil {
				logger.Error("failed to close log file", "error", err)
			}
		}()
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
	tracer := telemetry.Tracer(scheduler.TracerName)
	metrics, err := scheduler.NewMetrics(telemetry.Meter(scheduler.TracerName))
	if err != nil {
		logger.Error("failed to initialize scheduler metrics", "error", err)
		os.Exit(1)
	}
	infra.SetTracer(tracer)

	monitor := obs.NewHealthMonitor()
	obs.StartHealthServer(ctx, config.Health, monitor)
	go func() {
		<-ctx.Done()
		monitor.SetStatus(obs.LevelWarn, "shutting down")
	}()

	var rl infra.RateLimiter
	if config.RateLimitConfigPath != "" {
		rlCfg, err := infra.ReadRateLimitConfig(config.RateLimitConfigPath)
		if err != nil {
			logger.Error("failed to load rate limit config", "path", config.RateLimitConfigPath, "error", err)
			os.Exit(1)
		}
		rl = infra.NewInMemoryRateLimiter(rlCfg)
	} else {
		rl = infra.NewInMemoryRateLimiter(infra.DefaultRateLimitConfig())
	}

	vClient, err := infra.NewValkeyClient(ctx, infra.ValkeyClientConfig{
		Addr:           config.Valkey.Addr(),
		Username:       config.Valkey.Username,
		Password:       config.Valkey.Password,
		DB:             config.Valkey.DB,
		ClientName:     config.Valkey.ClientName,
		TracingEnabled: config.Valkey.TracingEnabled,
		MetricsEnabled: config.Valkey.MetricsEnabled,
	})
	if err != nil {
		logger.Error("failed to connect to Valkey", "addr", config.Valkey.Addr(), "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to connect to Valkey")
		os.Exit(1)
	}
	defer func() { _ = vClient.Close() }()

	locker, err := infra.NewValkeyLocker(ctx, vClient)
	if err != nil {
		logger.Error("failed to initialize locker scripts", "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to load locker scripts")
		os.Exit(1)
	}

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

	toggles, err := infra.NewSchedulerToggleStore(vClient)
	if err != nil {
		logger.Error("failed to initialize scheduler toggles", "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to initialize scheduler toggles")
		os.Exit(1)
	}
	if err := toggles.Initialize(ctx, config.SchedulerName, !config.StartPaused); err != nil {
		logger.Error("failed to initialize scheduler toggle state", "scheduler", config.SchedulerName, "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to initialize scheduler toggle state")
		os.Exit(1)
	}

	dbRepo, dbRepoCloser, err := pg.NewRepositoryBuilder(config.Postgres).NewRepository(ctx)
	if err != nil {
		logger.Error("failed to initialize repository", "host", config.Postgres.Host, "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to connect to Postgres")
		os.Exit(1)
	}
	defer func() {
		if err := dbRepoCloser.Close(); err != nil {
			logger.Error("failed to close repository resources", "error", err)
		}
	}()

	logger = lg.WithHook(
		logger,
		lg.SinceHook("uptime", time.Now()),
		lg.AttrHook("pid", fmt.Sprintf("%d", os.Getpid())),
		lg.ServiceHook("scheduler"),
	)

	svc, err := scheduler.New(scheduler.Config{
		Name:       config.SchedulerName,
		BatchSize:  config.BatchSize,
		RetryMax:   config.RetryMax,
		Kinds:      config.Kinds,
		MediaQuota: config.MediaQuota,
		Buffer:     config.Buffer,
	}, scheduler.Dependencies{
		Logger:    logger,
		Tracer:    tracer,
		Metrics:   metrics,
		RateLimit: rl,
		Tasks:     dbRepo.Scheduler(),
		Publisher: msgr,
	})
	if err != nil {
		logger.Error("failed to initialize scheduler service", "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to initialize scheduler service")
		os.Exit(1)
	}

	logger.Info(
		"scheduler starting",
		"lock_key", config.LockKey,
		"interval", config.Interval,
		"kinds", config.Kinds,
		"batch_size", config.BatchSize,
		"media_quota", config.MediaQuota,
		"buffer", config.Buffer,
		"messenger", config.MessengerType,
	)
	monitor.OK()

	ticker := time.NewTicker(config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("shutting down scheduler gracefully")
			return
		case tickTime := <-ticker.C:
			if ctx.Err() != nil {
				logger.Info("shutdown requested before scheduler tick")
				return
			}
			logger.Info("scheduler tick triggered", "time", tickTime)
			enabled, err := toggles.Enabled(ctx, config.SchedulerName)
			if err != nil {
				logger.Error("failed to read scheduler toggle", "scheduler", config.SchedulerName, "error", err)
				continue
			}
			if !enabled {
				logger.Debug("scheduler paused", "scheduler", config.SchedulerName)
				continue
			}

			lockCtx, lockSpan := tracer.Start(ctx, "scheduler.lock.acquire",
				trace.WithAttributes(attribute.String("scheduler.lock_key", config.LockKey)))
			secret, err := locker.TryLock(lockCtx, config.LockKey, scheduler.LockTTL)
			if err != nil {
				lockSpan.RecordError(err)
				lockSpan.SetStatus(codes.Error, "acquire lock")
			}
			lockSpan.End()
			if err != nil {
				logger.Error("failed to acquire lock", "error", err)
				continue
			}
			if secret == "" {
				logger.Warn("lock held by another instance, skipping tick", "key", config.LockKey)
				continue
			}
			logger.Info("lock acquired", "key", config.LockKey)

			tickCtx, cancelTick := infra.NewDrainContext(config.ShutdownTimeout)
			tasks := svc.RunTick(tickCtx)
			enabled, err = toggles.Enabled(tickCtx, config.SchedulerName)
			if err != nil || !enabled {
				if err != nil {
					logger.Error("failed to recheck scheduler toggle", "scheduler", config.SchedulerName, "error", err)
				}
				ids := make([]uuid.UUID, 0, len(tasks))
				for _, task := range tasks {
					ids = append(ids, task.ID)
				}
				svc.ReleaseAll(tickCtx, ids)
				if unlockErr := locker.Unlock(tickCtx, config.LockKey, secret); unlockErr != nil {
					logger.Error("failed to release lock", "error", unlockErr)
				}
				cancelTick()
				continue
			}
			if len(tasks) > 0 {
				if err := svc.DispatchTasks(tickCtx, tasks); err != nil {
					logger.Error("dispatch loop finished with error", "error", err)
				}
			} else {
				logger.Info("no tasks to dispatch this tick")
			}

			if err := locker.Unlock(tickCtx, config.LockKey, secret); err != nil {
				logger.Error("failed to release lock", "error", err)
			} else {
				logger.Info("lock released", "key", config.LockKey)
			}
			cancelTick()
		}
	}
}
