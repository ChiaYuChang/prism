package extractor

import (
	"github.com/ChiaYuChang/prism/internal/model"
	pkgschema "github.com/ChiaYuChang/prism/pkg/schema"
	"github.com/ChiaYuChang/prism/pkg/utils"
	"github.com/google/jsonschema-go/jsonschema"
)

type ExtractionEntity string

const (
	ExtractionEntityPerson            ExtractionEntity = "person"
	ExtractionEntityParty             ExtractionEntity = "party"
	ExtractionEntityGovernmentAgency  ExtractionEntity = "government_agency"
	ExtractionEntityLegislativeBody   ExtractionEntity = "legislative_body"
	ExtractionEntityJudicialBody      ExtractionEntity = "judicial_body"
	ExtractionEntityMilitary          ExtractionEntity = "military"
	ExtractionEntityForeignGovernment ExtractionEntity = "foreign_government"
	ExtractionEntityOrganization      ExtractionEntity = "organization"
	ExtractionEntityMedia             ExtractionEntity = "media"
	ExtractionEntityCivicGroup        ExtractionEntity = "civic_group"
	ExtractionEntityLocation          ExtractionEntity = "location"
	ExtractionEntityOther             ExtractionEntity = "other"
)

var ExtractionResultJSONSchema = func() pkgschema.JSONSchema {
	s := pkgschema.NewSkeleton[model.ExtractionOutput]("extraction_result", 1)
	s.Title = "Extraction Result"
	s.Description = "Structured political news extraction result for downstream discovery jobs."
	s.Required = []string{"title", "entities", "topics", "phrases", "summary"}

	s.Properties["title"].Description = "Neutral, non-sensational news title that preserves the article's core meaning."
	s.Properties["title"].MinLength = utils.Ptr(1)

	s.Properties["entities"].Description = "Normalized key entities relevant to the article."
	s.Properties["entities"].MinItems = utils.Ptr(1)
	s.Properties["entities"].Items = &jsonschema.Schema{
		Type:        "object",
		Description: "Named entity with normalized storage form and source wording.",
		Required:    []string{"canonical", "surface", "type"},
		Properties: map[string]*jsonschema.Schema{
			"canonical": {
				Type:        "string",
				Description: "Normalized storage form, preferably Taiwan-common Traditional Chinese wording when available.",
				MinLength:   utils.Ptr(1),
			},
			"surface": {
				Type:        "string",
				Description: "Observed wording from the source text or the closest directly supported form.",
				MinLength:   utils.Ptr(1),
			},
			"type": {
				Type:        "string",
				Description: "Entity type from the allowed type list.",
				Enum: []any{
					ExtractionEntityPerson,
					ExtractionEntityParty,
					ExtractionEntityGovernmentAgency,
					ExtractionEntityLegislativeBody,
					ExtractionEntityJudicialBody,
					ExtractionEntityMilitary,
					ExtractionEntityForeignGovernment,
					ExtractionEntityOrganization,
					ExtractionEntityMedia,
					ExtractionEntityCivicGroup,
					ExtractionEntityLocation,
					ExtractionEntityOther,
				},
			},
		},
		PropertyOrder: []string{"canonical", "surface", "type"},
	}

	s.Properties["topics"].Description = "Core political or public-affairs topics discussed in the article."
	s.Properties["topics"].MinItems = utils.Ptr(1)
	s.Properties["topics"].UniqueItems = true
	s.Properties["topics"].Items = &jsonschema.Schema{
		Type:        "string",
		Description: "Concrete issue label using precise, search-friendly wording.",
		MinLength:   utils.Ptr(1),
	}

	s.Properties["phrases"].Description = "Composite search phrases optimized for related article discovery."
	s.Properties["phrases"].MinItems = utils.Ptr(1)
	s.Properties["phrases"].UniqueItems = true
	s.Properties["phrases"].Items = &jsonschema.Schema{
		Type:        "string",
		Description: "Specific web or news search phrase that combines key entities and topics.",
		MinLength:   utils.Ptr(1),
	}

	s.Properties["summary"].Description = "Neutral 2 to 3 sentence summary that can stand on its own."
	s.Properties["summary"].MinLength = utils.Ptr(1)
	return s
}()
