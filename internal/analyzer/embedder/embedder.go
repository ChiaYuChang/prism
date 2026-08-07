package embedder

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ChiaYuChang/prism/internal/analyzer"
	"github.com/ChiaYuChang/prism/internal/llm"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/trace"
)

var (
	ErrParamMissing     = errors.New("param missing")
	ErrInvalidEmbedding = errors.New("invalid embedding response")
	ErrNoEmbeddableText = errors.New("target has no embeddable text")
)

// Service encapsulates the core business logic for reading articles/candidates from DB,
// generating vector embeddings via llm.Embedder, and persisting vectors back to repo.Embeddings.
type Service struct {
	logger     *slog.Logger
	tracer     trace.Tracer
	embedder   llm.Embedder
	scout      repo.Scout
	pipeline   repo.Pipeline
	embeddings repo.Embeddings
	modelID    int16
	modelName  string
	dimension  int
}

var _ analyzer.ArticleEmbedder = (*Service)(nil)

// NewService instantiates an instrumented Service.
func NewService(
	logger *slog.Logger,
	tracer trace.Tracer,
	embedder llm.Embedder,
	scout repo.Scout,
	pipeline repo.Pipeline,
	embeddings repo.Embeddings,
	modelID int16,
	modelName string,
	dimension int,
) (*Service, error) {
	if logger == nil {
		return nil, fmt.Errorf("%w: logger", ErrParamMissing)
	}
	if tracer == nil {
		return nil, fmt.Errorf("%w: tracer", ErrParamMissing)
	}
	if embedder == nil {
		return nil, fmt.Errorf("%w: embedder", ErrParamMissing)
	}
	if scout == nil {
		return nil, fmt.Errorf("%w: scout", ErrParamMissing)
	}
	if pipeline == nil {
		return nil, fmt.Errorf("%w: pipeline", ErrParamMissing)
	}
	if embeddings == nil {
		return nil, fmt.Errorf("%w: embeddings", ErrParamMissing)
	}
	if modelID < 1 {
		return nil, fmt.Errorf("%w: model_id", ErrParamMissing)
	}
	if strings.TrimSpace(modelName) == "" {
		return nil, fmt.Errorf("%w: model_name", ErrParamMissing)
	}
	if dimension < 1 {
		return nil, fmt.Errorf("%w: dimension", ErrParamMissing)
	}

	return &Service{
		logger:     logger,
		tracer:     tracer,
		embedder:   embedder,
		scout:      scout,
		pipeline:   pipeline,
		embeddings: embeddings,
		modelID:    modelID,
		modelName:  modelName,
		dimension:  dimension,
	}, nil
}

type embeddingInput struct {
	category string
	text     string
}

// EmbedArticle implements analyzer.ArticleEmbedder for single title + content texts.
func (s *Service) EmbedArticle(ctx context.Context, title string, content string) ([]float32, error) {
	text := strings.TrimSpace(title + "\n\n" + content)
	if text == "" {
		return nil, ErrNoEmbeddableText
	}
	resp, err := s.embedder.Embed(ctx, &llm.EmbedRequest{
		Model:      s.modelName,
		Input:      []string{text},
		Dimensions: s.dimension,
	})
	if err != nil {
		return nil, fmt.Errorf("llm embed: %w", err)
	}
	if resp == nil || len(resp.Vectors) == 0 || len(resp.Vectors[0]) != s.dimension {
		return nil, fmt.Errorf("%w: invalid vector output", ErrInvalidEmbedding)
	}
	return resp.Vectors[0], nil
}

// EmbedCandidate reads a candidate brief by ID from DB, computes input hashes,
// generates vector embeddings, and upserts them to candidate_embeddings.
func (s *Service) EmbedCandidate(ctx context.Context, candidateID uuid.UUID, traceID string) error {
	ctx, span := s.tracer.Start(ctx, "analyzer.embedder.embed_candidate")
	defer span.End()

	candidate, err := s.scout.GetCandidateByID(ctx, candidateID)
	if err != nil {
		return fmt.Errorf("get candidate %s: %w", candidateID, err)
	}
	return s.EmbedCandidateSnapshot(ctx, candidate, traceID)
}

// EmbedCandidateSnapshot embeds a candidate captured by a pipeline input
// snapshot, avoiding a mutable live-row read.
func (s *Service) EmbedCandidateSnapshot(ctx context.Context, candidate repo.Candidate, traceID string) error {
	ctx, span := s.tracer.Start(ctx, "analyzer.embedder.embed_candidate_snapshot")
	defer span.End()

	inputs := make([]embeddingInput, 0, 2)
	if text := strings.TrimSpace(candidate.Title); text != "" {
		inputs = append(inputs, embeddingInput{category: repo.EmbeddingCategoryTitle, text: text})
	}
	if candidate.Description != nil {
		if text := strings.TrimSpace(*candidate.Description); text != "" {
			inputs = append(inputs, embeddingInput{category: repo.EmbeddingCategoryBrief, text: text})
		}
	}
	if len(inputs) == 0 {
		return fmt.Errorf("%w: candidate %s", ErrNoEmbeddableText, candidate.ID)
	}

	return s.processAndPersistInputs(ctx, candidate.ID, inputs, traceID, func(input embeddingInput, vector []float32, hash string) error {
		_, err := s.embeddings.UpsertCandidateEmbedding(ctx, repo.CreateCandidateEmbeddingParams{
			CandidateID: candidate.ID,
			ModelID:     s.modelID,
			Category:    input.category,
			InputHash:   hash,
			Vector:      vector,
			TraceID:     traceID,
		})
		return err
	})
}

// EmbedContent reads an article content by ID from DB, computes canonical text,
// generates vector embeddings, and upserts them to content_embeddings.
func (s *Service) EmbedContent(ctx context.Context, contentID uuid.UUID, traceID string) error {
	ctx, span := s.tracer.Start(ctx, "analyzer.embedder.embed_content")
	defer span.End()

	content, err := s.pipeline.GetContentByID(ctx, contentID)
	if err != nil {
		return fmt.Errorf("get content %s: %w", contentID, err)
	}
	if content.DeletedAt != nil {
		return nil
	}
	return s.EmbedContentSnapshot(ctx, content, traceID)
}

// EmbedContentSnapshot embeds content captured by a pipeline input snapshot,
// including records later soft-deleted from the live content table.
func (s *Service) EmbedContentSnapshot(ctx context.Context, content repo.Content, traceID string) error {
	ctx, span := s.tracer.Start(ctx, "analyzer.embedder.embed_content_snapshot")
	defer span.End()

	text := CanonicalDocumentText(content)
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("%w: content %s", ErrNoEmbeddableText, content.ID)
	}

	inputs := []embeddingInput{{category: "full", text: text}}
	return s.processAndPersistInputs(ctx, content.ID, inputs, traceID, func(input embeddingInput, vector []float32, hash string) error {
		_, err := s.embeddings.UpsertContentEmbedding(ctx, repo.CreateContentEmbeddingParams{
			ContentID: content.ID,
			ModelID:   s.modelID,
			InputHash: hash,
			Vector:    vector,
			TraceID:   traceID,
		})
		return err
	})
}

func (s *Service) processAndPersistInputs(
	ctx context.Context,
	targetID uuid.UUID,
	inputs []embeddingInput,
	traceID string,
	persistFn func(embeddingInput, []float32, string) error,
) error {
	pending := make([]embeddingInput, 0, len(inputs))
	hashes := make(map[string]string, len(inputs))

	for _, input := range inputs {
		hash := HashInput(input.text)
		hashes[input.category] = hash

		var existing string
		var err error
		if input.category == repo.EmbeddingCategoryTitle || input.category == repo.EmbeddingCategoryBrief {
			existing, err = s.embeddings.GetCandidateEmbeddingInputHash(ctx, targetID, s.modelID, input.category)
		} else {
			existing, err = s.embeddings.GetContentEmbeddingInputHash(ctx, targetID, s.modelID)
		}
		if err == nil && existing == hash {
			continue // Unchanged hash - skip re-embedding
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("get existing embedding hash: %w", err)
		}
		pending = append(pending, input)
	}

	if len(pending) == 0 {
		return nil
	}

	texts := make([]string, len(pending))
	for i := range pending {
		texts[i] = pending[i].text
	}

	resp, err := s.embedder.Embed(ctx, &llm.EmbedRequest{
		Model:      s.modelName,
		Input:      texts,
		Dimensions: s.dimension,
		Meta:       map[string]string{"trace_id": traceID},
	})
	if err != nil {
		return fmt.Errorf("llm embed %d inputs: %w", len(texts), err)
	}

	if resp == nil || len(resp.Vectors) != len(pending) {
		return fmt.Errorf("%w: got %d vectors for %d inputs", ErrInvalidEmbedding, len(resp.Vectors), len(pending))
	}

	for i, vector := range resp.Vectors {
		if len(vector) != s.dimension {
			return fmt.Errorf("%w: vector %d dimension mismatch (got %d, want %d)", ErrInvalidEmbedding, i, len(vector), s.dimension)
		}
		if err := persistFn(pending[i], vector, hashes[pending[i].category]); err != nil {
			return fmt.Errorf("persist vector %d: %w", i, err)
		}
	}
	return nil
}

// CanonicalDocumentText builds standardized text for full content embedding.
func CanonicalDocumentText(content repo.Content) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Title: %s\n", strings.TrimSpace(content.Title))
	fmt.Fprintf(&b, "Type: %s\n", strings.TrimSpace(content.Type))
	fmt.Fprintf(&b, "Source: %s\n", strings.TrimSpace(content.SourceAbbr))
	if !content.PublishedAt.IsZero() {
		fmt.Fprintf(&b, "Published: %s\n", content.PublishedAt.UTC().Format(time.RFC3339))
	}
	if content.Author != nil && strings.TrimSpace(*content.Author) != "" {
		fmt.Fprintf(&b, "Author: %s\n", strings.TrimSpace(*content.Author))
	}
	b.WriteString("\n")
	b.WriteString(strings.TrimSpace(content.Content))
	return b.String()
}

// HashInput generates SHA-256 hex string of input text for dedup.
func HashInput(input string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(input)))
}
