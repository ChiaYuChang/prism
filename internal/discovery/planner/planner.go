package planner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ChiaYuChang/prism/internal/discovery"
	"github.com/ChiaYuChang/prism/internal/model"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
)

const (
	TaskKindDirectoryFetch = "DIRECTORY_FETCH"
	SourceTypeMedia        = "MEDIA"
)

var (
	ErrParamMissing   = errors.New("param missing")
	ErrZeroBatchID    = errors.New("batch id is zero")
	ErrMissingTraceID = errors.New("trace id is missing")
	ErrNoTargets      = errors.New("planner target is missing")
	ErrNoSeedContents = errors.New("seed contents are missing")
)

type MediaTaskPayload struct {
	Query string `json:"query"`
	Site  string `json:"site,omitempty"`
}

type Planner struct {
	logger    *slog.Logger
	tracer    trace.Tracer
	extractor discovery.Extractor
	tasks     repo.Tasks
	pipeline  repo.Pipeline
}

func New(
	logger *slog.Logger,
	tracer trace.Tracer,
	extractor discovery.Extractor,
	tasks repo.Tasks,
	pipeline repo.Pipeline,
) (*Planner, error) {
	if logger == nil {
		return nil, fmt.Errorf("%w: logger", ErrParamMissing)
	}
	if tracer == nil {
		return nil, fmt.Errorf("%w: tracer", ErrParamMissing)
	}
	if extractor == nil {
		return nil, fmt.Errorf("%w: extractor", ErrParamMissing)
	}
	if tasks == nil {
		return nil, fmt.Errorf("%w: tasks", ErrParamMissing)
	}
	if pipeline == nil {
		return nil, fmt.Errorf("%w: pipeline", ErrParamMissing)
	}

	return &Planner{
		logger:    logger,
		tracer:    tracer,
		extractor: extractor,
		tasks:     tasks,
		pipeline:  pipeline,
	}, nil
}

func (p *Planner) Plan(ctx context.Context, req discovery.PlannerRequest) (discovery.PlannerResult, error) {
	ctx, span := p.tracer.Start(ctx, "discovery.planner.plan")
	defer span.End()

	var result discovery.PlannerResult
	if req.BatchID == uuid.Nil {
		return result, ErrZeroBatchID
	}
	if strings.TrimSpace(req.TraceID) == "" {
		return result, ErrMissingTraceID
	}
	if len(req.Targets) == 0 {
		return result, ErrNoTargets
	}

	contents, err := p.pipeline.ListContentsByBatchID(ctx, req.BatchID)
	if err != nil {
		return result, fmt.Errorf("list contents by batch %s: %w", req.BatchID, err)
	}
	if len(contents) == 0 {
		return result, ErrNoSeedContents
	}
	result.SeedContents = len(contents)

	phrases := make(map[string]struct{})
	for _, content := range contents {
		out, err := p.extractor.Extract(ctx, &model.ExtractionInput{
			Title: content.Title,
			Body:  content.Content,
		})
		if err != nil {
			return result, fmt.Errorf("extract content %s: %w", content.ID, err)
		}
		result.Extractions++
		for _, phrase := range out.Phrases {
			normalized := normalizePhrase(phrase)
			if normalized == "" {
				continue
			}
			phrases[normalized] = struct{}{}
		}
	}

	result.UniquePhrases = len(phrases)
	for _, target := range req.Targets {
		if err := validateTarget(target); err != nil {
			return result, err
		}
		for phrase := range phrases {
			payload, err := json.Marshal(MediaTaskPayload{
				Query: phrase,
				Site:  strings.TrimSpace(target.Site),
			})
			if err != nil {
				return result, fmt.Errorf("marshal task payload for source %d: %w", target.SourceID, err)
			}
			if _, err := p.tasks.CreateTask(ctx, repo.CreateTaskParams{
				BatchID:    req.BatchID,
				Kind:       TaskKindDirectoryFetch,
				SourceType: SourceTypeMedia,
				SourceID:   target.SourceID,
				URL:        target.URL,
				Payload:    payload,
				TraceID:    req.TraceID,
				Frequency:  req.Frequency,
				NextRunAt:  req.NextRunAt,
				ExpiresAt:  req.ExpiresAt,
			}); err != nil {
				return result, fmt.Errorf("create task for source %d phrase %q: %w", target.SourceID, phrase, err)
			}
			result.TasksCreated++
		}
	}

	p.logger.InfoContext(ctx, "planner completed",
		slog.String("batch_id", req.BatchID.String()),
		slog.Int("seed_contents", result.SeedContents),
		slog.Int("unique_phrases", result.UniquePhrases),
		slog.Int("tasks_created", result.TasksCreated),
	)
	return result, nil
}

func normalizePhrase(in string) string {
	return strings.TrimSpace(in)
}

func validateTarget(target discovery.PlannerTarget) error {
	if target.SourceID == 0 {
		return fmt.Errorf("%w: target.source_id", ErrParamMissing)
	}
	if strings.TrimSpace(target.URL) == "" {
		return fmt.Errorf("%w: target.url", ErrParamMissing)
	}
	return nil
}
