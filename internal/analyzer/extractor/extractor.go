package extractor

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
	ErrFailedToDecodeOutput = errors.New("failed to decode topic extraction output")
)

// Extractor implements analyzer.TopicExtractor using an LLM generator.
type Extractor struct {
	generator llm.Generator
	logger    *slog.Logger
	tracer    trace.Tracer
	model     string
	prompt    string
}

var _ analyzer.TopicExtractor = (*Extractor)(nil)

// NewExtractor creates a new Extractor instance.
func NewExtractor(generator llm.Generator, logger *slog.Logger, tracer trace.Tracer, model, prompt string) (*Extractor, error) {
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

	return &Extractor{
		generator: generator,
		logger:    logger,
		tracer:    tracer,
		model:     model,
		prompt:    prompt,
	}, nil
}

// ExtractTopic analyzes an article collection and returns structured topic metadata, facts, common ground, and issues.
func (e *Extractor) ExtractTopic(ctx context.Context, in *analyzer.TopicExtractionInput) (*analyzer.TopicExtractionOutput, error) {
	ctx, span := e.tracer.Start(ctx, "analyzer.extractor.extract_topic")
	defer span.End()

	if in == nil || len(in.Articles) == 0 {
		return nil, ErrNilInput
	}

	content, err := json.Marshal(in.Articles)
	if err != nil {
		return nil, fmt.Errorf("marshal articles input: %w", err)
	}

	req := &llm.GenerateRequest{
		Model:             e.model,
		SystemInstruction: e.prompt,
		Prompt:            string(content),
		Temperature:       utils.Ptr(float32(0.2)),
		Format:            llm.ResponseFormatJsonSchema,
		JSONSchema:        TopicExtractionResultJSONSchema,
	}

	resp, err := e.generator.Generate(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("llm generate topic extraction: %w", err)
	}

	var out analyzer.TopicExtractionOutput
	if err := resp.DecodeJSONSchema(&out); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFailedToDecodeOutput, err)
	}

	out.Issues = analyzer.AssignIssueIDs(out.Issues)
	return &out, nil
}
