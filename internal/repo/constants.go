package repo

type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "PENDING"
	TaskStatusRunning   TaskStatus = "RUNNING"
	TaskStatusFailed    TaskStatus = "FAILED"
	TaskStatusCompleted TaskStatus = "COMPLETED"
)

const (
	// Task Kinds
	TaskKindDirectoryFetch = "DIRECTORY_FETCH"
	TaskKindKeywordSearch  = "KEYWORD_SEARCH"
	TaskKindPageFetch      = "PAGE_FETCH"
	TaskKindEmbedCandidate = "EMBED_CANDIDATE"
	TaskKindEmbedContent   = "EMBED_CONTENT"

	// Source Types
	SourceTypeParty = "PARTY"
	SourceTypeMedia = "MEDIA"

	// Ingestion Methods
	IngestionMethodDirectory    = "DIRECTORY"
	IngestionMethodSearch       = "SEARCH"
	IngestionMethodSubscription = "SUBSCRIPTION"
	IngestionMethodManual       = "MANUAL"

	// Content Types
	ContentTypePartyRelease = "PARTY_RELEASE"
	ContentTypeArticle      = "ARTICLE"

	// Source Abbreviations (Commonly used)
	SourceAbbrDPP   = "dpp"
	SourceAbbrKMT   = "kmt"
	SourceAbbrTPP   = "tpp"
	SourceAbbrYahoo = "yahoo"
)

const (
	EmbeddingCategoryTitle = "TITLE"
	EmbeddingCategoryBrief = "BRIEF"
)
