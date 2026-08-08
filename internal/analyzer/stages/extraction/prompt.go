package extraction

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"text/template"

	"github.com/ChiaYuChang/prism/internal/analyzer/llmapi"
)

//go:embed prompt.tmpl
var DefaultPromptTemplateText string

// PromptData represents the data fed into the extraction prompt template.
type PromptData struct {
	Title   string
	Content string
	Repair  *PromptRepair
}

// PromptRepair encapsulates the serialized repair hint for the prompt template.
type PromptRepair struct {
	PreviousOutput string
	Errors         string
}

// RenderPrompt generates the final LLM prompt string for Stage 1.
// If hint is not nil, a repair section is included with the serialized previous output and errors.
func RenderPrompt(tmpl *template.Template, title, content string, hint *llmapi.RepairHint) (string, error) {
	data := PromptData{
		Title:   title,
		Content: content,
	}

	if hint != nil {
		// Use standard json.Marshal so that it is exactly syntactically valid JSON.
		prevBytes, err := json.MarshalIndent(hint.PreviousOutput, "", "  ")
		if err != nil {
			return "", err
		}

		errBytes, err := json.MarshalIndent(hint.Errors, "", "  ")
		if err != nil {
			return "", err
		}

		data.Repair = &PromptRepair{
			PreviousOutput: string(prevBytes),
			Errors:         string(errBytes),
		}
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
