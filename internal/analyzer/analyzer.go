package analyzer

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrParamMissing         = errors.New("param missing")
	ErrNilInput             = errors.New("input is nil")
	ErrInvalidScore         = errors.New("invalid stance score")
	ErrFailedToDecodeOutput = errors.New("failed to decode analyzer output")
)

// IssueType defines the importance tier of a controversial issue.
type IssueType string

const (
	IssueTypeMajor IssueType = "major"
	IssueTypeMinor IssueType = "minor"
)

// ArticleInput represents a single news article or text snippet input for analysis.
type ArticleInput struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

// Issue represents a single controversial policy/viewpoint issue extracted from coverage.
type Issue struct {
	ID          string    `json:"id,omitempty"`
	Type        IssueType `json:"type"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	ProCriteria []string  `json:"pro_criteria"`
	ConCriteria []string  `json:"con_criteria"`
}

// TopicExtractionInput represents the payload containing multiple articles for topic-level extraction.
type TopicExtractionInput struct {
	Articles []ArticleInput `json:"articles"`
}

// TopicExtractionOutput represents the structured output of topic-level extraction.
type TopicExtractionOutput struct {
	TopicName    string   `json:"topic_name"`
	Facts        []string `json:"facts"`
	CommonGround []string `json:"common_ground"`
	Issues       []Issue  `json:"issues"`
}

// StanceScore represents the evaluated stance score and evidence for a single issue.
type StanceScore struct {
	IssueID    string   `json:"issue_id"`
	Score      int      `json:"score"`
	Reasoning  string   `json:"reasoning"`
	References []string `json:"references"`
}

// StanceScoringInput represents the input for evaluating an article's stance on defined issues.
type StanceScoringInput struct {
	TopicName string       `json:"topic_name"`
	Issues    []Issue      `json:"issues"`
	Article   ArticleInput `json:"article"`
}

// StanceScoringOutput represents the structured output of stance scoring.
type StanceScoringOutput struct {
	Scores []StanceScore `json:"scores"`
}

// TopicExtractor extracts topic-level structure, facts, common ground, and issues from article collections.
type TopicExtractor interface {
	ExtractTopic(ctx context.Context, in *TopicExtractionInput) (*TopicExtractionOutput, error)
}

// StanceScorer evaluates an article's stance toward a target topic and its defined issues.
type StanceScorer interface {
	ScoreStance(ctx context.Context, in *StanceScoringInput) (*StanceScoringOutput, error)
}

// ArticleEmbedder generates vector embeddings for article text using an LLM Embedder.
type ArticleEmbedder interface {
	EmbedArticle(ctx context.Context, title string, content string) ([]float32, error)
}

// Analyzer composes TopicExtractor, StanceScorer, and ArticleEmbedder into a unified analysis interface.
type Analyzer interface {
	TopicExtractor
	StanceScorer
	ArticleEmbedder
}

// AssignIssueIDs populates deterministic, non-duplicated IDs ("major_1", "major_2", "minor_1", etc.)
// for extracted issues.
func AssignIssueIDs(issues []Issue) []Issue {
	if len(issues) == 0 {
		return issues
	}
	out := make([]Issue, len(issues))
	majorCount := 0
	minorCount := 0
	for i, issue := range issues {
		item := issue
		if strings.EqualFold(string(item.Type), string(IssueTypeMajor)) {
			majorCount++
			item.ID = fmt.Sprintf("major_%d", majorCount)
		} else {
			minorCount++
			item.ID = fmt.Sprintf("minor_%d", minorCount)
		}
		out[i] = item
	}
	return out
}

// ValidateStanceScore ensures stance score integers are within the 1..5 Likert range.
func ValidateStanceScore(score int) error {
	if score < 1 || score > 5 {
		return fmt.Errorf("%w: score %d must be between 1 and 5", ErrInvalidScore, score)
	}
	return nil
}
