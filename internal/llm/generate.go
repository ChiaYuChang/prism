package llm

import (
	"errors"

	pkgschema "github.com/ChiaYuChang/prism/pkg/schema"
)

var (
	ErrNilGenerateResponse          = errors.New("generate response is nil")
	ErrMissingResponseSchema        = errors.New("response schema is missing")
	ErrEmptyResponsePayload         = errors.New("response payload is empty")
	ErrStructuredResponsePayload    = errors.New("structured response payload is invalid")
	ErrInvalidSchemaDefaults        = errors.New("schema defaults are invalid")
	ErrStructuredResponseValidation = errors.New("structured response does not match the schema")
	ErrStructuredResponseDecode     = errors.New("failed to decode structured response")
)

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
	JSONSchema        pkgschema.JSONSchema
}

func NewGenerateRequest(model, instruction, prompt string) *GenerateRequest {
	return &GenerateRequest{
		Model:             model,
		SystemInstruction: instruction,
		Prompt:            prompt,
	}
}

// GenerateResponse holds the result and any underlying provider-specific data.
type GenerateResponse struct {
	Model      string     `json:"model"`
	Text       string     `json:"text"`
	Usage      Usage      `json:"usage"`
	Raw        any        `json:"raw"`
	JsonSchema pkgschema.JSONSchema `json:"jsonschema"`
}

// DecodeJSONSchema validates and decodes a JSON-schema response into out.
func (r *GenerateResponse) DecodeJSONSchema(out any) error {
	if r == nil {
		return ErrNilGenerateResponse
	}
	return DecodeJsonSchema(r.JsonSchema, r.Text, out)
}
