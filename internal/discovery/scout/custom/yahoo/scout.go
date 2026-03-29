package yahoo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	rootscout "github.com/ChiaYuChang/prism/internal/discovery/scout"
	htmlscout "github.com/ChiaYuChang/prism/internal/discovery/scout/html"
	"github.com/ChiaYuChang/prism/internal/model"
	"go.opentelemetry.io/otel/trace"
)

var streamItemsPattern = regexp.MustCompile(`(?s)"stream_items"\s*:\s*(\[.*?\])\s*,\s*"stream_total"`)

type Config struct {
	Name     string            `yaml:"name" json:"name"`
	Format   string            `yaml:"format" json:"format"`
	SpanName string            `yaml:"span_name" json:"span_name"`
	Headers  map[string]string `yaml:"headers" json:"headers"`
}

type yahooNewsItem struct {
	Title     string `json:"title"`
	Summary   string `json:"summary"`
	Publisher string `json:"publisher"`
	Timestamp int64  `json:"pubtime"`
	URL       string `json:"url"`
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
		return nil, rootscout.ErrNilLogger
	}
	if tracer == nil {
		return nil, rootscout.ErrNilTracer
	}

	loc, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		loc = time.FixedZone("Asia/Taipei", 8*60*60)
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
		loc:    loc,
		cfg:    cfg,
	}, nil
}

func (s *Scout) Discover(ctx context.Context, rawURL string) ([]model.Candidates, error) {
	ctx, span := s.tracer.Start(ctx, s.cfg.SpanName)
	defer span.End()

	body, err := htmlscout.Fetch(ctx, s.client, rawURL, s.cfg.Headers)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := body.Close(); err != nil {
			s.logger.ErrorContext(ctx, "failed to close response body", slog.String("url", rawURL), slog.String("error", err.Error()))
		}
	}()

	content, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	match := streamItemsPattern.FindSubmatch(content)
	if len(match) < 2 {
		return nil, fmt.Errorf("yahoo custom scout: stream_items not found")
	}

	var items []yahooNewsItem
	if err := json.Unmarshal(match[1], &items); err != nil {
		return nil, fmt.Errorf("parse yahoo stream items: %w", err)
	}

	out := make([]model.Candidates, 0, len(items))
	for _, item := range items {
		title := rootscout.NormalizeText(item.Title)
		link := strings.TrimSpace(item.URL)
		if title == "" || link == "" {
			continue
		}

		candidate := model.Candidates{
			URL:             link,
			Title:           title,
			Description:     rootscout.NormalizeText(item.Summary),
			IngestionMethod: "DIRECTORY",
			DiscoveredAt:    s.now().In(s.loc),
			Metadata: map[string]any{
				"scout":     s.cfg.Name,
				"format":    s.cfg.Format,
				"publisher": rootscout.NormalizeText(item.Publisher),
			},
		}

		if item.Timestamp > 0 {
			candidate.PublishedAt = time.UnixMilli(item.Timestamp).In(s.loc)
		}

		out = append(out, candidate)
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("%s custom scout: %w", s.cfg.Name, rootscout.ErrNoCandidatesFound)
	}

	s.logger.DebugContext(ctx, "yahoo custom scout discovered candidates",
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
	c.Headers = htmlscout.CloneHeaders(c.Headers)
	if len(c.Headers) == 0 {
		c.Headers = htmlscout.CloneHeaders(htmlscout.BrowserLikeHeaders)
	}
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
