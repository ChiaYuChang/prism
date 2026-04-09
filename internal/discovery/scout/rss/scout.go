package rssscout

import (
	"context"
	"encoding/xml"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	rootscout "github.com/ChiaYuChang/prism/internal/discovery/scout"
	"github.com/ChiaYuChang/prism/internal/model"
	"go.opentelemetry.io/otel/trace"
)

type Config struct {
	Name     string `yaml:"name" json:"name"`
	Format   string `yaml:"format" json:"format"`
	SpanName string `yaml:"span_name" json:"span_name"`
}

type rssFeed struct {
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Items []rssItem `xml:"item"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
}

type Scout struct {
	logger *slog.Logger
	tracer trace.Tracer
	client *http.Client
	now    func() time.Time
	loc    *time.Location
	cfg    Config
}

func New(logger *slog.Logger, tracer trace.Tracer, client *http.Client, cfg Config) (*Scout, error) {
	if logger == nil {
		return nil, fmt.Errorf("%w: logger", rootscout.ErrParamMissing)
	}
	if tracer == nil {
		return nil, fmt.Errorf("%w: tracer", rootscout.ErrParamMissing)
	}

	cfg = cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &Scout{
		logger: logger,
		tracer: tracer,
		client: client,
		now:    time.Now,
		loc:    time.Local,
		cfg:    cfg,
	}, nil
}

func (s *Scout) Discover(ctx context.Context, rawURL string) ([]model.Candidates, error) {
	ctx, span := s.tracer.Start(ctx, s.cfg.SpanName)
	defer span.End()

	body, err := rootscout.Fetch(ctx, s.client, rawURL)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := body.Close(); err != nil {
			s.logger.ErrorContext(ctx, "failed to close response body", slog.String("url", rawURL), slog.String("error", err.Error()))
		}
	}()

	var parsed rssFeed
	if err := xml.NewDecoder(body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("parse rss feed: %w", err)
	}

	out := make([]model.Candidates, 0, len(parsed.Channel.Items))
	for _, item := range parsed.Channel.Items {
		title := rootscout.NormalizeText(item.Title)
		link := strings.TrimSpace(item.Link)
		if title == "" || link == "" {
			continue
		}

		candidate := model.Candidates{
			URL:             link,
			Title:           title,
			Description:     rootscout.NormalizeText(item.Description),
			IngestionMethod: "DIRECTORY",
			DiscoveredAt:    s.now().In(s.loc),
			Metadata: map[string]any{
				"scout":  s.cfg.Name,
				"format": s.cfg.Format,
			},
		}

		if publishedAt, ok := parsePubDate(item.PubDate, s.loc); ok {
			candidate.PublishedAt = publishedAt
		}

		out = append(out, candidate)
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("%s rss scout: %w", s.cfg.Name, rootscout.ErrNoCandidatesFound)
	}

	s.logger.DebugContext(ctx, "rss scout discovered candidates",
		slog.String("url", rawURL),
		slog.String("scout", s.cfg.Name),
		slog.String("span_name", s.cfg.SpanName),
		slog.Int("count", len(out)),
	)
	return out, nil
}

func (c Config) Normalize() Config {
	c.Name = strings.TrimSpace(c.Name)
	c.Format = strings.TrimSpace(c.Format)
	c.SpanName = strings.TrimSpace(c.SpanName)
	return c
}

func (c Config) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("%w: %s", rootscout.ErrConfigFieldEmpty, "name")
	}
	if c.Format == "" {
		return fmt.Errorf("%w: %s", rootscout.ErrConfigFieldEmpty, "format")
	}
	if c.SpanName == "" {
		return fmt.Errorf("%w: %s", rootscout.ErrConfigFieldEmpty, "span_name")
	}
	return nil
}

func parsePubDate(raw string, loc *time.Location) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}

	layouts := []string{
		time.RFC1123Z,
		time.RFC1123,
		time.RFC822Z,
		time.RFC822,
		time.RFC3339,
	}

	for _, layout := range layouts {
		t, err := time.Parse(layout, raw)
		if err == nil {
			if loc != nil {
				return t.In(loc), true
			}
			return t, true
		}
	}

	return time.Time{}, false
}
