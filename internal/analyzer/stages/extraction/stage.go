package extraction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"text/template"

	"github.com/ChiaYuChang/prism/internal/analyzer/llmapi"
	"github.com/ChiaYuChang/prism/internal/llm"
	pkgschema "github.com/ChiaYuChang/prism/pkg/schema"
)

var (
	ErrEmptyArticleContent = errors.New("article content cannot be empty")
)

type Stage struct {
	generator         llm.Generator
	model             string
	schema            pkgschema.JSONSchema
	systemInstruction string
	requestTemplate   *template.Template
}

// NewV1 creates a new extraction Stage using V1 parameters.
func NewV1(ctx context.Context, deps Dependencies, params V1Parameters) (*Stage, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}
	if deps.Generator == nil {
		return nil, errors.New("generator is required")
	}
	if deps.PromptLoader == nil {
		return nil, errors.New("prompt loader is required")
	}

	loaded, err := deps.PromptLoader.Load(ctx, params.PromptID)
	if err != nil {
		return nil, fmt.Errorf("load prompt %s: %w", params.PromptID, err)
	}

	requestTemplate, err := template.New("extraction_request").Parse(RequestTemplateText)
	if err != nil {
		return nil, fmt.Errorf("parse request template: %w", err)
	}

	return &Stage{
		generator:         deps.Generator,
		model:             params.Model,
		schema:            Schema(),
		systemInstruction: string(loaded.Body),
		requestTemplate:   requestTemplate,
	}, nil
}

func (s *Stage) Name() string {
	return "extraction"
}

func (s *Stage) PreProcess(ctx context.Context, p *llmapi.Packet[Input, any, Output]) error {
	in := p.Input()
	if strings.TrimSpace(in.Content) == "" {
		return ErrEmptyArticleContent
	}

	renderedPrompt, err := RenderRequest(s.requestTemplate, in.Title, in.Content, p.RepairHint)
	if err != nil {
		return err
	}

	req := llm.NewGenerateRequest(
		s.model,
		s.systemInstruction,
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
