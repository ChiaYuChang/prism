package extraction

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"text/template"

	"github.com/ChiaYuChang/prism/internal/analyzer/llmapi"
)

//go:embed system_instruction.txt
var DefaultSystemInstructionText string

const RequestTemplateText = `
<title>
{{ .Title }}
</title>

<article>
{{ .Content }}
</article>
{{- if .Repair }}

Previous result:
<previous_output>
{{ .Repair.PreviousOutput }}
</previous_output>

Validation errors:
<validation_errors>
{{ .Repair.Errors }}
</validation_errors>
{{- end }}
`

// RequestData represents the data fed into the request envelope template.
type RequestData struct {
	Title   string
	Content string
	Repair  *RequestRepair
}

// RequestRepair encapsulates the serialized repair hint for the request envelope template.
type RequestRepair struct {
	PreviousOutput string
	Errors         string
}

// RenderRequest generates the final LLM request string (the user prompt).
// If hint is not nil, a repair section is included with the serialized previous output and errors.
func RenderRequest(tmpl *template.Template, title, content string, hint *llmapi.RepairHint) (string, error) {
	data := RequestData{
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

		data.Repair = &RequestRepair{
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
