package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	pkgschema "github.com/ChiaYuChang/prism/pkg/schema"
	"github.com/go-viper/mapstructure/v2"
)

var (
	ErrMissingAPIKEY = errors.New("missing api key")

	ErrCfgModError    = errors.New("config modification error")
	ErrCfgValError    = errors.New("config validation error")
	ErrCliCreateError = errors.New("client creation error")

	ErrGenAPIError        = errors.New("content generation API error")
	ErrEmbedAPIError      = errors.New("embedding API error")
	ErrEmbedBatchAPIError = errors.New("batch embedding API error")
)

// ResponseFormat defines the expected format of the LLM output.
type ResponseFormat string

const (
	ResponseFormatText       ResponseFormat = "text"
	ResponseFormatJsonSchema ResponseFormat = "json_schema"
)

type Usage struct {
	InputTokenCount  int `json:"input_token_count"`
	OutputTokenCount int `json:"output_token_count"`
	TotalTokenCount  int `json:"total_token_count"`
}

// Generator defines the interface for text generation (LLMs).
type Generator interface {
	Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error)
}

// Embedder defines the interface for creating vector embeddings.
type Embedder interface {
	Embed(ctx context.Context, req *EmbedRequest) (*EmbedResponse, error)
}

type Provider interface {
	Generator
	Embedder
}

// DecodeJsonSchema validates and decodes a JSON-schema payload into out.
func DecodeJsonSchema(schema pkgschema.JSONSchema, in string, out any) error {
	if out == nil {
		return fmt.Errorf("%w: output target is nil", ErrStructuredResponseDecode)
	}

	if schema.Schema == nil {
		return ErrMissingResponseSchema
	}

	if in == "" {
		return ErrEmptyResponsePayload
	}

	var raw map[string]any
	if err := json.Unmarshal([]byte(in), &raw); err != nil {
		return fmt.Errorf("%w: %s", ErrStructuredResponsePayload, err)
	}

	if err := schema.ApplyDefaults(raw); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidSchemaDefaults, err)
	}

	if err := schema.Validate(raw); err != nil {
		return fmt.Errorf("%w: %s", ErrStructuredResponseValidation, err)
	}

	decoder, err := mapstructure.NewDecoder(
		&mapstructure.DecoderConfig{
			TagName: "json",
			Result:  out,
		})
	if err != nil {
		return fmt.Errorf("%w: %s", ErrStructuredResponseDecode, err)
	}
	if err := decoder.Decode(raw); err != nil {
		return fmt.Errorf("%w: %s", ErrStructuredResponseDecode, err)
	}
	return nil
}
