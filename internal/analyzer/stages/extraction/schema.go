package extraction

import (
	"github.com/ChiaYuChang/prism/pkg/schema"
)

// Schema returns the configured JSON Schema for Stage 1 extraction output.
func Schema() schema.JSONSchema {
	s := schema.NewSkeleton[Output](
		"article_extraction_result",
		1,
	)

	s.Title = "Article Extraction Result"
	s.Description = "Structured article-local semantic extraction of one news article."

	// Top-level requirements
	s.Required = []string{"summary", "statements", "entities"}
	s.PropertyOrder = []string{"summary", "statements", "entities"}

	summarySchema := s.Properties["summary"]
	summarySchema.MinLength = ptr(1)

	statementsSchema := s.Properties["statements"]
	statementsSchema.MinItems = ptr(1)

	// Statement schema
	statementSchema := statementsSchema.Items
	statementSchema.Required = []string{
		"statement",
		"type",
		"tags",
		"sentiment",
		"source",
		"importance",
		"evidence",
	}
	statementSchema.PropertyOrder = []string{
		"statement",
		"type",
		"tags",
		"sentiment",
		"source",
		"importance",
		"evidence",
	}

	statementTextSchema := statementSchema.Properties["statement"]
	statementTextSchema.MinLength = ptr(1)

	statementTypeSchema := statementSchema.Properties["type"]
	statementTypeSchema.Enum = []any{
		string(StatementTypeFactual),
		string(StatementTypeOpinion),
		string(StatementTypeMixed),
	}
	statementTypeSchema.Description = "factual = evidence-checkable in principle; not necessarily verified true. mixed = intertwined factual and subjective content, not model uncertainty."

	statementTagsSchema := statementSchema.Properties["tags"]
	statementTagsSchema.Items.Enum = []any{
		string(StatementTagPrediction),
		string(StatementTagEvaluation),
		string(StatementTagAccusation),
		string(StatementTagProposal),
		string(StatementTagPromise),
	}

	statementSentimentSchema := statementSchema.Properties["sentiment"]
	statementSentimentSchema.Enum = []any{
		string(SentimentPositive),
		string(SentimentNeutral),
		string(SentimentNegative),
	}

	statementImportanceSchema := statementSchema.Properties["importance"]
	statementImportanceSchema.Enum = []any{
		string(ImportancePrimary),
		string(ImportanceSupporting),
		string(ImportanceMentioned),
	}

	// Source schema
	sourceSchema := statementSchema.Properties["source"]
	sourceSchema.Required = []string{"name", "type", "mode"}
	sourceSchema.PropertyOrder = []string{"name", "type", "mode"}

	sourceTypeSchema := sourceSchema.Properties["type"]
	sourceTypeSchema.Enum = []any{
		string(SourceTypePerson),
		string(SourceTypeOrganization),
		string(SourceTypeCollective),
		string(SourceTypeAnonymous),
		string(SourceTypeDocument),
		string(SourceTypeOther),
		string(SourceTypeNone),
	}

	sourceModeSchema := sourceSchema.Properties["mode"]
	sourceModeSchema.Enum = []any{
		string(SourceModeDirect),
		string(SourceModeIndirect),
		string(SourceModeUnattributed),
	}

	// Evidence schema
	evidenceSchema := statementSchema.Properties["evidence"]
	evidenceSchema.Required = []string{"summary", "start_with", "end_with", "quote"}
	evidenceSchema.PropertyOrder = []string{"summary", "start_with", "end_with", "quote"}

	evidenceSummarySchema := evidenceSchema.Properties["summary"]
	evidenceSummarySchema.MinLength = ptr(1)

	evidenceStartWithSchema := evidenceSchema.Properties["start_with"]
	evidenceStartWithSchema.Description = "verbatim source text"
	
	evidenceEndWithSchema := evidenceSchema.Properties["end_with"]
	evidenceEndWithSchema.Description = "verbatim source text"
	
	evidenceQuoteSchema := evidenceSchema.Properties["quote"]
	evidenceQuoteSchema.Description = "verbatim source text"

	// Entity schema
	entitiesSchema := s.Properties["entities"]
	entitySchema := entitiesSchema.Items
	entitySchema.Required = []string{"name", "type"}
	entitySchema.PropertyOrder = []string{"name", "type"}

	entityNameSchema := entitySchema.Properties["name"]
	entityNameSchema.MinLength = ptr(1)
	entityNameSchema.Description = "Preserve the surface form used in this article; do not canonicalize aliases."

	entityTypeSchema := entitySchema.Properties["type"]
	entityTypeSchema.Enum = []any{
		string(EntityTypePerson),
		string(EntityTypePublicSector),
		string(EntityTypePoliticalParty),
		string(EntityTypePrivateSector),
		string(EntityTypeCivilSociety),
		string(EntityTypeMedia),
		string(EntityTypeLocation),
		string(EntityTypeFacility),
		string(EntityTypePolicy),
		string(EntityTypeEvent),
		string(EntityTypeOther),
	}

	return s
}

func ptr[T any](v T) *T {
	return &v
}
