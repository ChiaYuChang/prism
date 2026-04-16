package brave_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ChiaYuChang/prism/internal/discovery/search/brave"
	"github.com/stretchr/testify/require"
)

func TestClient_DiscoverNews_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		require.Equal(t, "test-key", r.Header.Get("x-subscription-token"))

		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "台灣半導體", body["q"])

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
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := brave.NewClientWithURL(
		&http.Client{}, "test-key", brave.DefaultOptions(), srv.URL,
	)

	candidates, err := client.DiscoverNews(context.Background(), "台灣半導體", "")
	require.NoError(t, err)
	require.Len(t, candidates, 3)

	require.Equal(t, "TSMC expands", candidates[0].Title)
	require.Equal(t, "https://example.com/tsmc", candidates[0].URL)
	require.Equal(t, "TSMC announced expansion plans.", candidates[0].Description)
	require.Equal(t, "brave", candidates[0].Metadata["search_engine"])
	require.Equal(t, "2 hours ago", candidates[0].Metadata["age"])
	require.Equal(t, "example.com", candidates[0].Metadata["hostname"])

	require.Equal(t, "Third result", candidates[2].Title)
	require.Empty(t, candidates[2].Description)
}

func TestClient_DiscoverNews_WithSiteFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "台灣政策 site:cna.com.tw", body["q"])

		resp := map[string]any{"type": "news", "results": []any{}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := brave.NewClientWithURL(
		&http.Client{}, "key", brave.DefaultOptions(), srv.URL,
	)

	candidates, err := client.DiscoverNews(context.Background(), "台灣政策", "cna.com.tw")
	require.NoError(t, err)
	require.Empty(t, candidates)
}

func TestClient_DiscoverNews_EmptyResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{"type": "news", "results": []any{}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := brave.NewClientWithURL(
		&http.Client{}, "key", brave.DefaultOptions(), srv.URL,
	)

	candidates, err := client.DiscoverNews(context.Background(), "nonexistent query", "")
	require.NoError(t, err)
	require.Empty(t, candidates)
}

func TestClient_DiscoverNews_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	client := brave.NewClientWithURL(
		&http.Client{}, "key", brave.DefaultOptions(), srv.URL,
	)

	_, err := client.DiscoverNews(context.Background(), "query", "")
	require.Error(t, err)
	require.True(t, errors.Is(err, brave.ErrRateLimited))
}

func TestClient_DiscoverNews_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := brave.NewClientWithURL(
		&http.Client{}, "key", brave.DefaultOptions(), srv.URL,
	)

	_, err := client.DiscoverNews(context.Background(), "query", "")
	require.Error(t, err)
	require.False(t, errors.Is(err, brave.ErrRateLimited))
}

func TestClient_DiscoverNews_SkipsEmptyTitleOrURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"type": "news",
			"results": []map[string]any{
				{"title": "", "url": "https://example.com/a"},
				{"title": "Good", "url": ""},
				{"title": "Valid", "url": "https://example.com/b", "description": "ok"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := brave.NewClientWithURL(
		&http.Client{}, "key", brave.DefaultOptions(), srv.URL,
	)

	candidates, err := client.DiscoverNews(context.Background(), "test", "")
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, "Valid", candidates[0].Title)
}
