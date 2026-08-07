package extraction_test

import (
	"encoding/json"
	"testing"

	"github.com/ChiaYuChang/prism/internal/analyzer/stages/extraction"
	"github.com/stretchr/testify/require"
)

func TestOutputStableJSONContract(t *testing.T) {
	name := "Alice"
	startWith := "ABC"
	endWith := "DEF"
	quote := "XYZ"

	expected := extraction.Output{
		Summary: "Test summary",
		Statements: []extraction.Statement{
			{
				Statement:  "A test statement",
				Type:       extraction.StatementTypeFactual,
				Tags:       []extraction.StatementTag{extraction.StatementTagPrediction},
				Sentiment:  extraction.SentimentNeutral,
				Source: extraction.Source{
					Name: &name,
					Type: extraction.SourceTypePerson,
					Mode: extraction.SourceModeDirect,
				},
				Importance: extraction.ImportancePrimary,
				Evidence: extraction.Evidence{
					Summary:   "Evidence summary",
					StartWith: &startWith,
					EndWith:   &endWith,
					Quote:     &quote,
				},
			},
		},
		Entities: []extraction.Entity{
			{
				Name: "Test Entity",
				Type: extraction.EntityTypePrivateSector,
			},
		},
	}

	b, err := json.Marshal(expected)
	require.NoError(t, err)

	// Verify exact JSON payload struct via map
	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(b, &raw))

	// check root
	require.Contains(t, raw, "summary")
	require.Contains(t, raw, "statements")
	require.Contains(t, raw, "entities")

	// check statement
	statements := raw["statements"].([]interface{})
	require.Len(t, statements, 1)
	stmt := statements[0].(map[string]interface{})
	require.Contains(t, stmt, "statement")
	require.Contains(t, stmt, "type")
	require.Contains(t, stmt, "tags")
	require.Contains(t, stmt, "sentiment")
	require.Contains(t, stmt, "source")
	require.Contains(t, stmt, "importance")
	require.Contains(t, stmt, "evidence")

	// check source
	source := stmt["source"].(map[string]interface{})
	require.Contains(t, source, "name")
	require.Contains(t, source, "type")
	require.Contains(t, source, "mode")

	// check evidence
	evidence := stmt["evidence"].(map[string]interface{})
	require.Contains(t, evidence, "summary")
	require.Contains(t, evidence, "start_with")
	require.Contains(t, evidence, "end_with")
	require.Contains(t, evidence, "quote")

	// check entity
	entities := raw["entities"].([]interface{})
	require.Len(t, entities, 1)
	entity := entities[0].(map[string]interface{})
	require.Contains(t, entity, "name")
	require.Contains(t, entity, "type")

	// Check round-trip
	var actual extraction.Output
	require.NoError(t, json.Unmarshal(b, &actual))
	require.Equal(t, expected, actual)
}

func TestNullableContractFieldsPreserveNull(t *testing.T) {
	out := extraction.Output{
		Statements: []extraction.Statement{
			{
				Source: extraction.Source{
					Name: nil,
					Type: extraction.SourceTypeNone,
					Mode: extraction.SourceModeUnattributed,
				},
				Evidence: extraction.Evidence{
					StartWith: nil,
					EndWith:   nil,
					Quote:     nil,
				},
			},
		},
	}

	b, err := json.Marshal(out)
	require.NoError(t, err)

	jsonStr := string(b)
	// it should literally contain `"name":null`, `"start_with":null`, etc.
	require.Contains(t, jsonStr, `"name":null`)
	require.Contains(t, jsonStr, `"start_with":null`)
	require.Contains(t, jsonStr, `"end_with":null`)
	require.Contains(t, jsonStr, `"quote":null`)
}

func TestEnumLiteralsAreStable(t *testing.T) {
	require.Equal(t, "factual", string(extraction.StatementTypeFactual))
	require.Equal(t, "opinion", string(extraction.StatementTypeOpinion))
	require.Equal(t, "mixed", string(extraction.StatementTypeMixed))

	require.Equal(t, "prediction", string(extraction.StatementTagPrediction))
	require.Equal(t, "evaluation", string(extraction.StatementTagEvaluation))
	require.Equal(t, "accusation", string(extraction.StatementTagAccusation))
	require.Equal(t, "proposal", string(extraction.StatementTagProposal))
	require.Equal(t, "promise", string(extraction.StatementTagPromise))

	require.Equal(t, "positive", string(extraction.SentimentPositive))
	require.Equal(t, "neutral", string(extraction.SentimentNeutral))
	require.Equal(t, "negative", string(extraction.SentimentNegative))

	require.Equal(t, "person", string(extraction.SourceTypePerson))
	require.Equal(t, "organization", string(extraction.SourceTypeOrganization))
	require.Equal(t, "collective", string(extraction.SourceTypeCollective))
	require.Equal(t, "anonymous", string(extraction.SourceTypeAnonymous))
	require.Equal(t, "document", string(extraction.SourceTypeDocument))
	require.Equal(t, "other", string(extraction.SourceTypeOther))
	require.Equal(t, "none", string(extraction.SourceTypeNone))

	require.Equal(t, "direct", string(extraction.SourceModeDirect))
	require.Equal(t, "indirect", string(extraction.SourceModeIndirect))
	require.Equal(t, "unattributed", string(extraction.SourceModeUnattributed))

	require.Equal(t, "primary", string(extraction.ImportancePrimary))
	require.Equal(t, "supporting", string(extraction.ImportanceSupporting))
	require.Equal(t, "mentioned", string(extraction.ImportanceMentioned))

	require.Equal(t, "person", string(extraction.EntityTypePerson))
	require.Equal(t, "public_sector", string(extraction.EntityTypePublicSector))
	require.Equal(t, "political_party", string(extraction.EntityTypePoliticalParty))
	require.Equal(t, "private_sector", string(extraction.EntityTypePrivateSector))
	require.Equal(t, "civil_society", string(extraction.EntityTypeCivilSociety))
	require.Equal(t, "media", string(extraction.EntityTypeMedia))
	require.Equal(t, "location", string(extraction.EntityTypeLocation))
	require.Equal(t, "facility", string(extraction.EntityTypeFacility))
	require.Equal(t, "policy", string(extraction.EntityTypePolicy))
	require.Equal(t, "event", string(extraction.EntityTypeEvent))
	require.Equal(t, "other", string(extraction.EntityTypeOther))
}
