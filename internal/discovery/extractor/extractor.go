package extractor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ChiaYuChang/prism/internal/llm"
	"github.com/ChiaYuChang/prism/internal/model"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/pkg/logger"
	"github.com/ChiaYuChang/prism/pkg/utils"
	"go.opentelemetry.io/otel/trace"
)

var (
	DefaultTemperature = 0.2

	ErrNilExtractionInput      = errors.New("extraction input is nil")
	ErrFailedToCreateExtractor = errors.New("failed to create extractor")
	ErrFailedToDecodeOutput    = errors.New("failed to decode extraction output")
)

// Extractor handles the high-level business logic of extracting keywords from news.
type Extractor struct {
	generator llm.Generator
	model     string
	prompt    string
	logger    *slog.Logger
	tracer    trace.Tracer
}

// NewExtractor creates a new Keyword Extractor.
func NewExtractor(generator llm.Generator, logger *slog.Logger, tracer trace.Tracer, model, prompt string) (*Extractor, error) {
	if generator == nil {
		return nil, fmt.Errorf("%w: generator is nil", ErrFailedToCreateExtractor)
	}

	if model == "" {
		return nil, fmt.Errorf("%w: model is empty", ErrFailedToCreateExtractor)
	}

	if prompt == "" {
		return nil, fmt.Errorf("%w: prompt is empty", ErrFailedToCreateExtractor)
	}

	if logger == nil {
		return nil, fmt.Errorf("%w: logger is nil", ErrFailedToCreateExtractor)
	}

	if tracer == nil {
		return nil, fmt.Errorf("%w: tracer is nil", ErrFailedToCreateExtractor)
	}

	return &Extractor{
		generator: generator,
		model:     model,
		prompt:    prompt,
		logger:    logger,
		tracer:    tracer,
	}, nil
}

// Extract analyzes the content and returns structured insights.
func (e *Extractor) Extract(ctx context.Context, in *model.ExtractionInput) (*model.ExtractionOutput, error) {
	tid := obs.ExtractTraceID(ctx)
	uid := obs.ExtractUserID(ctx)

	l := logger.WithHook(e.logger,
		logger.SinceHook("time", time.Now()),
		func(ctx context.Context, r slog.Record) slog.Record {
			r.Add("trace_id", tid)
			r.Add("user_id", uid)
			return r
		})

	if in == nil {
		return nil, ErrNilExtractionInput
	}

	l.DebugContext(ctx, "extractor started",
		slog.String("model", e.model),
		slog.Int("title_len", len(in.Title)),
		slog.Int("body_len", len(in.Body)),
	)

	content, err := json.Marshal(in)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal extraction request: %w", err)
	}

	req := &llm.GenerateRequest{
		Model:             e.model,
		SystemInstruction: e.prompt,
		Prompt:            string(content),
		Temperature:       utils.Ptr(float32(DefaultTemperature)),
		Format:            llm.ResponseFormatJsonSchema,
		JSONSchema:        ExtractionResultJSONSchema,
	}

	resp, err := e.generator.Generate(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("llm extraction failed: %w", err)
	}

	var out model.ExtractionOutput
	if err := resp.DecodeJSONSchema(&out); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFailedToDecodeOutput, err)
	}

	l.DebugContext(ctx, "extractor completed",
		slog.String("model", e.model),
		slog.Int("entity_count", len(out.Entities)),
		slog.Int("topic_count", len(out.Topics)),
		slog.Int("phrase_count", len(out.Phrases)),
	)
	return &out, nil
}
