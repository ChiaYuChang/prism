package extraction

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/ChiaYuChang/prism/internal/analyzer/llmapi"
	"github.com/ChiaYuChang/prism/internal/llm"
	pkgschema "github.com/ChiaYuChang/prism/pkg/schema"
)

var (
	ErrEmptyArticleContent = errors.New("article content cannot be empty")
)

type Stage struct {
	generator llm.Generator
	model     string
	schema    pkgschema.JSONSchema
}

// NewStage creates a new extraction Stage.
func NewStage(generator llm.Generator, model string) *Stage {
	return &Stage{
		generator: generator,
		model:     model,
		schema:    Schema(),
	}
}

func (s *Stage) Name() string {
	return "extraction"
}

func (s *Stage) PreProcess(ctx context.Context, p *llmapi.Packet[Input, any, Output]) error {
	in := p.Input()
	if strings.TrimSpace(in.Content) == "" {
		return ErrEmptyArticleContent
	}

	// We can use a general system instruction, though the prompt template also acts as one.
	systemInstruction := "You are an expert news analyst tasked with extracting structured information from a single news article."

	renderedPrompt, err := RenderPrompt(in.Title, in.Content, p.RepairHint)
	if err != nil {
		return err
	}

	req := llm.NewGenerateRequest(
		s.model,
		systemInstruction,
		renderedPrompt,
	)

	req.Format = llm.ResponseFormatJsonSchema
	req.JSONSchema = s.schema

	p.Request = req
	return nil
}

func (s *Stage) APICall(ctx context.Context, p *llmapi.Packet[Input, any, Output]) error {
	resp, err := s.generator.Generate(ctx, p.Request)
	if err != nil {
		var transient *llm.TransientError
		if errors.As(err, &transient) {
			return &llmapi.RetryError{Cause: err}
		}
		return err
	}
	p.Response = resp
	return nil
}

func (s *Stage) PostProcess(ctx context.Context, p *llmapi.Packet[Input, any, Output]) error {
	var out Output
	if err := p.Response.DecodeJSONSchema(&out); err != nil {
		return err // Decode failures are fatal for now
	}

	semanticErrs := Validate(&out)
	if len(semanticErrs) > 0 {
		return newReAttemptError(out, semanticErrs)
	}

	groundingErrs := ValidateGrounding(p.Input().Content, &out)
	if len(groundingErrs) > 0 {
		return newReAttemptError(out, groundingErrs)
	}

	p.Output = out
	return nil
}

func newReAttemptError(out Output, validationErrors []llmapi.ValidationError) error {
	previous, err := json.Marshal(out)
	if err != nil {
		return err // Marshaling failure is a programming/fatal error
	}

	return &llmapi.ReAttemptError{
		Cause: errors.New("article extraction validation failed"),
		Hint: &llmapi.RepairHint{
			PreviousOutput: previous,
			Errors:         validationErrors,
		},
	}
}
