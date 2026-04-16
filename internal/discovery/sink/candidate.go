package sink

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ChiaYuChang/prism/internal/model"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/ChiaYuChang/prism/pkg/utils"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
)

var (
	ErrParamMissing    = errors.New("param missing")
	ErrMissingSourceID = errors.New("source id is missing")
	ErrMissingTraceID  = errors.New("trace id is missing")
)

// CandidateSink abstracts the persistence mechanism for discovered candidate briefs.
// Implementations of this interface ensure that candidates are stored asynchronously
// or synchronously before any promotion to full contents.
type CandidateSink interface {
	// Handle receives discovered candidates and processes them for storage.
	Handle(ctx context.Context, req CandidateSinkRequest) error
}

// CandidateSinkRequest wraps the payloads and metadata needed to store candidates.
// It carries batch_id and source_url mapping back to the executing task for auditability.
type CandidateSinkRequest struct {
	SourceURL       string             `json:"source_url,omitempty"`
	SourceAbbr      string             `json:"source_abbr,omitempty"`
	SourceType      string             `json:"source_type,omitempty"`
	BatchID         uuid.UUID          `json:"batch_id,omitempty"`
	TraceID         string             `json:"trace_id,omitempty"`
	IngestionMethod string             `json:"ingestion_method,omitempty"`
	DefaultMetadata map[string]any     `json:"default_metadata,omitempty"`
	Candidates      []model.Candidates `json:"candidates,omitempty"`
}

// PersistingCandidateSink is the concrete implementation of CandidateSink.
// In accordance with the system's normalization-first workflow, this sink ensures
// that candidate briefs fetched by Scouts are inserted into the 'candidates'
// PG repository. For PARTY sources, a PAGE_FETCH task is created in the tasks
// table so the scheduler-fast can dispatch it to the Collector Worker.
type PersistingCandidateSink struct {
	logger *slog.Logger
	tracer trace.Tracer
	scout  repo.Scout
	tasks  repo.Tasks
}

var _ CandidateSink = (*PersistingCandidateSink)(nil)

func NewPersistingCandidateSink(
	logger *slog.Logger,
	tracer trace.Tracer,
	scoutRepo repo.Scout,
	tasks repo.Tasks,
) (*PersistingCandidateSink, error) {
	if logger == nil {
		return nil, fmt.Errorf("%w: logger", ErrParamMissing)
	}
	if tracer == nil {
		return nil, fmt.Errorf("%w: tracer", ErrParamMissing)
	}
	if scoutRepo == nil {
		return nil, fmt.Errorf("%w: scout_repository", ErrParamMissing)
	}
	if tasks == nil {
		return nil, fmt.Errorf("%w: tasks_repository", ErrParamMissing)
	}

	return &PersistingCandidateSink{
		logger: logger,
		tracer: tracer,
		scout:  scoutRepo,
		tasks:  tasks,
	}, nil
}

// Handle executes the persistence of candidates into the database using UpsertCandidate.
// Parameters are merged with request-level metadata to preserve batch context and TraceIDs.
func (s *PersistingCandidateSink) Handle(ctx context.Context, req CandidateSinkRequest) error {
	ctx, span := s.tracer.Start(ctx, "discovery.sink.candidate.handle")
	defer span.End()

	for _, candidate := range req.Candidates {
		enrichedCand, err := applyRequestDefaults(candidate, req)
		if err != nil {
			return err
		}

		params, err := toUpsertCandidateParams(enrichedCand)
		if err != nil {
			return err
		}

		stored, err := s.scout.UpsertCandidate(ctx, params)
		if err != nil {
			return fmt.Errorf("upsert candidate %s: %w", params.URL, err)
		}

		if shouldCreatePageFetch(req.SourceType) {
			if err := s.createPageFetchTask(ctx, stored, req); err != nil {
				return fmt.Errorf("create page fetch task for %s: %w", stored.URL, err)
			}
		}
	}

	s.logger.DebugContext(ctx, "candidate sink persisted candidates",
		slog.String("source_url", req.SourceURL),
		slog.Int("count", len(req.Candidates)),
	)

	return nil
}

// applyRequestDefaults applies default values from the request to the candidate.
func applyRequestDefaults(candidate model.Candidates, req CandidateSinkRequest) (model.Candidates, error) {
	if candidate.SourceAbbr == "" {
		candidate.SourceAbbr = req.SourceAbbr
	}
	if candidate.SourceAbbr == "" {
		return candidate, ErrMissingSourceID
	}

	candidate.TraceID = strings.TrimSpace(candidate.TraceID)
	if candidate.TraceID == "" {
		candidate.TraceID = strings.TrimSpace(req.TraceID)
	}
	if candidate.TraceID == "" {
		return candidate, ErrMissingTraceID
	}

	candidate.IngestionMethod = strings.TrimSpace(candidate.IngestionMethod)
	if candidate.IngestionMethod == "" {
		candidate.IngestionMethod = strings.TrimSpace(req.IngestionMethod)
	}
	if candidate.IngestionMethod == "" {
		candidate.IngestionMethod = repo.IngestionMethodDirectory
	}

	if candidate.BatchID == uuid.Nil {
		candidate.BatchID = req.BatchID
	}

	candidate.Metadata = mergeMetadata(req.DefaultMetadata, candidate.Metadata)

	return candidate, nil
}

// toUpsertCandidateParams converts a Candidates model to UpsertCandidateParams.
func toUpsertCandidateParams(candidate model.Candidates) (repo.UpsertCandidateParams, error) {
	params := repo.UpsertCandidateParams{
		BatchID:         candidate.BatchID,
		Fingerprint:     candidate.Fingerprint(),
		SourceAbbr:      candidate.SourceAbbr,
		Title:           candidate.Title,
		URL:             candidate.URL,
		TraceID:         candidate.TraceID,
		IngestionMethod: candidate.IngestionMethod,
	}

	if description := strings.TrimSpace(candidate.Description); description != "" {
		params.Description = &description
	}
	if !candidate.PublishedAt.IsZero() {
		publishedAt := candidate.PublishedAt
		params.PublishedAt = &publishedAt
	}

	if len(candidate.Metadata) > 0 {
		b, err := json.Marshal(candidate.Metadata)
		if err != nil {
			return params, fmt.Errorf("marshal metadata: %w", err)
		}
		params.Metadata = b
	}

	return params, nil
}

// mergeMetadata merges default metadata with candidate metadata.
func mergeMetadata(defaultMetadata, candidateMetadata map[string]any) map[string]any {
	if len(defaultMetadata) == 0 && len(candidateMetadata) == 0 {
		return nil
	}
	return utils.MergeMap(defaultMetadata, candidateMetadata)
}

func shouldCreatePageFetch(sourceType string) bool {
	return strings.EqualFold(strings.TrimSpace(sourceType), repo.SourceTypeParty)
}

// createPageFetchTask inserts a PAGE_FETCH task for the given candidate.
// Duplicate active tasks (same URL already PENDING/RUNNING) are silently ignored.
// candidate_id is stored in meta for logging and observability in the collector.
func (s *PersistingCandidateSink) createPageFetchTask(ctx context.Context, stored repo.Candidate, req CandidateSinkRequest) error {
	meta, err := json.Marshal(map[string]any{"candidate_id": stored.ID.String()})
	if err != nil {
		return fmt.Errorf("marshal page fetch meta: %w", err)
	}
	_, err = s.tasks.CreateTask(ctx, repo.CreateTaskParams{
		BatchID:    stored.BatchID,
		Kind:       repo.TaskKindPageFetch,
		SourceType: req.SourceType,
		SourceAbbr: stored.SourceAbbr,
		URL:        stored.URL,
		Meta:       meta,
		TraceID:    stored.TraceID,
	})
	if err != nil && !errors.Is(err, repo.ErrTaskAlreadyActive) {
		return err
	}
	return nil
}
