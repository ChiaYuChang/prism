package serpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/discovery/search/serpapi"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestClient_DiscoverNews_HappyPath(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "google_news", r.URL.Query().Get("engine"))
		require.Equal(t, "test-key", r.URL.Query().Get("api_key"))
		require.Equal(t, "computex site:tw.news.yahoo.com", r.URL.Query().Get("q"))
		require.Equal(t, "tw", r.URL.Query().Get("gl"))
		require.Equal(t, "zh-tw", r.URL.Query().Get("hl"))
		require.Empty(t, r.URL.Query().Get("so"))

		resp := map[string]any{
			"news_results": []map[string]any{
				{
					"position":        1,
					"title":           "Yahoo article",
					"link":            "https://tw.news.yahoo.com/a",
					"snippet":         "Snippet text",
					"thumbnail":       "https://example.com/thumb.jpg",
					"thumbnail_small": "https://example.com/small.jpg",
					"date":            "01/02/2026, 10:30 PM, +0800 CST",
					"iso_date":        "2026-01-02T14:30:00Z",
					"source": map[string]any{
						"name":    "Yahoo News",
						"icon":    "https://example.com/icon.ico",
						"authors": []string{"Reporter"},
					},
				},
				{
					"position": 2,
					"title":    "Grouped result",
					"stories": []map[string]any{{
						"position": 1,
						"title":    "Nested article",
						"link":     "https://tw.news.yahoo.com/nested",
						"source":   "Nested Source",
					}},
				},
			},
		}
		body, err := json.Marshal(resp)
		require.NoError(t, err)
		return jsonResponse(r, http.StatusOK, string(body)), nil
	})}

	client := serpapi.NewClient(httpClient, "test-key", serpapi.DefaultOptions())

	candidates, err := client.DiscoverNews(context.Background(), "computex", "tw.news.yahoo.com")
	require.NoError(t, err)
	require.Len(t, candidates, 2)
	require.Equal(t, "Yahoo article", candidates[0].Title)
	require.Equal(t, "https://tw.news.yahoo.com/a", candidates[0].URL)
	require.Equal(t, "serpapi", candidates[0].Metadata["search_provider"])
	require.Equal(t, "tw.news.yahoo.com", candidates[0].Metadata["site_filter"])
	require.Equal(t, "Yahoo News", candidates[0].Metadata["source_name"])
	require.Equal(t, "https://example.com/thumb.jpg", candidates[0].Metadata["thumbnail"])
	require.Equal(t, time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC), candidates[0].PublishedAt)
	require.Equal(t, "Nested article", candidates[1].Title)
	require.Equal(t, "Nested Source", candidates[1].Metadata["source_name"])
}

func TestClient_DiscoverNews_ServerError(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(r, http.StatusUnauthorized, `{"error":"bad key"}`), nil
	})}

	client := serpapi.NewClient(httpClient, "key", serpapi.DefaultOptions())

	_, err := client.DiscoverNews(context.Background(), "query", "")
	require.Error(t, err)
}

func TestClient_DiscoverNews_RequestErrorRedactsAPIKey(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("request failed: %s", r.URL.String())
	})}

	client := serpapi.NewClient(httpClient, "secret-key", serpapi.DefaultOptions())

	_, err := client.DiscoverNews(context.Background(), "query", "")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "secret-key")
	require.True(t, strings.Contains(err.Error(), "api_key=REDACTED") || !strings.Contains(err.Error(), "api_key="))
}

func TestClient_DiscoverNews_DuckDuckGoParams(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, "duckduckgo_news", r.URL.Query().Get("engine"))
		require.Equal(t, "computex site:tw.news.yahoo.com", r.URL.Query().Get("q"))
		require.Equal(t, "tw-zh", r.URL.Query().Get("kl"))
		require.Equal(t, "-1", r.URL.Query().Get("safe"))
		require.Equal(t, "w", r.URL.Query().Get("df"))
		require.Equal(t, "25", r.URL.Query().Get("start"))
		require.Equal(t, "75", r.URL.Query().Get("m"))

		return jsonResponse(r, http.StatusOK, `{"news_results":[]}`), nil
	})}

	client := serpapi.NewClient(httpClient, "test-key", serpapi.Options{
		Engine:     "duckduckgo_news",
		Region:     "tw-zh",
		Safe:       "-1",
		DateFilter: "w",
		Start:      25,
		MaxResults: 75,
	})

	candidates, err := client.DiscoverNews(context.Background(), "computex", "tw.news.yahoo.com")
	require.NoError(t, err)
	require.Empty(t, candidates)
}

func jsonResponse(req *http.Request, statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}
