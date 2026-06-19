package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ChiaYuChang/prism/internal/appconfig"
	"github.com/ChiaYuChang/prism/internal/infra"
	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/ChiaYuChang/prism/internal/repo/pg"

	"github.com/google/uuid"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	lg "github.com/ChiaYuChang/prism/pkg/logger"

	wm "github.com/ThreeDotsLabs/watermill/message"
)

const (
	TracerName = "prism.scheduler"

	// LockTTL ensures the lock is released if the scheduler crashes.
	LockTTL = 30 * time.Second
)

// TaskPublisher is satisfied by any Watermill publisher.
type TaskPublisher interface {
	Publish(topic string, messages ...*wm.Message) error
}

// Scheduler coordinates task claiming, rate limiting, and dispatch for one
// scheduler instance (e.g. scheduler-fast or scheduler-slow).
type Scheduler struct {
	logger    *slog.Logger
	tracer    trace.Tracer
	metrics   *schedulerMetrics
	rl        infra.RateLimiter
	scheduler repo.Scheduler
	publisher TaskPublisher
}

type schedulerMetrics struct {
	tasks            metric.Int64Counter
	tickDuration     metric.Float64Histogram
	dispatchDuration metric.Float64Histogram
}

func newSchedulerMetrics(meter metric.Meter) (*schedulerMetrics, error) {
	tasks, err := meter.Int64Counter(
		"prism.scheduler.tasks",
		metric.WithDescription("Count of scheduler task dispatch outcomes."),
		metric.WithUnit("{task}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create scheduler task counter: %w", err)
	}

	tickDuration, err := meter.Float64Histogram(
		"prism.scheduler.tick.duration",
		metric.WithDescription("Scheduler tick duration."),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("create scheduler tick duration histogram: %w", err)
	}

	dispatchDuration, err := meter.Float64Histogram(
		"prism.scheduler.dispatch.duration",
		metric.WithDescription("Scheduler dispatch loop duration."),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("create scheduler dispatch duration histogram: %w", err)
	}

	return &schedulerMetrics{tasks: tasks, tickDuration: tickDuration, dispatchDuration: dispatchDuration}, nil
}

func (m *schedulerMetrics) recordTask(ctx context.Context, task repo.Task, result string) {
	if m == nil {
		return
	}
	m.tasks.Add(ctx, 1, metric.WithAttributes(
		attribute.String("task.kind", task.Kind),
		attribute.String("source.type", task.SourceType),
		attribute.String("result", result),
	))
}

func (m *schedulerMetrics) recordTickDuration(ctx context.Context, started time.Time, result string) {
	if m == nil {
		return
	}
	m.tickDuration.Record(
		ctx,
		time.Since(started).Seconds(),
		metric.WithAttributes(attribute.String("result", result)),
	)
}

func (m *schedulerMetrics) recordDispatchDuration(ctx context.Context, started time.Time, result string) {
	if m == nil {
		return
	}
	m.dispatchDuration.Record(
		ctx,
		time.Since(started).Seconds(),
		metric.WithAttributes(attribute.String("result", result)),
	)
}

func newScheduler(logger *slog.Logger, tracer trace.Tracer, metrics *schedulerMetrics, rl infra.RateLimiter, scheduler repo.Scheduler, publisher TaskPublisher) *Scheduler {
	return &Scheduler{
		logger:    logger,
		tracer:    tracer,
		metrics:   metrics,
		rl:        rl,
		scheduler: scheduler,
		publisher: publisher,
	}
}

// RunTick executes one scheduler tick: claim tasks, apply rate limiting,
// release excess, and return the approved dispatch list.
//
// When MediaQuota > 0 and PAGE_FETCH is in Kinds, two sequential ClaimTasks
// calls are made: MEDIA first (user-waiting), PARTY second (fills remainder).
// Otherwise a single call claims all kinds without source_type filtering.
func (s *Scheduler) RunTick(ctx context.Context, cfg *Config) []repo.Task {
	started := time.Now()
	result := "ok"
	ctx, span := s.tracer.Start(ctx, "scheduler.tick",
		trace.WithAttributes(
			attribute.String("scheduler.lock_key", cfg.LockKey),
			attribute.StringSlice("scheduler.kinds", cfg.Kinds),
			attribute.Int("scheduler.batch_size", cfg.BatchSize),
		),
	)
	defer func() {
		s.metrics.recordTickDuration(ctx, started, result)
		span.End()
	}()

	n := cfg.BatchSize
	buf := cfg.Buffer

	var tasks []repo.Task
	if cfg.MediaQuota > 0 && slices.Contains(cfg.Kinds, repo.TaskKindPageFetch) {
		tasks = s.runPriorityTick(ctx, n, cfg.MediaQuota, buf, cfg.Kinds)
	} else {
		tasks = s.runSimpleTick(ctx, n, buf, cfg.Kinds)
	}
	span.SetAttributes(attribute.Int("task.count.dispatching", len(tasks)))
	return tasks
}

// runSimpleTick claims n+buffer tasks for all kinds and source types,
// applies rate limiting, releases excess, and returns the approved list.
func (s *Scheduler) runSimpleTick(ctx context.Context, n, buf int, kinds []string) []repo.Task {
	ctx, span := s.tracer.Start(ctx, "scheduler.claim.simple")
	defer span.End()

	claimed, err := s.scheduler.ClaimTasks(ctx, int32(n+buf), kinds, nil)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "claim tasks")
		s.logger.Error("failed to claim tasks", "error", err)
		return nil
	}
	pass, toRelease := applyRateLimit(claimed, s.rl, n)
	s.ReleaseAll(ctx, toRelease)
	span.SetAttributes(
		attribute.Int("task.count.claimed", len(claimed)),
		attribute.Int("task.count.dispatching", len(pass)),
		attribute.Int("task.count.released", len(toRelease)),
	)
	s.logger.Info("tick complete",
		slog.Int("claimed", len(claimed)),
		slog.Int("dispatching", len(pass)),
		slog.Int("released", len(toRelease)),
	)
	return pass
}

// runPriorityTick implements the two-step claim:
//  1. Claim up to (mdQuota + buf) PAGE_FETCH+MEDIA tasks.
//  2. Claim up to (n - mediaActual + buf) tasks for all kinds + PARTY source.
//
// Rate limiting is applied to both groups; excess tasks are released.
func (s *Scheduler) runPriorityTick(ctx context.Context, n, mdQuota, buf int, kinds []string) []repo.Task {
	ctx, span := s.tracer.Start(ctx, "scheduler.claim.priority")
	defer span.End()

	// Step 1: MEDIA PAGE_FETCH — user-triggered, highest priority.
	mdClaimed, err := s.scheduler.ClaimTasks(ctx, int32(mdQuota+buf),
		[]string{repo.TaskKindPageFetch},
		[]string{repo.SourceTypeMedia},
	)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "claim media page fetch tasks")
		s.logger.Error("failed to claim MEDIA PAGE_FETCH tasks", "error", err)
		return nil
	}
	mdPass, mdRelease := applyRateLimit(mdClaimed, s.rl, mdQuota)
	s.ReleaseAll(ctx, mdRelease)

	// Step 2: remaining capacity filled by PARTY + background kinds.
	remaining := n - len(mdPass)
	bgClaimed, err := s.scheduler.ClaimTasks(ctx, int32(remaining+buf), kinds, []string{repo.SourceTypeParty})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "claim background tasks")
		s.logger.Error("failed to claim background tasks", "error", err)
		return mdPass // still dispatch approved MEDIA tasks
	}
	bgPass, bgRelease := applyRateLimit(bgClaimed, s.rl, remaining)
	s.ReleaseAll(ctx, bgRelease)

	all := append(mdPass, bgPass...)
	span.SetAttributes(
		attribute.Int("task.count.media_claimed", len(mdClaimed)),
		attribute.Int("task.count.media_dispatching", len(mdPass)),
		attribute.Int("task.count.background_claimed", len(bgClaimed)),
		attribute.Int("task.count.background_dispatching", len(bgPass)),
		attribute.Int("task.count.dispatching", len(all)),
	)
	s.logger.Info("priority tick complete",
		slog.Int("media_claimed", len(mdClaimed)),
		slog.Int("media_pass", len(mdPass)),
		slog.Int("background_claimed", len(bgClaimed)),
		slog.Int("background_pass", len(bgPass)),
		slog.Int("total_dispatching", len(all)),
	)
	return all
}

// ReleaseAll resets a batch of task IDs back to PENDING, logging any error.
func (s *Scheduler) ReleaseAll(ctx context.Context, ids []uuid.UUID) {
	if len(ids) == 0 {
		return
	}
	if err := s.scheduler.ReleaseTasks(ctx, ids); err != nil {
		s.logger.Error("failed to release rate-limited tasks", "count", len(ids), "error", err)
	}
}

// DispatchTasks publishes a TaskSignal for each task. If publishing fails the
// task is marked failed so it can be retried on the next tick.
func (s *Scheduler) DispatchTasks(ctx context.Context, tasks []repo.Task) error {
	started := time.Now()
	result := "ok"
	ctx, span := s.tracer.Start(ctx, "scheduler.dispatch_tasks", trace.WithAttributes(attribute.Int("task.count.dispatching", len(tasks))))
	defer func() {
		s.metrics.recordDispatchDuration(ctx, started, result)
		span.End()
	}()

	for _, task := range tasks {
		tLogger := lg.WithHook(s.logger,
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
			SourceAbbr: task.SourceAbbr,
			URL:        task.URL,
			Payload:    task.Payload,
			Meta:       task.Meta,
			TraceID:    task.TraceID,
			SentAt:     time.Now(),
		}

		payload, err := sig.Marshal()
		if err != nil {
			span.RecordError(err)
			result = "error"
			s.metrics.recordTask(ctx, task, "marshal_failed")
			tLogger.Error("failed to marshal task signal", "error", err)
			continue
		}

		msg := wm.NewMessage(uuid.Must(uuid.NewV7()).String(), payload)
		msg.Metadata.Set("trace_id", task.TraceID)
		if err := message.InjectTraceContext(ctx, msg); err != nil {
			span.RecordError(err)
			result = "error"
			s.metrics.recordTask(ctx, task, "trace_context_failed")
			tLogger.Error("failed to inject trace context", "error", err)
			continue
		}

		if err := s.publisher.Publish(message.TaskTopic, msg); err != nil {
			span.RecordError(err)
			result = "error"
			tLogger.Error("failed to publish task signal", "error", err)
			if failErr := s.scheduler.FailTask(ctx, task.ID); failErr != nil {
				span.RecordError(failErr)
				s.metrics.recordTask(ctx, task, "mark_failed_error")
				tLogger.Error("failed to mark task as failed", "error", failErr)
			} else {
				s.metrics.recordTask(ctx, task, "marked_failed")
			}
			continue
		}

		s.metrics.recordTask(ctx, task, "published")
		tLogger.Debug("dispatched task")
	}

	return nil
}

// applyRateLimit filters tasks through the rate limiter, returning the approved
// list (capped at limit) and the IDs of tasks to release.
// Pure function — no struct state required.
func applyRateLimit(tasks []repo.Task, rl infra.RateLimiter, limit int) (pass []repo.Task, release []uuid.UUID) {
	for _, t := range tasks {
		if len(pass) < limit && rl.Allow(t.SourceAbbr) {
			pass = append(pass, t)
		} else {
			release = append(release, t.ID)
		}
	}
	return pass, release
}

// deriveLockKey builds a deterministic Valkey lock key from the sorted kinds
// list so that different scheduler instances never share the same lock.
func deriveLockKey(kinds []string) string {
	sorted := make([]string, len(kinds))
	copy(sorted, kinds)
	sort.Strings(sorted)
	return "prism:scheduler:" + strings.Join(sorted, "+") + ":lock"
}

func main() {
	// 1. Load Configuration
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

	// 2. Initialize Logger
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
				slog.Error("failed to close log file", "error", err)
			}
		}()
	}
	logger.Info("telemetry configured", slog.Any("telemetry", config.Telemetry))

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
	metrics, err := newSchedulerMetrics(telemetry.Meter(TracerName))
	if err != nil {
		logger.Error("failed to initialize scheduler metrics", "error", err)
		os.Exit(1)
	}
	infra.SetTracer(tracer)

	// 3. Health Monitor
	monitor := obs.NewHealthMonitor()
	obs.StartHealthServer(ctx, config.HealthPort, monitor)

	// 4. Rate Limiter
	var rl infra.RateLimiter
	if config.RateLimitConfigPath != "" {
		rlCfg, err := infra.ReadRateLimitConfig(config.RateLimitConfigPath)
		if err != nil {
			slog.Error("failed to load rate limit config", "path", config.RateLimitConfigPath, "error", err)
			os.Exit(1)
		}
		rl = infra.NewInMemoryRateLimiter(rlCfg)
	} else {
		rl = infra.NewInMemoryRateLimiter(infra.DefaultRateLimitConfig())
	}

	// 5. Valkey + Distributed Locker
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
		slog.Error("failed to connect to Valkey", "addr", config.Valkey.Addr(), "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to connect to Valkey")
		os.Exit(1)
	}
	defer func() { _ = vClient.Close() }()

	locker, err := infra.NewValkeyLocker(ctx, vClient)
	if err != nil {
		slog.Error("failed to initialize locker scripts", "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to load locker scripts")
		os.Exit(1)
	}

	// 6. Messenger
	msgr, err := config.Messenger.NewMessenger(logger)
	if err != nil {
		slog.Error("failed to initialize messenger", "type", config.MessengerType, "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to initialize messenger")
		os.Exit(1)
	}
	defer func() {
		if err := msgr.Close(); err != nil {
			slog.Error("failed to close messenger", "error", err)
		}
	}()

	// 7. Repository
	dbRepo, dbRepoCloser, err := pg.NewRepositoryBuilder(config.Postgres).NewRepository(ctx)
	if err != nil {
		slog.Error("failed to initialize repository", "host", config.Postgres.Host, "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to connect to Postgres")
		os.Exit(1)
	}
	defer func() {
		if err := dbRepoCloser.Close(); err != nil {
			slog.Error("failed to close repository resources", "error", err)
		}
	}()

	logger = lg.WithHook(logger,
		lg.SinceHook("uptime", time.Now()),
		lg.AttrHook("pid", fmt.Sprintf("%d", os.Getpid())),
		lg.ServiceHook("scheduler"),
	)

	svc := Scheduler{
		logger:    logger,
		tracer:    tracer,
		metrics:   metrics,
		rl:        rl,
		scheduler: dbRepo.Scheduler(),
		publisher: msgr,
	}

	logger.Info("Scheduler starting",
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
			logger.Info("Shutting down scheduler gracefully")
			return
		case t := <-ticker.C:
			logger.Info("Tick triggered", "time", t)

			lockCtx, lockSpan := tracer.Start(ctx, "scheduler.lock.acquire", trace.WithAttributes(attribute.String("scheduler.lock_key", config.LockKey)))
			secret, err := locker.TryLock(lockCtx, config.LockKey, LockTTL)
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
			logger.Info("Lock acquired", "key", config.LockKey)

			tasks := svc.RunTick(ctx, config)
			if len(tasks) > 0 {
				if err := svc.DispatchTasks(ctx, tasks); err != nil {
					logger.Error("dispatch loop finished with error", "error", err)
				}
			} else {
				logger.Info("No tasks to dispatch this tick")
			}

			if err := locker.Unlock(ctx, config.LockKey, secret); err != nil {
				logger.Error("failed to release lock", "error", err)
			} else {
				logger.Info("Lock released", "key", config.LockKey)
			}
		}
	}
}
