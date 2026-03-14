package analysis

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ChiaYuChang/prism/internal/llm"
	"github.com/ChiaYuChang/prism/internal/model"
	"github.com/ChiaYuChang/prism/pkg/utils"
)

const (
	defaultSystemPrompt = `//TODO`
)

// Extractor handles the high-level business logic of extracting keywords from news.
type Extractor struct {
	provider llm.Provider
	model    string
}

// NewExtractor creates a new Keyword Extractor.
func NewExtractor(provider llm.Provider, modelName string) *Extractor {
	return &Extractor{
		provider: provider,
		model:    modelName,
	}
}

// Extract analyzes the content and returns structured insights.
func (e *Extractor) Extract(ctx context.Context, content string) (*model.ExtractionResult, error) {
	req := &llm.GenerateRequest{
		Model:             e.model,
		SystemInstruction: defaultSystemPrompt,
		Prompt:            content,
		Temperature:       utils.Ptr(float32(0.2)), // Low temperature for consistent extraction
		Format:            llm.ResponseFormatJsonSchema,
	}

	resp, err := e.provider.Generate(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("llm extraction failed: %w", err)
	}

	var result model.ExtractionResult
	if err := json.Unmarshal([]byte(resp.Text), &result); err != nil {
		return nil, fmt.Errorf("failed to parse extraction result: %w", err)
	}

	return &result, nil
}
