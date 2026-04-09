package sink

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/model"
	"github.com/ChiaYuChang/prism/internal/repo"
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
	SourceID        int32              `json:"source_id,omitempty"`
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
// PG repository. By storing briefs here, the system optimizes for recall while
// deliberately waiting for explicit triggers to promote candidates into full contents.
type PersistingCandidateSink struct {
	logger    *slog.Logger
	tracer    trace.Tracer
	repo      repo.Scout
	publisher message.PageFetchPublisher
}

func NewPersistingCandidateSink(
	logger *slog.Logger,
	tracer trace.Tracer,
	scoutRepo repo.Scout,
	publisher message.PageFetchPublisher,
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

	return &PersistingCandidateSink{
		logger:    logger,
		tracer:    tracer,
		repo:      scoutRepo,
		publisher: publisher,
	}, nil
}

// Handle executes the persistence of candidates into the database using UpsertCandidate.
// Parameters are merged with request-level metadata to preserve batch context and TraceIDs.
func (s *PersistingCandidateSink) Handle(ctx context.Context, req CandidateSinkRequest) error {
	ctx, span := s.tracer.Start(ctx, "discovery.sink.candidate.handle")
	defer span.End()

	for _, candidate := range req.Candidates {
		params, err := toUpsertCandidateParams(candidate, req)
		if err != nil {
			return err
		}
		stored, err := s.repo.UpsertCandidate(ctx, params)
		if err != nil {
			return fmt.Errorf("upsert candidate %s: %w", params.URL, err)
		}
		if shouldEmitPageFetch(req.SourceType) && s.publisher != nil {
			if err := s.publisher.PublishPageFetch(ctx, &message.PageFetchSignal{
				CandidateID: stored.ID,
				BatchID:     stored.BatchID,
				SourceID:    stored.SourceID,
				SourceType:  req.SourceType,
				URL:         stored.URL,
				TraceID:     stored.TraceID,
				SentAt:      timeNow(),
			}); err != nil {
				return fmt.Errorf("publish page fetch for %s: %w", stored.URL, err)
			}
		}
	}

	s.logger.DebugContext(ctx, "candidate sink persisted candidates",
		slog.String("source_url", req.SourceURL),
		slog.Int("count", len(req.Candidates)),
	)

	return nil
}

var timeNow = func() time.Time {
	return time.Now()
}

func toUpsertCandidateParams(candidate model.Candidates, req CandidateSinkRequest) (repo.UpsertCandidateParams, error) {
	sourceID := req.SourceID
	if candidate.SourceID != 0 {
		sourceID = int32(candidate.SourceID)
	}
	if sourceID == 0 {
		return repo.UpsertCandidateParams{}, ErrMissingSourceID
	}

	traceID := strings.TrimSpace(candidate.TraceID)
	if traceID == "" {
		traceID = strings.TrimSpace(req.TraceID)
	}
	if traceID == "" {
		return repo.UpsertCandidateParams{}, ErrMissingTraceID
	}

	ingestionMethod := strings.TrimSpace(candidate.IngestionMethod)
	if ingestionMethod == "" {
		ingestionMethod = strings.TrimSpace(req.IngestionMethod)
	}
	if ingestionMethod == "" {
		ingestionMethod = "DIRECTORY"
	}

	params := repo.UpsertCandidateParams{
		BatchID:         mergeBatchID(req.BatchID, candidate.BatchID),
		Fingerprint:     candidate.Fingerprint(),
		SourceID:        sourceID,
		Title:           candidate.Title,
		URL:             candidate.URL,
		TraceID:         traceID,
		IngestionMethod: ingestionMethod,
	}

	if description := strings.TrimSpace(candidate.Description); description != "" {
		params.Description = &description
	}
	if !candidate.PublishedAt.IsZero() {
		publishedAt := candidate.PublishedAt
		params.PublishedAt = &publishedAt
	}

	metadata, err := mergeMetadata(req.DefaultMetadata, candidate.Metadata)
	if err != nil {
		return repo.UpsertCandidateParams{}, fmt.Errorf("marshal metadata for %s: %w", candidate.URL, err)
	}
	params.Metadata = metadata

	return params, nil
}

func mergeBatchID(requestBatchID uuid.UUID, candidateBatchID uuid.UUID) uuid.UUID {
	if candidateBatchID != uuid.Nil {
		return candidateBatchID
	}
	return requestBatchID
}

func mergeMetadata(defaultMetadata, candidateMetadata map[string]any) ([]byte, error) {
	if len(defaultMetadata) == 0 && len(candidateMetadata) == 0 {
		return nil, nil
	}

	metadata := make(map[string]any, len(defaultMetadata)+len(candidateMetadata))
	for key, value := range defaultMetadata {
		metadata[key] = value
	}
	for key, value := range candidateMetadata {
		metadata[key] = value
	}

	return json.Marshal(metadata)
}

func shouldEmitPageFetch(sourceType string) bool {
	return strings.EqualFold(strings.TrimSpace(sourceType), "PARTY")
}
