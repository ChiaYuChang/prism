package extraction_test

import (
	"encoding/json"
	"testing"

	"github.com/ChiaYuChang/prism/internal/analyzer/stages/extraction"
	"github.com/stretchr/testify/require"
)

func TestSchemaValidExtractionOutputPasses(t *testing.T) {
	s := extraction.Schema()
	
	validJSON := `{
		"summary": "Valid summary",
		"statements": [
			{
				"statement": "This is a statement",
				"type": "factual",
				"tags": ["prediction"],
				"sentiment": "neutral",
				"source": {
					"name": "Alice",
					"type": "person",
					"mode": "direct"
				},
				"importance": "primary",
				"evidence": {
					"summary": "Evidence summary",
					"start_with": "start",
					"end_with": "end",
					"quote": null
				}
			}
		],
		"entities": [
			{
				"name": "Alice",
				"type": "person"
			}
		]
	}`

	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(validJSON), &m))
	
	err := s.Validate(m)
	require.NoError(t, err)
}

func TestSchemaExplicitNullFieldsAreAccepted(t *testing.T) {
	s := extraction.Schema()
	
	validJSON := `{
		"summary": "Valid summary",
		"statements": [
			{
				"statement": "This is a statement",
				"type": "factual",
				"tags": [],
				"sentiment": "neutral",
				"source": {
					"name": null,
					"type": "none",
					"mode": "unattributed"
				},
				"importance": "primary",
				"evidence": {
					"summary": "Evidence summary",
					"start_with": null,
					"end_with": null,
					"quote": "quote text"
				}
			}
		],
		"entities": []
	}`

	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(validJSON), &m))
	
	err := s.Validate(m)
	require.NoError(t, err)
}

func TestSchemaMissingNullableFieldIsRejected(t *testing.T) {
	s := extraction.Schema()
	
	invalidJSON := `{
		"summary": "Valid summary",
		"statements": [
			{
				"statement": "This is a statement",
				"type": "factual",
				"tags": [],
				"sentiment": "neutral",
				"source": {
					"type": "none",
					"mode": "unattributed"
				},
				"importance": "primary",
				"evidence": {
					"summary": "Evidence summary",
					"start_with": null,
					"end_with": null,
					"quote": "quote text"
				}
			}
		],
		"entities": []
	}`

	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(invalidJSON), &m))
	
	err := s.Validate(m)
	require.Error(t, err)
	require.Contains(t, err.Error(), "name")
}

func TestSchemaUnknownStatementTypeIsRejected(t *testing.T) {
	s := extraction.Schema()
	
	invalidJSON := `{
		"summary": "Valid summary",
		"statements": [
			{
				"statement": "This is a statement",
				"type": "claim",
				"tags": [],
				"sentiment": "neutral",
				"source": {
					"name": null,
					"type": "none",
					"mode": "unattributed"
				},
				"importance": "primary",
				"evidence": {
					"summary": "Evidence summary",
					"start_with": null,
					"end_with": null,
					"quote": "quote text"
				}
			}
		],
		"entities": []
	}`

	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(invalidJSON), &m))
	
	err := s.Validate(m)
	require.Error(t, err)
	require.Contains(t, err.Error(), "type")
}

func TestSchemaEmptyTagsArrayIsAccepted(t *testing.T) {
	s := extraction.Schema()
	
	validJSON := `{
		"summary": "Valid summary",
		"statements": [
			{
				"statement": "This is a statement",
				"type": "factual",
				"tags": [],
				"sentiment": "neutral",
				"source": {
					"name": null,
					"type": "none",
					"mode": "unattributed"
				},
				"importance": "primary",
				"evidence": {
					"summary": "Evidence summary",
					"start_with": null,
					"end_with": null,
					"quote": "quote text"
				}
			}
		],
		"entities": []
	}`

	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(validJSON), &m))
	
	err := s.Validate(m)
	require.NoError(t, err)
}

func TestSchemaMissingStatementFieldIsRejected(t *testing.T) {
	s := extraction.Schema()
	
	invalidJSON := `{
		"summary": "Valid summary",
		"statements": [
			{
				"statement": "This is a statement",
				"type": "factual",
				"tags": [],
				"source": {
					"name": null,
					"type": "none",
					"mode": "unattributed"
				},
				"importance": "primary",
				"evidence": {
					"summary": "Evidence summary",
					"start_with": null,
					"end_with": null,
					"quote": "quote text"
				}
			}
		],
		"entities": []
	}`

	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(invalidJSON), &m))
	
	err := s.Validate(m)
	require.Error(t, err)
	require.Contains(t, err.Error(), "sentiment")
}

func TestSchemaEmptyStatementsArrayIsRejected(t *testing.T) {
	s := extraction.Schema()
	
	invalidJSON := `{
		"summary": "Valid summary",
		"statements": [],
		"entities": []
	}`

	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(invalidJSON), &m))
	
	err := s.Validate(m)
	require.Error(t, err)
}

func TestSchemaEmptyEntitiesArrayIsAccepted(t *testing.T) {
	s := extraction.Schema()
	
	validJSON := `{
		"summary": "Valid summary",
		"statements": [
			{
				"statement": "This is a statement",
				"type": "factual",
				"tags": [],
				"sentiment": "neutral",
				"source": {
					"name": null,
					"type": "none",
					"mode": "unattributed"
				},
				"importance": "primary",
				"evidence": {
					"summary": "Evidence summary",
					"start_with": null,
					"end_with": null,
					"quote": "quote text"
				}
			}
		],
		"entities": []
	}`

	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(validJSON), &m))
	
	err := s.Validate(m)
	require.NoError(t, err)
}

func TestSchemaMetadataIsStable(t *testing.T) {
	s := extraction.Schema()
	
	require.Equal(t, "article_extraction_result", s.Name)
	require.Equal(t, 1, s.Version)
}

func TestSchemaOpenAINullability(t *testing.T) {
	s := extraction.Schema()
	
	m := s.MustToOpenAI()
	
	b, err := json.MarshalIndent(m, "", "  ")
	require.NoError(t, err)
	
	jsonStr := string(b)
	
	// Check that we didn't lose nullability in the generated schema for start_with
	require.Contains(t, jsonStr, `"start_with"`)
	require.Contains(t, jsonStr, `"name"`)
	
	require.NotNil(t, m)
}
