package model

// ExtractionInput represents the input for the extraction process.
type ExtractionInput struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// ExtractionOutput represents the structured insights from LLM analysis.
type ExtractionOutput struct {
	// Title is a neutral, audit-friendly title summarizing the article.
	Title string `json:"title"`
	// Entities contains key people, parties, or organizations with normalized and source forms.
	Entities []ExtractionEntity `json:"entities"`
	// Topics contains central issues (e.g., "Parliamentary Reform", "Nuclear Power").
	Topics []string `json:"topics"`
	// Phrases contains composite search strings optimized for search engines.
	Phrases []string `json:"phrases"`
	// Summary provides a concise multi-sentence overview for auditability.
	Summary string `json:"summary"`
}

// ExtractionEntity represents a normalized named entity extracted from content.
type ExtractionEntity struct {
	// Canonical is the normalized form of the entity.
	Canonical string `json:"canonical"`
	// Surface is the form of the entity as it appears in the content.
	Surface string `json:"surface"`
	// Type is the type of the entity. (e.g. "person", "party", "government_agency", "legislative_body")
	Type string `json:"type"`
}
