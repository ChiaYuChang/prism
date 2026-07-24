package scorer

import (
	"github.com/ChiaYuChang/prism/internal/analyzer"
	pkgschema "github.com/ChiaYuChang/prism/pkg/schema"
	"github.com/ChiaYuChang/prism/pkg/utils"
	"github.com/google/jsonschema-go/jsonschema"
)

// StanceScoringResultJSONSchema defines the JSON schema for stance scoring LLM output decoding.
var StanceScoringResultJSONSchema = func() pkgschema.JSONSchema {
	s := pkgschema.NewSkeleton[analyzer.StanceScoringOutput]("stance_scoring_result", 1)
	s.Title = "Stance Scoring Result"
	s.Description = "Stance evaluation result containing scores array for target issues."
	s.Required = []string{"scores"}

	s.Properties["scores"].Description = "Array of stance score objects."
	s.Properties["scores"].MinItems = utils.Ptr(1)
	s.Properties["scores"].Items = &jsonschema.Schema{
		Type:        "object",
		Description: "Stance evaluation score object.",
		Required:    []string{"issue_id", "score", "reasoning", "references"},
		Properties: map[string]*jsonschema.Schema{
			"issue_id": {
				Type:        "string",
				Description: "Exact matching issue ID from input.",
				MinLength:   utils.Ptr(1),
			},
			"score": {
				Type:        "integer",
				Description: "Likert stance score integer from 1 to 5.",
				Minimum:     utils.Ptr(float64(1)),
				Maximum:     utils.Ptr(float64(5)),
			},
			"reasoning": {
				Type:        "string",
				Description: "Detailed deduction in Traditional Chinese.",
				MinLength:   utils.Ptr(1),
			},
			"references": {
				Type:        "array",
				Description: "Exact verbatim quotes extracted from article text.",
				Items: &jsonschema.Schema{
					Type:      "string",
					MinLength: utils.Ptr(1),
				},
			},
		},
		PropertyOrder: []string{"issue_id", "score", "reasoning", "references"},
	}
	return s
}()
