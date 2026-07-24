package scorer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/ChiaYuChang/prism/internal/analyzer"
	"github.com/ChiaYuChang/prism/internal/llm"
	"github.com/ChiaYuChang/prism/pkg/utils"
	"go.opentelemetry.io/otel/trace"
)

var (
	ErrParamMissing         = errors.New("param missing")
	ErrNilInput             = errors.New("input is nil")
	ErrFailedToDecodeOutput = errors.New("failed to decode stance scoring output")
)

// Scorer implements analyzer.StanceScorer using an LLM generator.
type Scorer struct {
	generator llm.Generator
	logger    *slog.Logger
	tracer    trace.Tracer
	model     string
	prompt    string
}

var _ analyzer.StanceScorer = (*Scorer)(nil)

// NewScorer creates a new Scorer instance.
func NewScorer(generator llm.Generator, logger *slog.Logger, tracer trace.Tracer, model, prompt string) (*Scorer, error) {
	if generator == nil {
		return nil, fmt.Errorf("%w: generator", ErrParamMissing)
	}
	if logger == nil {
		return nil, fmt.Errorf("%w: logger", ErrParamMissing)
	}
	if tracer == nil {
		return nil, fmt.Errorf("%w: tracer", ErrParamMissing)
	}
	if model == "" {
		return nil, fmt.Errorf("%w: model", ErrParamMissing)
	}
	if prompt == "" {
		return nil, fmt.Errorf("%w: prompt", ErrParamMissing)
	}

	return &Scorer{
		generator: generator,
		logger:    logger,
		tracer:    tracer,
		model:     model,
		prompt:    prompt,
	}, nil
}

// ScoreStance evaluates an article's stance toward defined issues and returns Likert (1..5) scores and verbatim quotes.
func (s *Scorer) ScoreStance(ctx context.Context, in *analyzer.StanceScoringInput) (*analyzer.StanceScoringOutput, error) {
	ctx, span := s.tracer.Start(ctx, "analyzer.scorer.score_stance")
	defer span.End()

	if in == nil || len(in.Issues) == 0 {
		return nil, ErrNilInput
	}

	content, err := json.Marshal(in)
	if err != nil {
		return nil, fmt.Errorf("marshal stance scoring payload: %w", err)
	}

	req := &llm.GenerateRequest{
		Model:             s.model,
		SystemInstruction: s.prompt,
		Prompt:            string(content),
		Temperature:       utils.Ptr(float32(0.2)),
		Format:            llm.ResponseFormatJsonSchema,
		JSONSchema:        StanceScoringResultJSONSchema,
	}

	resp, err := s.generator.Generate(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("llm generate stance scoring: %w", err)
	}

	var out analyzer.StanceScoringOutput
	if err := resp.DecodeJSONSchema(&out); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFailedToDecodeOutput, err)
	}

	for _, score := range out.Scores {
		if err := analyzer.ValidateStanceScore(score.Score); err != nil {
			return nil, fmt.Errorf("validate score for issue %s: %w", score.IssueID, err)
		}
	}
	return &out, nil
}
