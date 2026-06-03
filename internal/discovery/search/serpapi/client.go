package serpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ChiaYuChang/prism/internal/discovery"
	"github.com/ChiaYuChang/prism/internal/model"
)

var _ discovery.SearchClient = (*Client)(nil)

// Options controls SerpAPI Google News Search requests.
type Options struct {
	Engine     string
	Country    string
	Language   string
	Region     string
	Safe       string
	DateFilter string
	Start      int
	MaxResults int
	SortOrder  int
	Filter     string
	NoCache    *bool
}

// DefaultOptions returns Taiwan-oriented SerpAPI defaults.
func DefaultOptions() Options {
	return Options{Engine: "google_news", Country: "tw", Language: "zh-tw"}
}

// Client calls SerpAPI's google_news engine.
type Client struct {
	httpClient *http.Client
	apiKey     string
	baseURL    string
	opts       Options
}

// NewClient creates a SerpAPI Google News client using the public endpoint.
func NewClient(httpClient *http.Client, apiKey string, opts Options) *Client {
	return NewClientWithURL(httpClient, apiKey, opts, searchURL)
}

// NewClientWithURL creates a SerpAPI client with an overrideable endpoint.
func NewClientWithURL(httpClient *http.Client, apiKey string, opts Options, baseURL string) *Client {
	if opts.Engine == "" {
		opts.Engine = "google_news"
	}
	if opts.Country == "" {
		opts.Country = "tw"
	}
	if opts.Language == "" {
		opts.Language = "zh-tw"
	}
	return &Client{httpClient: httpClient, apiKey: apiKey, baseURL: baseURL, opts: opts}
}

const searchURL = "https://serpapi.com/search.json"

type searchResponse struct {
	NewsResults []newsResult `json:"news_results"`
}

type newsResult struct {
	Position       int          `json:"position"`
	Title          string       `json:"title"`
	Link           string       `json:"link"`
	Snippet        string       `json:"snippet"`
	Thumbnail      string       `json:"thumbnail"`
	ThumbnailSmall string       `json:"thumbnail_small"`
	Date           string       `json:"date"`
	ISODate        string       `json:"iso_date"`
	Source         source       `json:"source"`
	Stories        []newsResult `json:"stories"`
}

type source struct {
	Name    string   `json:"name"`
	Icon    string   `json:"icon"`
	Authors []string `json:"authors"`
}

func (s *source) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err == nil {
		s.Name = name
		return nil
	}
	type sourceAlias source
	var out sourceAlias
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	*s = source(out)
	return nil
}

// DiscoverNews executes a Google News query via SerpAPI.
func (c *Client) DiscoverNews(ctx context.Context, query string, site string) ([]model.Candidates, error) {
	q := query
	if site != "" {
		q = query + " site:" + site
	}

	u, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse serpapi url: %w", err)
	}
	params := u.Query()
	params.Set("engine", c.opts.Engine)
	params.Set("api_key", c.apiKey)
	params.Set("q", q)
	params.Set("output", "json")
	c.applyEngineParams(params)
	if c.opts.NoCache != nil {
		params.Set("no_cache", fmt.Sprintf("%t", *c.opts.NoCache))
	}
	u.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("serpapi request: %s", redactSecretValues(err.Error()))
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("serpapi: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var sr searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return nil, fmt.Errorf("decode serpapi response: %w", err)
	}

	now := time.Now()
	candidates := make([]model.Candidates, 0, len(sr.NewsResults))
	for _, result := range flattenResults(sr.NewsResults) {
		if result.Link == "" || result.Title == "" {
			continue
		}
		meta := map[string]any{
			"search_provider": "serpapi",
			"query":           query,
			"rank":            result.Position,
		}
		if site != "" {
			meta["site_filter"] = site
		}
		if result.Source.Name != "" {
			meta["source_name"] = result.Source.Name
		}
		if result.Source.Icon != "" {
			meta["source_icon"] = result.Source.Icon
		}
		if len(result.Source.Authors) > 0 {
			meta["authors"] = result.Source.Authors
		}
		if result.Thumbnail != "" {
			meta["thumbnail"] = result.Thumbnail
		}
		if result.ThumbnailSmall != "" {
			meta["thumbnail_small"] = result.ThumbnailSmall
		}
		if result.Date != "" {
			meta["date"] = result.Date
		}
		if result.ISODate != "" {
			meta["iso_date"] = result.ISODate
		}

		candidates = append(candidates, model.Candidates{
			URL:          result.Link,
			Title:        result.Title,
			Description:  strings.TrimSpace(result.Snippet),
			PublishedAt:  parseISODateOrZero(result.ISODate),
			DiscoveredAt: now,
			Metadata:     meta,
		})
	}

	return candidates, nil
}

func flattenResults(results []newsResult) []newsResult {
	out := make([]newsResult, 0, len(results))
	for _, result := range results {
		out = append(out, result)
		out = append(out, flattenResults(result.Stories)...)
	}
	return out
}

func parseISODateOrZero(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return t
}

func setQueryParam(values url.Values, key, value string) {
	if value != "" {
		values.Set(key, value)
	}
}

func redactSecretValues(raw string) string {
	for _, key := range []string{"api_key", "apikey", "key", "access_token", "token"} {
		raw = redactQueryValue(raw, key)
	}
	return raw
}

func redactQueryValue(raw, key string) string {
	needle := key + "="
	offset := 0
	for {
		idx := strings.Index(raw[offset:], needle)
		if idx < 0 {
			return raw
		}
		start := offset + idx
		if start > 0 && raw[start-1] != '?' && raw[start-1] != '&' {
			offset = start + len(needle)
			continue
		}

		valueStart := start + len(needle)
		if strings.HasPrefix(raw[valueStart:], "REDACTED") {
			offset = valueStart + len("REDACTED")
			continue
		}
		valueEnd := valueStart
		for valueEnd < len(raw) && raw[valueEnd] != '&' && raw[valueEnd] != '"' && raw[valueEnd] != 39 {
			valueEnd++
		}
		raw = raw[:valueStart] + "REDACTED" + raw[valueEnd:]
		offset = valueStart + len("REDACTED")
	}
}

func (c *Client) applyEngineParams(params url.Values) {
	switch c.opts.Engine {
	case "duckduckgo_news":
		setQueryParam(params, "kl", c.opts.Region)
		setQueryParam(params, "safe", c.opts.Safe)
		setQueryParam(params, "df", c.opts.DateFilter)
		if c.opts.Start > 0 {
			params.Set("start", fmt.Sprintf("%d", c.opts.Start))
		}
		if c.opts.MaxResults > 0 {
			params.Set("m", fmt.Sprintf("%d", c.opts.MaxResults))
		}
	case "google_news":
		setQueryParam(params, "gl", c.opts.Country)
		setQueryParam(params, "hl", c.opts.Language)
		if c.opts.SortOrder > 0 {
			params.Set("so", fmt.Sprintf("%d", c.opts.SortOrder))
		}
	case "bing_news":
		setQueryParam(params, "cc", c.opts.Country)
		setQueryParam(params, "mkt", c.opts.Language)
		setQueryParam(params, "safeSearch", c.opts.Safe)
		setQueryParam(params, "qft", c.opts.Filter)
		if c.opts.Start > 0 {
			params.Set("first", fmt.Sprintf("%d", c.opts.Start))
		}
		if c.opts.MaxResults > 0 {
			params.Set("count", fmt.Sprintf("%d", c.opts.MaxResults))
		}
	default:
		setQueryParam(params, "gl", c.opts.Country)
		setQueryParam(params, "hl", c.opts.Language)
	}
}
