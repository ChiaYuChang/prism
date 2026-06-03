package brave_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ChiaYuChang/prism/internal/discovery/search/brave"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestClient_DiscoverNews_HappyPath(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "test-key", r.Header.Get("x-subscription-token"))
		require.Equal(t, "台灣半導體", r.URL.Query().Get("q"))
		require.Equal(t, "20", r.URL.Query().Get("count"))
		require.Equal(t, "zh-hant", r.URL.Query().Get("search_lang"))
		require.Equal(t, "TW", r.URL.Query().Get("country"))
		require.Equal(t, "pw", r.URL.Query().Get("freshness"))

		resp := map[string]any{
			"type": "news",
			"results": []map[string]any{
				{
					"title":       "TSMC expands",
					"url":         "https://example.com/tsmc",
					"description": "TSMC announced expansion plans.",
					"age":         "2 hours ago",
					"meta_url":    map[string]any{"hostname": "example.com"},
				},
				{
					"title":       "Chip policy update",
					"url":         "https://example.com/chip",
					"description": "Government updates semiconductor policy.",
					"age":         "5 hours ago",
				},
				{
					"title":       "Third result",
					"url":         "https://example.com/third",
					"description": "",
				},
			},
		}
		body, err := json.Marshal(resp)
		require.NoError(t, err)
		return jsonResponse(r, http.StatusOK, string(body)), nil
	})}

	client := brave.NewClient(httpClient, "test-key", brave.DefaultOptions())

	candidates, err := client.DiscoverNews(context.Background(), "台灣半導體", "")
	require.NoError(t, err)
	require.Len(t, candidates, 3)

	require.Equal(t, "TSMC expands", candidates[0].Title)
	require.Equal(t, "https://example.com/tsmc", candidates[0].URL)
	require.Equal(t, "TSMC announced expansion plans.", candidates[0].Description)
	require.Equal(t, "brave", candidates[0].Metadata["search_provider"])
	require.Equal(t, "2 hours ago", candidates[0].Metadata["age"])
	require.Equal(t, "example.com", candidates[0].Metadata["hostname"])

	require.Equal(t, "Third result", candidates[2].Title)
	require.Empty(t, candidates[2].Description)
}

func TestClient_DiscoverNews_WithSiteFilter(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "台灣政策 site:cna.com.tw", r.URL.Query().Get("q"))

		return jsonResponse(r, http.StatusOK, `{"type":"news","results":[]}`), nil
	})}

	client := brave.NewClient(httpClient, "key", brave.DefaultOptions())

	candidates, err := client.DiscoverNews(context.Background(), "台灣政策", "cna.com.tw")
	require.NoError(t, err)
	require.Empty(t, candidates)
}

func TestClient_DiscoverNews_WithOptionalParams(t *testing.T) {
	spellcheck := true
	includeFetchMetadata := true
	operators := true
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		q := r.URL.Query()
		require.Equal(t, "1", q.Get("offset"))
		require.Equal(t, "zh-TW", q.Get("ui_lang"))
		require.Equal(t, "moderate", q.Get("safesearch"))
		require.Equal(t, "true", q.Get("spellcheck"))
		require.Equal(t, "2", q.Get("extra_snippets"))
		require.Equal(t, "goggle", q.Get("goggles"))
		require.Equal(t, "true", q.Get("include_fetch_metadata"))
		require.Equal(t, "true", q.Get("operators"))
		require.Equal(t, "2025-01-01", r.Header.Get("api-version"))
		require.Equal(t, "no-cache", r.Header.Get("cache-control"))
		require.Equal(t, "prism-test", r.Header.Get("user-agent"))

		return jsonResponse(r, http.StatusOK, `{"type":"news","results":[]}`), nil
	})}

	client := brave.NewClient(httpClient, "key", brave.Options{
		Count:                10,
		Offset:               1,
		SearchLang:           "zh",
		UILang:               "zh-TW",
		Country:              "TW",
		Freshness:            "pd",
		SafeSearch:           "moderate",
		Spellcheck:           &spellcheck,
		ExtraSnippets:        "2",
		Goggles:              "goggle",
		IncludeFetchMetadata: &includeFetchMetadata,
		Operators:            &operators,
		APIVersion:           "2025-01-01",
		CacheControl:         "no-cache",
		UserAgent:            "prism-test",
	})

	candidates, err := client.DiscoverNews(context.Background(), "query", "")
	require.NoError(t, err)
	require.Empty(t, candidates)
}

func TestClient_DiscoverNews_EmptyResults(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(r, http.StatusOK, `{"type":"news","results":[]}`), nil
	})}

	client := brave.NewClient(httpClient, "key", brave.DefaultOptions())

	candidates, err := client.DiscoverNews(context.Background(), "nonexistent query", "")
	require.NoError(t, err)
	require.Empty(t, candidates)
}

func TestClient_DiscoverNews_RateLimited(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(r, http.StatusTooManyRequests, ""), nil
	})}

	client := brave.NewClient(httpClient, "key", brave.DefaultOptions())

	_, err := client.DiscoverNews(context.Background(), "query", "")
	require.Error(t, err)
	require.True(t, errors.Is(err, brave.ErrRateLimited))
}

func TestClient_DiscoverNews_ServerError(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(r, http.StatusInternalServerError, ""), nil
	})}

	client := brave.NewClient(httpClient, "key", brave.DefaultOptions())

	_, err := client.DiscoverNews(context.Background(), "query", "")
	require.Error(t, err)
	require.False(t, errors.Is(err, brave.ErrRateLimited))
}

func TestClient_DiscoverNews_SkipsEmptyTitleOrURL(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		resp := map[string]any{
			"type": "news",
			"results": []map[string]any{
				{"title": "", "url": "https://example.com/a"},
				{"title": "Good", "url": ""},
				{"title": "Valid", "url": "https://example.com/b", "description": "ok"},
			},
		}
		body, err := json.Marshal(resp)
		require.NoError(t, err)
		return jsonResponse(r, http.StatusOK, string(body)), nil
	})}

	client := brave.NewClient(httpClient, "key", brave.DefaultOptions())

	candidates, err := client.DiscoverNews(context.Background(), "test", "")
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, "Valid", candidates[0].Title)
}

func jsonResponse(req *http.Request, statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}
