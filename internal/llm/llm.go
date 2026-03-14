package llm

import (
	"context"
	"errors"

	"github.com/ChiaYuChang/prism/pkg/utils"
)

const (
	DefaultTemperature = 1.0
	DefaultTopP        = 0.95
	DefaultTopK        = 64
)

var (
	ErrMissingAPIKEY = errors.New("missing api key")
)

// ResponseFormat defines the expected format of the LLM output.
type ResponseFormat string

const (
	ResponseFormatText       ResponseFormat = "text"
	ResponseFormatJsonSchema ResponseFormat = "json_schema"
)

type Provider interface {
	Generator
	Embedder
}

// Generator defines the interface for text generation (LLMs).
type Generator interface {
	Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error)
}

// Embedder defines the interface for creating vector embeddings.
type Embedder interface {
	Embed(ctx context.Context, req *EmbedRequest) (*EmbedResponse, error)
}

// GenerateRequest encapsulates all parameters for a generation task.
type GenerateRequest struct {
	Model             string
	SystemInstruction string
	Prompt            string
	Temperature       *float32
	TopP              *float32
	TopK              *int
	MaxTokens         *int
	Meta              map[string]string
	Format            ResponseFormat
	JSONSchema        JsonSchema
}

func NewGenerateRequest(model, instruction, prompt string) *GenerateRequest {
	return &GenerateRequest{
		Model:             model,
		SystemInstruction: instruction,
		Prompt:            prompt,
		Temperature:       utils.Ptr(float32(DefaultTemperature)),
		TopP:              utils.Ptr(float32(DefaultTopP)),
		TopK:              utils.Ptr(int(DefaultTopK)),
		Meta:              make(map[string]string),
	}
}

// GenerateResponse holds the result and any underlying provider-specific data.
type GenerateResponse struct {
	Model      string
	Text       string
	Raw        any
	JsonSchema JsonSchema
}

// EmbedRequest encapsulates parameters for an embedding task.
type EmbedRequest struct {
	Model      string
	Input      []string
	Dimentions int
	Meta       map[string]string
}

func NewEmbedRequest(model string, input ...string) *EmbedRequest {
	return &EmbedRequest{
		Model: model,
		Input: input,
		Meta:  make(map[string]string),
	}
}

// EmbedResponse holds the resulting vectors.
type EmbedResponse struct {
	Model   string
	Vectors [][]float32
	Raw     any
}

type BatchJobRequest struct {
	DisplayName string
}

type BatchJobResponse struct {
	Name        string
	DisplayName string
	State       string
	OutFileName string
	Raw         any
}
