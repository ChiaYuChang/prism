package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"time"

	"github.com/ChiaYuChang/prism/internal/infra"
	prismmessage "github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/ChiaYuChang/prism/pkg/logger"
	wm "github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
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

// Config contains the behavior of one scheduler process.
type Config struct {
	Name       string
	BatchSize  int
	RetryMax   int
	Kinds      []string
	MediaQuota int
	Buffer     int
}

// Dependencies contains the runtime dependencies for one scheduler process.
type Dependencies struct {
	Logger    *slog.Logger
	Tracer    trace.Tracer
	Metrics   *Metrics
	RateLimit infra.RateLimiter
	Tasks     repo.Scheduler
	Publisher TaskPublisher
}

// Service claims, filters, and publishes tasks for one scheduler process.
type Service struct {
	logger    *slog.Logger
	tracer    trace.Tracer
	metrics   *Metrics
	rl        infra.RateLimiter
	tasks     repo.Scheduler
	publisher TaskPublisher
	config    Config
	retryMax  int
}

// Metrics contains scheduler task, tick, and dispatch instruments.
type Metrics struct {
	tasks            metric.Int64Counter
	tickDuration     metric.Float64Histogram
	dispatchDuration metric.Float64Histogram
}

// NewMetrics creates scheduler instruments from the supplied meter.
func NewMetrics(meter metric.Meter) (*Metrics, error) {
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

	return &Metrics{
		tasks:            tasks,
		tickDuration:     tickDuration,
		dispatchDuration: dispatchDuration,
	}, nil
}

// New creates a scheduler service for one process.
func New(cfg Config, deps Dependencies) (*Service, error) {
	if deps.Logger == nil {
		return nil, fmt.Errorf("param missing: logger")
	}
	if deps.Tracer == nil {
		return nil, fmt.Errorf("param missing: tracer")
	}
	if deps.RateLimit == nil {
		return nil, fmt.Errorf("param missing: rate limiter")
	}
	if deps.Tasks == nil {
		return nil, fmt.Errorf("param missing: task repository")
	}
	if deps.Publisher == nil {
		return nil, fmt.Errorf("param missing: publisher")
	}
	if cfg.BatchSize < 1 {
		return nil, fmt.Errorf("param missing: batch size")
	}

	retryMax := cfg.RetryMax
	if retryMax == 0 {
		retryMax = repo.DefaultTaskRetryMax
	}
	if retryMax < 1 {
		return nil, fmt.Errorf("param missing: retry max")
	}

	return &Service{
		logger:    deps.Logger,
		tracer:    deps.Tracer,
		metrics:   deps.Metrics,
		rl:        deps.RateLimit,
		tasks:     deps.Tasks,
		publisher: deps.Publisher,
		config:    cfg,
		retryMax:  retryMax,
	}, nil
}

func (m *Metrics) recordTask(ctx context.Context, task repo.Task, result string) {
	if m == nil {
		return
	}
	m.tasks.Add(ctx, 1, metric.WithAttributes(
		attribute.String("task.kind", task.Kind),
		attribute.String("source.type", task.SourceType),
		attribute.String("result", result),
	))
}

func (m *Metrics) recordTickDuration(ctx context.Context, started time.Time, result string) {
	if m == nil {
		return
	}
	m.tickDuration.Record(ctx, time.Since(started).Seconds(), metric.WithAttributes(
		attribute.String("result", result),
	))
}

func (m *Metrics) recordDispatchDuration(ctx context.Context, started time.Time, result string) {
	if m == nil {
		return
	}
	m.dispatchDuration.Record(ctx, time.Since(started).Seconds(), metric.WithAttributes(
		attribute.String("result", result),
	))
}

// RunTick claims tasks, applies rate limiting, releases excess tasks, and
// returns the approved dispatch list.
func (s *Service) RunTick(ctx context.Context) []repo.Task {
	started := time.Now()
	result := "ok"
	ctx, span := s.tracer.Start(ctx, "scheduler.tick", trace.WithAttributes(
		attribute.String("scheduler.name", s.config.Name),
		attribute.StringSlice("scheduler.kinds", s.config.Kinds),
		attribute.Int("scheduler.batch_size", s.config.BatchSize),
	))
	defer func() {
		s.metrics.recordTickDuration(ctx, started, result)
		span.End()
	}()

	var tasks []repo.Task
	if s.config.MediaQuota > 0 && slices.Contains(s.config.Kinds, repo.TaskKindPageFetch) {
		tasks = s.runPriorityTick(ctx)
	} else {
		tasks = s.runSimpleTick(ctx)
	}
	span.SetAttributes(attribute.Int("task.count.dispatching", len(tasks)))
	return tasks
}

func (s *Service) runSimpleTick(ctx context.Context) []repo.Task {
	ctx, span := s.tracer.Start(ctx, "scheduler.claim.simple")
	defer span.End()

	claimed, err := s.tasks.ClaimTasks(ctx, int32(s.config.BatchSize+s.config.Buffer), s.config.Kinds, nil)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "claim tasks")
		s.logger.Error("failed to claim tasks", "error", err)
		return nil
	}
	pass, toRelease := applyRateLimit(claimed, s.rl, s.config.BatchSize)
	s.ReleaseAll(ctx, toRelease)
	span.SetAttributes(
		attribute.Int("task.count.claimed", len(claimed)),
		attribute.Int("task.count.dispatching", len(pass)),
		attribute.Int("task.count.released", len(toRelease)),
	)
	s.logger.Info("tick complete", "scheduler", s.config.Name,
		"claimed", len(claimed), "dispatching", len(pass), "released", len(toRelease))
	return pass
}

func (s *Service) runPriorityTick(ctx context.Context) []repo.Task {
	ctx, span := s.tracer.Start(ctx, "scheduler.claim.priority")
	defer span.End()

	mediaClaimed, err := s.tasks.ClaimTasks(
		ctx,
		int32(s.config.MediaQuota+s.config.Buffer),
		[]string{repo.TaskKindPageFetch},
		[]string{repo.SourceTypeMedia},
	)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "claim media page fetch tasks")
		s.logger.Error("failed to claim MEDIA PAGE_FETCH tasks", "error", err)
		return nil
	}
	mediaPass, mediaRelease := applyRateLimit(mediaClaimed, s.rl, s.config.MediaQuota)
	s.ReleaseAll(ctx, mediaRelease)

	remaining := s.config.BatchSize - len(mediaPass)
	backgroundClaimed, err := s.tasks.ClaimTasks(
		ctx,
		int32(remaining+s.config.Buffer),
		s.config.Kinds,
		[]string{repo.SourceTypeParty},
	)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "claim background tasks")
		s.logger.Error("failed to claim background tasks", "error", err)
		return mediaPass
	}
	backgroundPass, backgroundRelease := applyRateLimit(backgroundClaimed, s.rl, remaining)
	s.ReleaseAll(ctx, backgroundRelease)

	all := append(mediaPass, backgroundPass...)
	span.SetAttributes(
		attribute.Int("task.count.media_claimed", len(mediaClaimed)),
		attribute.Int("task.count.media_dispatching", len(mediaPass)),
		attribute.Int("task.count.background_claimed", len(backgroundClaimed)),
		attribute.Int("task.count.background_dispatching", len(backgroundPass)),
		attribute.Int("task.count.dispatching", len(all)),
	)
	s.logger.Info("priority tick complete", "scheduler", s.config.Name,
		"media_claimed", len(mediaClaimed), "media_pass", len(mediaPass),
		"background_claimed", len(backgroundClaimed), "background_pass", len(backgroundPass),
		"total_dispatching", len(all))
	return all
}

// ReleaseAll resets task IDs back to PENDING, logging any error.
func (s *Service) ReleaseAll(ctx context.Context, ids []uuid.UUID) {
	if len(ids) == 0 {
		return
	}
	if err := s.tasks.ReleaseTasks(ctx, ids); err != nil {
		s.logger.Error("failed to release rate-limited tasks", "count", len(ids), "error", err)
	}
}

// DispatchTasks publishes a TaskSignal for each task. Publish failures are
// marked failed so the task can be retried on a later tick.
func (s *Service) DispatchTasks(ctx context.Context, tasks []repo.Task) error {
	started := time.Now()
	result := "ok"
	ctx, span := s.tracer.Start(ctx, "scheduler.dispatch_tasks", trace.WithAttributes(
		attribute.String("scheduler.name", s.config.Name),
		attribute.Int("task.count.dispatching", len(tasks)),
	))
	defer func() {
		s.metrics.recordDispatchDuration(ctx, started, result)
		span.End()
	}()

	for _, task := range tasks {
		tLogger := logger.WithHook(
			s.logger,
			logger.AttrHook("scheduler", s.config.Name),
			logger.AttrHook("task_id", task.ID.String()),
			logger.AttrHook("trace_id", task.TraceID),
			logger.AttrHook("retry_count", strconv.Itoa(task.RetryCount)),
			logger.SinceHook("dispatch_time", time.Now()),
		)

		sig := &prismmessage.TaskSignal{
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
		if err := prismmessage.InjectTraceContext(ctx, msg); err != nil {
			span.RecordError(err)
			result = "error"
			s.metrics.recordTask(ctx, task, "trace_context_failed")
			tLogger.Error("failed to inject trace context", "error", err)
			continue
		}

		if err := s.publisher.Publish(prismmessage.TaskTopic, msg); err != nil {
			span.RecordError(err)
			result = "error"
			tLogger.Error("failed to publish task signal", "error", err)
			if failErr := s.tasks.FailTask(ctx, task.ID, s.retryMax, err.Error()); failErr != nil {
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

func applyRateLimit(tasks []repo.Task, rl infra.RateLimiter, limit int) (pass []repo.Task, release []uuid.UUID) {
	for _, task := range tasks {
		if len(pass) < limit && rl.Allow(task.SourceAbbr) {
			pass = append(pass, task)
		} else {
			release = append(release, task.ID)
		}
	}
	return pass, release
}
