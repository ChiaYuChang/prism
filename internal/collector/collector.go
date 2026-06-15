package collector

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ChiaYuChang/prism/pkg/utils"
	"github.com/go-playground/validator/v10"
)

// ErrInvalidArticle is returned when a parsed article does not meet the minimum requirements (Title and Content).
var ErrInvalidArticle = errors.New("invalid article")

var validate = validator.New()

// NormalizeArticle applies string normalization to Title, Content, and Author fields.
func NormalizeArticle(article *Article) *Article {
	if article == nil {
		return nil
	}
	article.Title = utils.NormalizeString(article.Title)
	article.Content = utils.NormalizeString(article.Content)
	article.Author = utils.NormalizeString(article.Author)

	return article
}

// ValidateArticle executes struct validation against validation tags on the Article.
func ValidateArticle(article *Article) error {
	if article == nil {
		return ErrInvalidArticle
	}
	if err := validate.Struct(article); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidArticle, err)
	}
	return nil
}

// Article is the structured output of the parser pipeline.
// It contains only fields that a parser can populate from raw HTML.
// DB-specific identifiers (source, fingerprint, type) are added by the
// storage layer when persisting to contents.
type Article struct {
	URL         string
	Title       string `validate:"required"`
	Content     string `validate:"required"`
	Author      string
	PublishedAt time.Time
	FetchedAt   time.Time
	Metadata    map[string]any
}

// Archive is a raw content record destined for object storage (S3/SeaweedFS).
// It holds the compressed, base64-encoded payload alongside audit metadata.
type Archive struct {
	Fingerprint string
	URL         string
	Payload     string // Gzip + Base64 encoded canonical string
	TraceID     string
	Timestamp   time.Time
	Metadata    map[string]any
}

// Collector defines a unified interface that composes all the core pipeline
// components: Fetching, Transforming, Parsing, and Saving.
type Collector interface {
	Fetcher
	Transformer
	Parser
	Saver
}

// Fetcher retrieves raw, unprocessed data (HTML/JSON) from a remote source.
// Both the HTTP fetcher (F) and the file recoverer (R) implement this interface.
type Fetcher interface {
	Fetch(ctx context.Context, url string) (string, error)
}

// Transformer applies a string → string transformation to content.
// This single interface covers both minification (strip noise, reduce size)
// and semantic reshaping (type-specific canonicalisation). Which role a
// particular implementation plays is determined by its position in a
// pipeline.Pipeline, not by its type — see docs/pipeline-wiring-design.md.
type Transformer interface {
	Transform(ctx context.Context, in string) (string, error)
}

// Saver persists an Archive record to object storage (SeaweedFS / S3).
// Used on two paths:
//   - success: stores minified content after Minify succeeds
//   - error:   stores raw content when Minify fails, for later replay
type Saver interface {
	Save(ctx context.Context, record Archive) error
}

// Parser extracts a structured Article from canonical HTML.
type Parser interface {
	Parse(ctx context.Context, url string, data string) (*Article, error)
}
