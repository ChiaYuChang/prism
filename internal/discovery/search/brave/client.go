package brave

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

type Options struct {
	Count                int    // results per request, max 50, default 20
	Offset               int    // zero-based page offset, max 9
	SearchLang           string // default "zh"
	UILang               string // e.g. "zh-TW"
	Country              string // default "TW"
	Freshness            string // default "pw" (past week)
	SafeSearch           string // off, moderate, strict
	Spellcheck           *bool
	ExtraSnippets        string
	Goggles              string
	IncludeFetchMetadata *bool
	Operators            *bool
	APIVersion           string
	CacheControl         string
	UserAgent            string
}

func DefaultOptions() Options {
	return Options{
		Count:      20,
		SearchLang: "zh-hant",
		Country:    "TW",
		Freshness:  "pw",
	}
}

type Client struct {
	httpClient *http.Client
	apiKey     string
	baseURL    string
	opts       Options
}

func NewClient(httpClient *http.Client, apiKey string, opts Options) *Client {
	return NewClientWithURL(httpClient, apiKey, opts, searchURL)
}

func NewClientWithURL(httpClient *http.Client, apiKey string, opts Options, baseURL string) *Client {
	if opts.Count <= 0 || opts.Count > 50 {
		opts.Count = 20
	}
	if opts.SearchLang == "" {
		opts.SearchLang = "zh-hant"
	}
	if opts.Country == "" {
		opts.Country = "TW"
	}
	if opts.Freshness == "" {
		opts.Freshness = "pw"
	}
	return &Client{
		httpClient: httpClient,
		apiKey:     apiKey,
		baseURL:    baseURL,
		opts:       opts,
	}
}

const searchURL = "https://api.search.brave.com/res/v1/news/search"

type searchResponse struct {
	Type    string       `json:"type"`
	Results []newsResult `json:"results"`
}

type newsResult struct {
	Title       string     `json:"title"`
	URL         string     `json:"url"`
	Description string     `json:"description"`
	Age         string     `json:"age"`
	PageAge     string     `json:"page_age"`
	PageFetched string     `json:"page_fetched"`
	Breaking    bool       `json:"breaking"`
	MetaURL     *metaURL   `json:"meta_url"`
	Thumbnail   *thumbnail `json:"thumbnail"`
}

type metaURL struct {
	Hostname string `json:"hostname"`
}

type thumbnail struct {
	Src string `json:"src"`
}

func (c *Client) DiscoverNews(ctx context.Context, query string, site string) ([]model.Candidates, error) {
	q := query
	if site != "" {
		q = query + " site:" + site
	}

	u, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse brave search url: %w", err)
	}
	params := u.Query()
	params.Set("q", q)
	params.Set("count", fmt.Sprintf("%d", c.opts.Count))
	setQueryParam(params, "search_lang", c.opts.SearchLang)
	setQueryParam(params, "ui_lang", c.opts.UILang)
	setQueryParam(params, "country", c.opts.Country)
	setQueryParam(params, "freshness", c.opts.Freshness)
	setQueryParam(params, "safesearch", c.opts.SafeSearch)
	setQueryParam(params, "extra_snippets", c.opts.ExtraSnippets)
	setQueryParam(params, "goggles", c.opts.Goggles)
	if c.opts.Offset > 0 {
		params.Set("offset", fmt.Sprintf("%d", c.opts.Offset))
	}
	setBoolQueryParam(params, "spellcheck", c.opts.Spellcheck)
	setBoolQueryParam(params, "include_fetch_metadata", c.opts.IncludeFetchMetadata)
	setBoolQueryParam(params, "operators", c.opts.Operators)
	u.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-subscription-token", c.apiKey)
	setHeader(req.Header, "api-version", c.opts.APIVersion)
	setHeader(req.Header, "cache-control", c.opts.CacheControl)
	setHeader(req.Header, "user-agent", c.opts.UserAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("brave search request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, ErrRateLimited
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("brave search: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var sr searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return nil, fmt.Errorf("decode brave response: %w", err)
	}

	now := time.Now()
	candidates := make([]model.Candidates, 0, len(sr.Results))
	for _, r := range sr.Results {
		if r.URL == "" || r.Title == "" {
			continue
		}

		meta := map[string]any{
			"search_provider": "brave",
			"query":           query,
		}
		if site != "" {
			meta["site_filter"] = site
		}
		if r.Age != "" {
			meta["age"] = r.Age
		}
		if r.PageAge != "" {
			meta["page_age"] = r.PageAge
		}
		if r.PageFetched != "" {
			meta["page_fetched"] = r.PageFetched
		}
		if r.Breaking {
			meta["breaking"] = true
		}
		if r.MetaURL != nil && r.MetaURL.Hostname != "" {
			meta["hostname"] = r.MetaURL.Hostname
		}
		if r.Thumbnail != nil && r.Thumbnail.Src != "" {
			meta["thumbnail"] = r.Thumbnail.Src
		}

		candidates = append(candidates, model.Candidates{
			URL:          r.URL,
			Title:        r.Title,
			Description:  strings.TrimSpace(r.Description),
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

func setBoolQueryParam(values url.Values, key string, value *bool) {
	if value != nil {
		values.Set(key, fmt.Sprintf("%t", *value))
	}
}

func setHeader(header http.Header, key, value string) {
	if value != "" {
		header.Set(key, value)
	}
}
