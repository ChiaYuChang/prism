package fetcher

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/ChiaYuChang/prism/internal/collector"
)

// HTTPFetcher fetches raw HTML from a URL using a standard HTTP client.
type HTTPFetcher struct {
	client *http.Client
}

var _ collector.Fetcher = (*HTTPFetcher)(nil)

func NewHTTPFetcher(client *http.Client) *HTTPFetcher {
	return &HTTPFetcher{client: client}
}

// Fetch fetches a URL and returns the body, failing on non-2xx status codes.
// Use RetryFetcher for retry logic with per-status-code handling.
func (f *HTTPFetcher) Fetch(ctx context.Context, url string) (string, error) {
	body, status, err := f.fetchWithStatus(ctx, url)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("fetch %s: unexpected status %d", url, status)
	}
	return body, nil
}

// fetchWithStatus performs the HTTP GET and returns body, status code, and any
// network-level error. A non-2xx status is NOT returned as an error here;
// callers (e.g. RetryFetcher) inspect the status code themselves.
func (f *HTTPFetcher) fetchWithStatus(ctx context.Context, url string) (body string, statusCode int, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Prism/1.0)")

	resp, err := f.client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", resp.StatusCode, fmt.Errorf("read body from %s: %w", url, err)
	}
	return string(raw), resp.StatusCode, nil
}
