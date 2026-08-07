package extraction

// Input represents the immutable article input required for Stage 1.
type Input struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

// Output represents the structured semantic result from Stage 1.
type Output struct {
	Summary    string      `json:"summary"`
	Statements []Statement `json:"statements"`
	Entities   []Entity    `json:"entities"`
}

// Statement represents a single extracted statement.
type Statement struct {
	Statement  string         `json:"statement"`
	Type       StatementType  `json:"type"`
	Tags       []StatementTag `json:"tags"`
	Sentiment  Sentiment      `json:"sentiment"`
	Source     Source         `json:"source"`
	Importance Importance     `json:"importance"`
	Evidence   Evidence       `json:"evidence"`
}

// Source describes who or what the statement is attributed to.
type Source struct {
	Name *string    `json:"name"`
	Type SourceType `json:"type"`
	Mode SourceMode `json:"mode"`
}

// Evidence locates the exact source text supporting the statement.
type Evidence struct {
	Summary   string  `json:"summary"`
	StartWith *string `json:"start_with"`
	EndWith   *string `json:"end_with"`
	Quote     *string `json:"quote"`
}

// Entity represents an entity mentioned in the article.
type Entity struct {
	Name string     `json:"name"`
	Type EntityType `json:"type"`
}

type StatementType string

const (
	StatementTypeFactual StatementType = "factual"
	StatementTypeOpinion StatementType = "opinion"
	StatementTypeMixed   StatementType = "mixed"
)

type StatementTag string

const (
	StatementTagPrediction StatementTag = "prediction"
	StatementTagEvaluation StatementTag = "evaluation"
	StatementTagAccusation StatementTag = "accusation"
	StatementTagProposal   StatementTag = "proposal"
	StatementTagPromise    StatementTag = "promise"
)

type Sentiment string

const (
	SentimentPositive Sentiment = "positive"
	SentimentNeutral  Sentiment = "neutral"
	SentimentNegative Sentiment = "negative"
)

type SourceType string

const (
	SourceTypePerson       SourceType = "person"
	SourceTypeOrganization SourceType = "organization"
	SourceTypeCollective   SourceType = "collective"
	SourceTypeAnonymous    SourceType = "anonymous"
	SourceTypeDocument     SourceType = "document"
	SourceTypeOther        SourceType = "other"
	SourceTypeNone         SourceType = "none"
)

type SourceMode string

const (
	SourceModeDirect       SourceMode = "direct"
	SourceModeIndirect     SourceMode = "indirect"
	SourceModeUnattributed SourceMode = "unattributed"
)

type Importance string

const (
	ImportancePrimary    Importance = "primary"
	ImportanceSupporting Importance = "supporting"
	ImportanceMentioned  Importance = "mentioned"
)

type EntityType string

const (
	EntityTypePerson         EntityType = "person"
	EntityTypePublicSector   EntityType = "public_sector"
	EntityTypePoliticalParty EntityType = "political_party"
	EntityTypePrivateSector  EntityType = "private_sector"
	EntityTypeCivilSociety   EntityType = "civil_society"
	EntityTypeMedia          EntityType = "media"
	EntityTypeLocation       EntityType = "location"
	EntityTypeFacility       EntityType = "facility"
	EntityTypePolicy         EntityType = "policy"
	EntityTypeEvent          EntityType = "event"
	EntityTypeOther          EntityType = "other"
)
