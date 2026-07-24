package extractor

import (
	"github.com/ChiaYuChang/prism/internal/analyzer"
	pkgschema "github.com/ChiaYuChang/prism/pkg/schema"
	"github.com/ChiaYuChang/prism/pkg/utils"
	"github.com/google/jsonschema-go/jsonschema"
)

// TopicExtractionResultJSONSchema defines the JSON schema for topic-level extraction LLM output decoding.
var TopicExtractionResultJSONSchema = func() pkgschema.JSONSchema {
	s := pkgschema.NewSkeleton[analyzer.TopicExtractionOutput]("topic_extraction_result", 1)
	s.Title = "Topic Extraction Result"
	s.Description = "Structured topic-level extraction result summarizing facts, common ground, and policy issues."
	s.Required = []string{"topic_name", "facts", "common_ground", "issues"}

	s.Properties["topic_name"].Description = "Neutral, concise title for the overarching public topic in Traditional Chinese."
	s.Properties["topic_name"].MinLength = utils.Ptr(1)

	s.Properties["facts"].Description = "Hard empirical event facts directly documented in the text."
	s.Properties["facts"].MinItems = utils.Ptr(1)
	s.Properties["facts"].Items = &jsonschema.Schema{
		Type:        "string",
		Description: "Empirical event fact in Traditional Chinese.",
		MinLength:   utils.Ptr(1),
	}

	s.Properties["common_ground"].Description = "Shared premises or acknowledged dilemmas agreed upon across factions."
	s.Properties["common_ground"].MinItems = utils.Ptr(1)
	s.Properties["common_ground"].Items = &jsonschema.Schema{
		Type:        "string",
		Description: "Consensus problem statement in Traditional Chinese.",
		MinLength:   utils.Ptr(1),
	}

	s.Properties["issues"].Description = "Controversial issues extracted from news coverage, sorted descending by importance."
	s.Properties["issues"].MinItems = utils.Ptr(1)
	s.Properties["issues"].Items = &jsonschema.Schema{
		Type:        "object",
		Description: "Controversial issue specification with Pro/Con criteria.",
		Required:    []string{"type", "name", "description", "pro_criteria", "con_criteria"},
		Properties: map[string]*jsonschema.Schema{
			"type": {
				Type:        "string",
				Description: "Importance tier of the issue.",
				Enum: []any{
					string(analyzer.IssueTypeMajor),
					string(analyzer.IssueTypeMinor),
				},
			},
			"name": {
				Type:        "string",
				Description: "Short, descriptive name of the controversy.",
				MinLength:   utils.Ptr(1),
			},
			"description": {
				Type:        "string",
				Description: "Scope and core dilemma of the issue.",
				MinLength:   utils.Ptr(1),
			},
			"pro_criteria": {
				Type:        "array",
				Description: "Explicit arguments or data justifying a Support stance.",
				MinItems:    utils.Ptr(1),
				Items: &jsonschema.Schema{
					Type:      "string",
					MinLength: utils.Ptr(1),
				},
			},
			"con_criteria": {
				Type:        "array",
				Description: "Explicit arguments or data justifying an Oppose stance.",
				MinItems:    utils.Ptr(1),
				Items: &jsonschema.Schema{
					Type:      "string",
					MinLength: utils.Ptr(1),
				},
			},
		},
		PropertyOrder: []string{"type", "name", "description", "pro_criteria", "con_criteria"},
	}
	return s
}()
