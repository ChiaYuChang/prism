package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/ChiaYuChang/prism/internal/infra"
	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/internal/repo/pg"
	"github.com/google/uuid"

	lg "github.com/ChiaYuChang/prism/pkg/logger"

	"github.com/ChiaYuChang/prism/internal/repo"
	wm "github.com/ThreeDotsLabs/watermill/message"
	"github.com/redis/go-redis/v9"
)

const (
	// LockKey identifies this specific scheduler instance in Valkey/Redis.
	LockKey = "prism:scheduler:lock"
	// LockTTL ensures the lock is released if the scheduler crashes.
	LockTTL = 30 * time.Second
)

type TaskPublisher interface {
	Publish(topic string, messages ...*wm.Message) error
}

func main() {
	// 1. Load Configuration
	config, err := LoadConfig(os.Args[1:])
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// 2. Initialize Shared Logger
	logger, logFile, err := obs.InitLogger(config.Logger.Path, config.Logger.GetLogLevel())
	if err != nil {
		slog.Error("failed to initialize logger", "error", err)
		os.Exit(1)
	}
	if logFile != nil {
		defer func() {
			if err := logFile.Close(); err != nil {
				slog.Error("failed to close log file", "error", err)
			}
		}()
	}

	// 3. Initial Setup: Health Monitoring
	monitor := obs.NewHealthMonitor()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Start Health Check Server
	obs.StartHealthServer(ctx, config.HealthPort, monitor)

	// 4. Resource Initialization (Valkey & Locker)
	vClient, err := infra.NewValkeyClient(ctx, &redis.Options{
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
	defer func() {
		if err := vClient.Close(); err != nil {
			slog.Error("failed to close Valkey client", "error", err)
		}
	}()

	// Initialize Lua-based Distributed Locker
	locker, err := infra.NewValkeyLocker(ctx, vClient)
	if err != nil {
		logger.Error("failed to initialize locker scripts", "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to load locker scripts")
		os.Exit(1)
	}

	// 5. Messenger Initialization (Polymorphic)
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

	// 6. Repository Initialization
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

	logger = lg.WithHook(logger,
		lg.SinceHook("uptime", time.Now()),
		lg.AttrHook("pid", fmt.Sprintf("%d", os.Getpid())),
		lg.ServiceHook("scheduler"),
	)
	scheduler := dbRepo.Scheduler()

	logger.Info("Scheduler starting",
		"interval", config.Interval,
		"valkey_addr", config.Valkey.Addr(),
		"messenger", config.MessengerType,
		"batch_size", config.BatchSize)

	// Once initialized, set to OK
	monitor.OK()

	// 5. Main Ticker Loop
	ticker := time.NewTicker(config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("Shutting down scheduler gracefully")
			return
		case t := <-ticker.C:
			logger.Info("Tick triggered", "time", t)

			// --- STEP 2: Atomic Distributed Lock (using Lua SHA) ---
			// Acquire the lock with a unique secret and TTL.
			secret, err := locker.TryLock(ctx, LockKey, LockTTL)
			if err != nil {
				logger.Error("failed to acquire lock via Lua", "error", err)
				continue
			}

			if secret == "" {
				logger.Warn("Failed to acquire lock: another scheduler might be running", "key", LockKey)
				continue
			}

			logger.Info("Lock acquired successfully", "key", LockKey, "secret", secret)

			// --- STEP 3: Dispatching ---
			// 1. Fetch active search tasks atomically using SKIP LOCKED
			tasks, err := scheduler.ClaimTasks(ctx, int32(config.BatchSize))
			if err != nil {
				logger.Error("failed to claim search tasks from postgres", "error", err)
				goto release
			}

			if len(tasks) == 0 {
				logger.Info("No pending tasks to dispatch")
				goto release
			}

			logger.Info("Tasks claimed", "count", len(tasks))

			if err := dispatchTasks(ctx, logger, msgr, scheduler, tasks); err != nil {
				logger.Error("dispatch loop finished with error", "error", err)
			}

		release:
			// Safely Release the lock with the unique secret
			if err := locker.Unlock(ctx, LockKey, secret); err != nil {
				logger.Error("failed to release lock safely", "error", err)
			} else {
				logger.Info("Lock released safely", "key", LockKey)
			}
		}
	}
}

func dispatchTasks(ctx context.Context, logger *slog.Logger, publisher TaskPublisher, scheduler repo.Scheduler, tasks []repo.Task) error {
	for _, task := range tasks {
		tLogger := lg.WithHook(logger,
			lg.AttrHook("task_id", task.ID.String()),
			lg.AttrHook("trace_id", task.TraceID),
			lg.AttrHook("retry_count", strconv.Itoa(task.RetryCount)),
			lg.SinceHook("dispatch_time", time.Now()),
		)

		sig := &message.TaskSignal{
			TaskID:     task.ID,
			BatchID:    task.BatchID,
			Kind:       task.Kind,
			SourceType: task.SourceType,
			SourceID:   task.SourceID,
			URL:        task.URL,
			Payload:    task.Payload,
			TraceID:    task.TraceID,
			SentAt:     time.Now(),
		}

		payload, err := sig.Marshal()
		if err != nil {
			tLogger.Error("failed to marshal search signal", "error", err)
			continue
		}

		msg := wm.NewMessage(uuid.Must(uuid.NewV7()).String(), payload)
		msg.Metadata.Set("trace_id", task.TraceID)

		if err := publisher.Publish(message.TaskTopic, msg); err != nil {
			tLogger.Error("failed to publish search signal", "error", err)
			if failErr := scheduler.FailTask(ctx, task.ID); failErr != nil {
				tLogger.Error("failed to mark task as failed", "error", failErr)
			}
			continue
		}

		tLogger.Debug("dispatched search task")
	}

	return nil
}
