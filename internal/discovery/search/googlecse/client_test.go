package googlecse_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ChiaYuChang/prism/internal/discovery/search/googlecse"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestClient_DiscoverNews_HappyPath(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "test-key", r.URL.Query().Get("key"))
		require.Equal(t, "test-cx", r.URL.Query().Get("cx"))
		require.Equal(t, "台灣半導體", r.URL.Query().Get("q"))
		require.Equal(t, "10", r.URL.Query().Get("num"))
		require.Equal(t, "lang_zh-TW", r.URL.Query().Get("lr"))
		require.Equal(t, "countryTW", r.URL.Query().Get("cr"))
		require.Equal(t, "tw", r.URL.Query().Get("gl"))
		require.Equal(t, "zh-TW", r.URL.Query().Get("hl"))
		require.Equal(t, "tw.news.yahoo.com", r.URL.Query().Get("siteSearch"))
		require.Equal(t, "i", r.URL.Query().Get("siteSearchFilter"))

		resp := map[string]any{
			"items": []map[string]any{
				{
					"title":       "Yahoo result",
					"link":        "https://tw.news.yahoo.com/a",
					"snippet":     "Snippet text",
					"displayLink": "tw.news.yahoo.com",
					"pagemap": map[string]any{
						"cse_thumbnail": []map[string]any{{"src": "https://example.com/thumb.jpg"}},
					},
				},
				{"title": "Missing link", "link": ""},
			},
		}
		body, err := json.Marshal(resp)
		require.NoError(t, err)
		return jsonResponse(r, http.StatusOK, string(body)), nil
	})}

	client := googlecse.NewClient(httpClient, "test-key", "test-cx", googlecse.DefaultOptions())

	candidates, err := client.DiscoverNews(context.Background(), "台灣半導體", "tw.news.yahoo.com")
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, "Yahoo result", candidates[0].Title)
	require.Equal(t, "https://tw.news.yahoo.com/a", candidates[0].URL)
	require.Equal(t, "Snippet text", candidates[0].Description)
	require.Equal(t, "google-cse", candidates[0].Metadata["search_provider"])
	require.Equal(t, "tw.news.yahoo.com", candidates[0].Metadata["site_filter"])
	require.Equal(t, "tw.news.yahoo.com", candidates[0].Metadata["display_link"])
	require.Equal(t, "https://example.com/thumb.jpg", candidates[0].Metadata["thumbnail"])
}

func TestClient_DiscoverNews_ServerError(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(r, http.StatusInternalServerError, ""), nil
	})}

	client := googlecse.NewClient(httpClient, "key", "cx", googlecse.DefaultOptions())

	_, err := client.DiscoverNews(context.Background(), "query", "")
	require.Error(t, err)
}

func jsonResponse(req *http.Request, statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}
