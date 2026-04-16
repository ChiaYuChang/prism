package brave

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ChiaYuChang/prism/internal/discovery"
	"github.com/ChiaYuChang/prism/internal/model"
)

var _ discovery.SearchClient = (*Client)(nil)

type Options struct {
	Count      int    // results per request, max 50, default 20
	SearchLang string // default "zh"
	Country    string // default "TW"
	Freshness  string // default "pw" (past week)
}

func DefaultOptions() Options {
	return Options{
		Count:      20,
		SearchLang: "zh",
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
		opts.SearchLang = "zh"
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

type requestBody struct {
	Q          string `json:"q"`
	Count      int    `json:"count"`
	SearchLang string `json:"search_lang"`
	Country    string `json:"country"`
	Freshness  string `json:"freshness"`
}

type searchResponse struct {
	Type    string       `json:"type"`
	Results []newsResult `json:"results"`
}

type newsResult struct {
	Title       string    `json:"title"`
	URL         string    `json:"url"`
	Description string    `json:"description"`
	Age         string    `json:"age"`
	MetaURL     *metaURL  `json:"meta_url"`
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

	body := requestBody{
		Q:          q,
		Count:      c.opts.Count,
		SearchLang: c.opts.SearchLang,
		Country:    c.opts.Country,
		Freshness:  c.opts.Freshness,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("x-subscription-token", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("brave search request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, ErrRateLimited
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("brave search: status %d", resp.StatusCode)
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
			"search_engine": "brave",
			"query":         query,
		}
		if site != "" {
			meta["site_filter"] = site
		}
		if r.Age != "" {
			meta["age"] = r.Age
		}
		if r.MetaURL != nil && r.MetaURL.Hostname != "" {
			meta["hostname"] = r.MetaURL.Hostname
		}
		if r.Thumbnail != nil && r.Thumbnail.Src != "" {
			meta["thumbnail"] = r.Thumbnail.Src
		}

		candidates = append(candidates, model.Candidates{
			URL:         r.URL,
			Title:       r.Title,
			Description: strings.TrimSpace(r.Description),
			PublishedAt: now,
			DiscoveredAt: now,
			Metadata:    meta,
		})
	}

	return candidates, nil
}
