package discovery

import (
	"context"
	"fmt"
	"time"

	"github.com/ChiaYuChang/prism/internal/model"
	"github.com/google/uuid"
)

const ScoutDiscoverSpanNamePattern = "discovery.scout.%s.%s.discover"

func ScoutDiscoverSpanName(format, name string) string {
	return fmt.Sprintf(ScoutDiscoverSpanNamePattern, format, name)
}

// Scout is responsible for lightweight, recall-oriented discovery of candidate
// article briefs or party press releases from external sources (e.g., directory
// pages, RSS/Atom feeds, search APIs). Following the system's normalization-first
// workflow, a Scout parses these sources into briefs, which are stored as candidates
// before being promoted to full contents.
type Scout interface {
	// Discover executes a search or crawls a page, returning initial candidate
	Discover(ctx context.Context, url string) (out []model.Candidates, err error)
}

// BackfillRequest describes one historical backfill run.
type BackfillRequest struct {
	// StartURL is the URL to start crawling from.
	StartURL string
	// BatchID is the ID of the batch to group ingestion.
	BatchID uuid.UUID
	// Until is the lower-bound date for replaying older listing pages.
	Until time.Time
	// MaxPages is the maximum number of pages to crawl.
	MaxPages int
}

// BackfillResult summarizes one historical backfill run, tracking progress and the
// number of candidate briefs that were successfully pushed to candidates storage.
type BackfillResult struct {
	PagesVisited        int       `json:"pages_visited,omitempty"`
	CandidatesSeen      int       `json:"candidates_seen,omitempty"`
	CandidatesProcessed int       `json:"candidates_processed,omitempty"`
	OldestPublishedAt   time.Time `json:"oldest_published_at,omitempty"`
}

// Backfiller coordinates the execution of historical backfill runs. It iterates
// through older listing pages until the requested lower-bound date is reached,
// utilizing a Scout to discover candidates on each page and pushing them into
// a CandidateSink.
type Backfiller interface {
	// Run executes the backfill process for a particular backfill request.
	Run(ctx context.Context, req BackfillRequest) (BackfillResult, error)
}

// Extractor is responsible for generating short, recall-oriented keyword groups
// from political party press releases. The extracted composite search phrases are
// intended to be used downstream by Planner to create MEDIA + KEYWORD_SEARCH tasks.
type Extractor interface {
	// Extract analyzes the seed content and returns structured keyword insights.
	// Bounded, high-signal seed inputs are processed to produce sets of keywords.
	Extract(ctx context.Context, in *model.ExtractionInput) (out *model.ExtractionOutput, err error)
}

// PlannerTarget describes one downstream MEDIA discovery endpoint that should
// receive generated keyword-based tasks.
type PlannerTarget struct {
	SourceID int32
	URL      string
	Site     string
}

// PlannerRequest describes one planning run over a completed PARTY batch.
type PlannerRequest struct {
	BatchID   uuid.UUID
	TraceID   string
	Targets   []PlannerTarget
	Frequency *time.Duration
	NextRunAt *time.Time
	ExpiresAt *time.Time
}

// PlannerResult summarizes one planning run.
type PlannerResult struct {
	// Number of PARTY contents used for extraction.
	SeedContents int
	// Number of extractions performed, should be equal to SeedContents if no error occurs.
	Extractions int
	// Number of unique keyword phrases generated.
	UniquePhrases int
	// Number of MEDIA tasks created.
	TasksCreated int
}

// Planner is responsible for turning completed PARTY seed contents into
// follow-up MEDIA discovery tasks.
type Planner interface {
	Plan(ctx context.Context, req PlannerRequest) (PlannerResult, error)
}

// SearchClient is responsible for communicating with external search engines
// (e.g., Google or Brave) to discover news coverage. It supports the media search
// step in the discovery loop, resolving short keyword groups into candidate briefs.
type SearchClient interface {
	// DiscoverNews executes a media API search and returns initial candidate briefs.
	DiscoverNews(ctx context.Context, query string, site string) ([]model.Candidates, error)
}
