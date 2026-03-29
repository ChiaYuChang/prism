package htmlscout

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	neturl "net/url"
	"strings"
	"time"

	rootscout "github.com/ChiaYuChang/prism/internal/discovery/scout"
	"github.com/ChiaYuChang/prism/internal/model"
	"github.com/PuerkitoBio/goquery"
	"go.opentelemetry.io/otel/trace"
)

type RuleConfig struct {
	ItemSelector        string
	LinkSelector        string
	LinkAttr            string
	TitleSelector       string
	DateSelector        string
	DateLayout          string
	DescriptionSelector string
}

type Config struct {
	Name     string
	Format   string
	SpanName string
	Rules    []RuleConfig
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

	cfg = cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	s := &Scout{
		logger: logger,
		tracer: tracer,
		client: client,
		now:    time.Now,
		loc:    time.Local,
		cfg:    cfg,
	}
	return s, nil
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
			s.logger.ErrorContext(
				ctx,
				"fail to close response body",
				slog.String("url", rawURL),
				slog.String("scout", s.cfg.Name),
				slog.String("span_name", s.cfg.SpanName),
				slog.String("error", err.Error()),
			)
		}
	}()

	doc, err := goquery.NewDocumentFromReader(body)
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}

	out := make([]model.Candidates, 0)
	seen := make(map[string]struct{})
	for _, rule := range s.cfg.Rules {
		doc.Find(rule.ItemSelector).Each(func(_ int, item *goquery.Selection) {
			candidate, ok := s.extractCandidate(item, rawURL, rule)
			if !ok {
				return
			}
			if _, exists := seen[candidate.URL]; exists {
				return
			}
			seen[candidate.URL] = struct{}{}
			out = append(out, candidate)
		})
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("%s html scout: %w", s.cfg.Name, rootscout.ErrNoCandidatesFound)
	}

	s.logger.DebugContext(ctx, "html scout discovered candidates",
		slog.String("url", rawURL),
		slog.String("scout", s.cfg.Name),
		slog.String("span_name", s.cfg.SpanName),
		slog.Int("count", len(out)),
	)
	return out, nil
}

func (s *Scout) extractCandidate(item *goquery.Selection, rawURL string, rule RuleConfig) (model.Candidates, bool) {
	href := item.Find(rule.LinkSelector).First().AttrOr(rule.LinkAttr, "")
	if rule.LinkSelector == "" {
		href = item.AttrOr(rule.LinkAttr, "")
	}
	if href == "" {
		return model.Candidates{}, false
	}

	title := rootscout.NormalizeText(item.Find(rule.TitleSelector).First().Text())
	if rule.TitleSelector == "" {
		title = rootscout.NormalizeText(item.Text())
	}
	if title == "" {
		if rule.LinkSelector == "" {
			title = rootscout.NormalizeText(item.Text())
		} else {
			title = rootscout.NormalizeText(item.Find(rule.LinkSelector).First().Text())
		}
	}
	if title == "" {
		return model.Candidates{}, false
	}

	candidate := model.Candidates{
		URL:             resolveURL(rawURL, href),
		Title:           title,
		IngestionMethod: "DIRECTORY",
		DiscoveredAt:    s.now().In(s.loc),
		Metadata: map[string]any{
			"scout":  s.cfg.Name,
			"format": s.cfg.Format,
		},
	}

	if desc := textsOf(item, rule.DescriptionSelector); desc != "" {
		candidate.Description = desc
	}

	if dateText := textOf(item, rule.DateSelector); dateText != "" && rule.DateLayout != "" {
		if publishedAt, err := time.ParseInLocation(rule.DateLayout, dateText, s.loc); err == nil {
			candidate.PublishedAt = publishedAt
		}
	}

	return candidate, true
}

func textOf(item *goquery.Selection, selector string) string {
	if selector == "" {
		return ""
	}
	return rootscout.NormalizeText(item.Find(selector).First().Text())
}

func textsOf(item *goquery.Selection, selector string) string {
	if selector == "" {
		return ""
	}

	out := make([]string, 0)
	item.Find(selector).Each(func(_ int, sel *goquery.Selection) {
		text := rootscout.NormalizeText(sel.Text())
		if text != "" {
			out = append(out, text)
		}
	})
	return strings.Join(out, "\n")
}

func resolveURL(baseURL, href string) string {
	base, err := neturl.Parse(baseURL)
	if err != nil {
		return href
	}
	ref, err := neturl.Parse(strings.TrimSpace(href))
	if err != nil {
		return href
	}
	return base.ResolveReference(ref).String()
}

func (c Config) Normalize() Config {
	c.Name = strings.TrimSpace(c.Name)
	c.Format = strings.TrimSpace(c.Format)
	c.SpanName = strings.TrimSpace(c.SpanName)

	for i, rule := range c.Rules {
		c.Rules[i] = rule.Normalize()
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
	if len(c.Rules) == 0 {
		return fmt.Errorf("%w: %s", rootscout.ErrConfigFieldEmpty, "rules")
	}

	for i, rule := range c.Rules {
		if err := rule.Validate(i); err != nil {
			return err
		}
	}

	return nil
}

func (r RuleConfig) Normalize() RuleConfig {
	r.ItemSelector = strings.TrimSpace(r.ItemSelector)
	r.LinkSelector = strings.TrimSpace(r.LinkSelector)
	r.LinkAttr = strings.TrimSpace(r.LinkAttr)
	r.TitleSelector = strings.TrimSpace(r.TitleSelector)
	r.DateSelector = strings.TrimSpace(r.DateSelector)
	r.DateLayout = strings.TrimSpace(r.DateLayout)
	r.DescriptionSelector = strings.TrimSpace(r.DescriptionSelector)
	if r.LinkAttr == "" {
		r.LinkAttr = "href"
	}
	return r
}

func (r RuleConfig) Validate(i int) error {
	if r.ItemSelector == "" {
		return fmt.Errorf("%w: rules[%d].item_selector", rootscout.ErrConfigFieldEmpty, i)
	}
	if r.LinkAttr == "" {
		return fmt.Errorf("%w: rules[%d].link_attr", rootscout.ErrConfigFieldEmpty, i)
	}
	if r.LinkSelector == "" && r.LinkAttr != "href" {
		return fmt.Errorf("%w: rules[%d].link_selector", rootscout.ErrConfigFieldEmpty, i)
	}
	if r.TitleSelector == "" && r.LinkSelector == "" {
		return fmt.Errorf("%w: rules[%d].title_selector", rootscout.ErrConfigFieldEmpty, i)
	}
	if r.DateSelector != "" && r.DateLayout == "" {
		return fmt.Errorf("%w: rules[%d].date_layout", rootscout.ErrConfigFieldEmpty, i)
	}
	return nil
}
