package googlecse

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

// Options controls Google Custom Search requests.
type Options struct {
	Count            int
	Language         string
	Country          string
	GeoLocation      string
	InterfaceLang    string
	DateRestrict     string
	ExactTerms       string
	ExcludeTerms     string
	OrTerms          string
	HighQualityTerms string
	Safe             string
	Sort             string
	Filter           string
	ChineseSearch    string
}

// DefaultOptions returns Taiwan-oriented Google CSE defaults.
func DefaultOptions() Options {
	return Options{
		Count:         10,
		Language:      "lang_zh-TW",
		Country:       "countryTW",
		GeoLocation:   "tw",
		InterfaceLang: "zh-TW",
	}
}

// Client calls Google Custom Search JSON API.
type Client struct {
	httpClient *http.Client
	apiKey     string
	cx         string
	baseURL    string
	opts       Options
}

// NewClient creates a Google CSE client using the public API endpoint.
func NewClient(httpClient *http.Client, apiKey, cx string, opts Options) *Client {
	return NewClientWithURL(httpClient, apiKey, cx, opts, searchURL)
}

// NewClientWithURL creates a Google CSE client with an overrideable endpoint.
func NewClientWithURL(httpClient *http.Client, apiKey, cx string, opts Options, baseURL string) *Client {
	if opts.Count <= 0 || opts.Count > 10 {
		opts.Count = 10
	}
	if opts.Language == "" {
		opts.Language = "lang_zh-TW"
	}
	if opts.Country == "" {
		opts.Country = "tw"
	}
	return &Client{
		httpClient: httpClient,
		apiKey:     apiKey,
		cx:         cx,
		baseURL:    baseURL,
		opts:       opts,
	}
}

const searchURL = "https://customsearch.googleapis.com/customsearch/v1"

type searchResponse struct {
	Items []item `json:"items"`
}

type item struct {
	Title       string  `json:"title"`
	Link        string  `json:"link"`
	Snippet     string  `json:"snippet"`
	DisplayLink string  `json:"displayLink"`
	Pagemap     pagemap `json:"pagemap"`
}

type pagemap struct {
	Thumbnails []thumbnail `json:"cse_thumbnail"`
}

type thumbnail struct {
	Src string `json:"src"`
}

// DiscoverNews executes a Custom Search query and maps results into candidates.
func (c *Client) DiscoverNews(ctx context.Context, query string, site string) ([]model.Candidates, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse google cse url: %w", err)
	}
	params := u.Query()
	params.Set("key", c.apiKey)
	params.Set("cx", c.cx)
	params.Set("q", query)
	params.Set("num", fmt.Sprintf("%d", c.opts.Count))
	setQueryParam(params, "lr", c.opts.Language)
	setQueryParam(params, "cr", c.opts.Country)
	setQueryParam(params, "gl", c.opts.GeoLocation)
	setQueryParam(params, "hl", c.opts.InterfaceLang)
	setQueryParam(params, "dateRestrict", c.opts.DateRestrict)
	setQueryParam(params, "exactTerms", c.opts.ExactTerms)
	setQueryParam(params, "excludeTerms", c.opts.ExcludeTerms)
	setQueryParam(params, "orTerms", c.opts.OrTerms)
	setQueryParam(params, "hq", c.opts.HighQualityTerms)
	setQueryParam(params, "safe", c.opts.Safe)
	setQueryParam(params, "sort", c.opts.Sort)
	setQueryParam(params, "filter", c.opts.Filter)
	setQueryParam(params, "c2coff", c.opts.ChineseSearch)
	if site != "" {
		params.Set("siteSearch", site)
		params.Set("siteSearchFilter", "i")
	}
	u.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("google cse request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("google cse: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var sr searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return nil, fmt.Errorf("decode google cse response: %w", err)
	}

	now := time.Now()
	candidates := make([]model.Candidates, 0, len(sr.Items))
	for i, item := range sr.Items {
		if item.Link == "" || item.Title == "" {
			continue
		}
		meta := map[string]any{
			"search_provider": "google-cse",
			"query":           query,
			"rank":            i + 1,
		}
		if site != "" {
			meta["site_filter"] = site
		}
		if item.DisplayLink != "" {
			meta["display_link"] = item.DisplayLink
		}
		if len(item.Pagemap.Thumbnails) > 0 && item.Pagemap.Thumbnails[0].Src != "" {
			meta["thumbnail"] = item.Pagemap.Thumbnails[0].Src
		}

		candidates = append(candidates, model.Candidates{
			URL:          item.Link,
			Title:        item.Title,
			Description:  strings.TrimSpace(item.Snippet),
			PublishedAt:  now,
			DiscoveredAt: now,
			Metadata:     meta,
		})
	}

	return candidates, nil
}

func setQueryParam(values url.Values, key, value string) {
	if value != "" {
		values.Set(key, value)
	}
}
