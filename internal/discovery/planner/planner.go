package planner

import (
	"context"
	"crypto/sha256"

	"encoding/hex"
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

var (
	ErrParamMissing       = errors.New("param missing")
	ErrZeroBatchID        = errors.New("batch id is zero")
	ErrMissingTraceID     = errors.New("trace id is missing")
	ErrNoTargets          = errors.New("planner target is missing")
	ErrNoSeedContents     = errors.New("seed contents are missing")
	DefaultMaxSearchTasks = 20
)

type MediaTaskPayload struct {
	Query string `json:"query"`
	Site  string `json:"site,omitempty"`
}

type Planner struct {
	logger         *slog.Logger
	tracer         trace.Tracer
	extractor      discovery.Extractor
	tasks          repo.Tasks
	pipeline       repo.Pipeline
	persist        repo.PlannerResults
	modelID        int16
	promptID       uuid.UUID
	maxSearchTasks int
}

// NewAtomic creates a planner that commits all extraction and task writes as one batch.
func NewAtomic(logger *slog.Logger, tracer trace.Tracer, extractor discovery.Extractor, pipeline repo.Pipeline, persist repo.PlannerResults, modelID int16, promptID uuid.UUID, maxSearchTasks int) (*Planner, error) {
	if persist == nil {
		return nil, fmt.Errorf("%w: persistence", ErrParamMissing)
	}
	if logger == nil {
		return nil, fmt.Errorf("%w: logger", ErrParamMissing)
	}
	if tracer == nil {
		return nil, fmt.Errorf("%w: tracer", ErrParamMissing)
	}
	if extractor == nil {
		return nil, fmt.Errorf("%w: extractor", ErrParamMissing)
	}
	if pipeline == nil {
		return nil, fmt.Errorf("%w: pipeline", ErrParamMissing)
	}
	if maxSearchTasks < 1 {
		return nil, fmt.Errorf("%w: max search tasks", ErrParamMissing)
	}
	return &Planner{logger: logger, tracer: tracer, extractor: extractor, pipeline: pipeline, persist: persist, modelID: modelID, promptID: promptID, maxSearchTasks: maxSearchTasks}, nil
}

var _ discovery.Planner = (*Planner)(nil)

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
		logger:         logger,
		tracer:         tracer,
		extractor:      extractor,
		tasks:          tasks,
		pipeline:       pipeline,
		maxSearchTasks: DefaultMaxSearchTasks,
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

	phraseSet := make(map[string]struct{})
	phrases := make([]string, 0)
	extractions := make([]repo.PlannerExtractionParams, 0, len(contents))
	for _, content := range contents {
		out, err := p.extractor.Extract(ctx, &model.ExtractionInput{
			Title: content.Title,
			Body:  content.Content,
		})
		if err != nil {
			return result, fmt.Errorf("extract content %s: %w", content.ID, err)
		}
		result.Extractions++
		extractions = append(extractions, repo.PlannerExtractionParams{
			ContentID: content.ID,
			Title:     out.Title,
			Summary:   out.Summary,
			RawResult: out.RawResult,
			Topics:    out.Topics,
			Phrases:   out.Phrases,
			Entities:  extractionEntities(out.Entities),
		})
		for _, phrase := range out.Phrases {
			normalized := normalizePhrase(phrase)
			if normalized == "" {
				continue
			}
			if _, exists := phraseSet[normalized]; exists {
				continue
			}
			phraseSet[normalized] = struct{}{}
			phrases = append(phrases, normalized)
		}
	}

	result.UniquePhrases = len(phrases)
	phraseLimit := p.maxSearchTasks / len(req.Targets)
	if phraseLimit < 1 {
		phraseLimit = 1
	}
	if len(phrases) > phraseLimit {
		p.logger.WarnContext(ctx, "planner search task budget applied",
			slog.Int("unique_phrases", len(phrases)),
			slog.Int("phrase_limit", phraseLimit),
			slog.Int("max_search_tasks", p.maxSearchTasks),
		)
		phrases = phrases[:phraseLimit]
	}
	plannerTasks := make([]repo.CreateTaskParams, 0, len(phrases)*len(req.Targets))
	for _, target := range req.Targets {
		if err := validateTarget(target); err != nil {
			return result, err
		}
		for _, phrase := range phrases {
			payload, err := json.Marshal(MediaTaskPayload{
				Query: phrase,
				Site:  strings.TrimSpace(target.Site),
			})
			if err != nil {
				return result, fmt.Errorf("marshal task payload for source %s: %w", target.SourceAbbr, err)
			}
			sum := sha256.Sum256(payload)
			hash := hex.EncodeToString(sum[:])
			taskParams := repo.CreateTaskParams{
				BatchID:     req.BatchID,
				Kind:        repo.TaskKindKeywordSearch,
				SourceType:  repo.SourceTypeMedia,
				SourceAbbr:  target.SourceAbbr,
				URL:         target.URL,
				Payload:     payload,
				PayloadHash: &hash,
				TraceID:     req.TraceID,
				Frequency:   req.Frequency,
				NextRunAt:   req.NextRunAt,
				ExpiresAt:   req.ExpiresAt,
			}
			if p.persist != nil {
				plannerTasks = append(plannerTasks, taskParams)
				continue
			}
			if _, createErr := p.tasks.CreateTask(ctx, taskParams); createErr != nil {
				if !errors.Is(createErr, repo.ErrTaskAlreadyActive) {
					return result, fmt.Errorf("create task for source %s phrase %q: %w", target.SourceAbbr, phrase, createErr)
				}
				if req.ExpiresAt != nil {
					if extErr := p.tasks.ExtendActiveTaskExpiry(ctx, repo.ExtendActiveTaskExpiryParams{
						SourceAbbr:  target.SourceAbbr,
						Kind:        repo.TaskKindKeywordSearch,
						PayloadHash: hash,
						ExpiresAt:   req.ExpiresAt,
					}); extErr != nil {
						return result, fmt.Errorf("extend task expiry for source %s phrase %q: %w", target.SourceAbbr, phrase, extErr)
					}
				}
				continue
			}
			result.TasksCreated++
		}
	}
	if p.persist != nil {
		persisted, err := p.persist.PersistPlannerResult(ctx, repo.PersistPlannerResultParams{
			BatchID: req.BatchID, TraceID: req.TraceID, ModelID: p.modelID, PromptID: p.promptID,
			SchemaName: "extraction_result", SchemaVersion: 1, Extractions: extractions, Tasks: plannerTasks,
		})
		if err != nil {
			return discovery.PlannerResult{}, fmt.Errorf("persist planner result: %w", err)
		}
		result.TasksCreated = persisted.TasksCreated
	}

	p.logger.InfoContext(ctx, "planner completed",
		slog.String("batch_id", req.BatchID.String()),
		slog.Int("seed_contents", result.SeedContents),
		slog.Int("unique_phrases", result.UniquePhrases),
		slog.Int("tasks_created", result.TasksCreated),
	)
	return result, nil
}

func extractionEntities(in []model.ExtractionEntity) []repo.PlannerEntityParams {
	out := make([]repo.PlannerEntityParams, 0, len(in))
	for i, entity := range in {
		ordinal := int16(i + 1)
		out = append(out, repo.PlannerEntityParams{Canonical: entity.Canonical, Type: entity.Type, Surface: entity.Surface, Ordinal: &ordinal})
	}
	return out
}

func normalizePhrase(in string) string {
	return strings.TrimSpace(in)
}

func validateTarget(target discovery.PlannerTarget) error {
	if strings.TrimSpace(target.SourceAbbr) == "" {
		return fmt.Errorf("%w: target.source_abbr", ErrParamMissing)
	}
	if strings.TrimSpace(target.URL) == "" {
		return fmt.Errorf("%w: target.url", ErrParamMissing)
	}
	return nil
}
