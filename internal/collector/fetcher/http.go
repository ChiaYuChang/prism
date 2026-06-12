package fetcher

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

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

func (*HTTPFetcher) String() string { return "HTTPFetcher" }

// Fetch fetches a URL and returns the body, failing on non-2xx status codes.
// Use RetryFetcher for retry logic with per-status-code handling.
func (f *HTTPFetcher) Fetch(ctx context.Context, url string) (string, error) {
	body, status, _, err := f.fetchWithStatus(ctx, url)
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
func (f *HTTPFetcher) fetchWithStatus(ctx context.Context, url string) (body string, statusCode int, header http.Header, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", 0, nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-TW,zh;q=0.9,en-US;q=0.8,en;q=0.7")

	resp, err := f.client.Do(req)
	if err != nil {
		return "", 0, nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	header = resp.Header.Clone()

	// Guard against silent corruption when the origin changes response type
	// (e.g. serves JSON at a URL that was HTML yesterday). The HTML pipeline
	// downstream assumes goquery-parseable input; a non-HTML body would be
	// silently wrapped and produce garbage. Per docs/pipeline-wiring-design.md
	// this is the stopgap until per-source pipelines land.
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		ct := resp.Header.Get("Content-Type")
		if !isHTMLContentType(ct) {
			return "", resp.StatusCode, header, fmt.Errorf("fetch %s: unexpected content-type %q (want text/html)", url, ct)
		}
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", resp.StatusCode, header, fmt.Errorf("read body from %s: %w", url, err)
	}
	return string(raw), resp.StatusCode, header, nil
}

// isHTMLContentType accepts text/html and application/xhtml+xml with any
// parameters (e.g. "; charset=utf-8"). Empty headers are rejected — callers
// that want to be permissive can wrap HTTPFetcher.
func isHTMLContentType(ct string) bool {
	mediaType, _, _ := strings.Cut(ct, ";")
	mediaType = strings.TrimSpace(strings.ToLower(mediaType))
	return mediaType == "text/html" || mediaType == "application/xhtml+xml"
}
