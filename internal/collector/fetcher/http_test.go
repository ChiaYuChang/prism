package fetcher_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ChiaYuChang/prism/internal/collector/fetcher"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHTTPFetcher_ContentTypeGuard covers the stopgap assertion that rejects
// non-HTML 2xx responses. See docs/pipeline-wiring-design.md — the HTML
// pipeline downstream assumes goquery-parseable input, so a JSON response
// (origin drift, misconfiguration) must fail loud instead of being silently
// wrapped in <html><body>.
func TestHTTPFetcher_ContentTypeGuard(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		status      int
		wantErr     bool
		errContains string
	}{
		{"html ok", "text/html; charset=utf-8", 200, false, ""},
		{"html bare", "text/html", 200, false, ""},
		{"xhtml ok", "application/xhtml+xml", 200, false, ""},
		{"html uppercase param", "TEXT/HTML; charset=UTF-8", 200, false, ""},
		{"json rejected", "application/json", 200, true, "unexpected content-type"},
		{"xml rejected", "application/xml", 200, true, "unexpected content-type"},
		{"plain text rejected", "text/plain", 200, true, "unexpected content-type"},
		{"empty header rejected", "", 200, true, "unexpected content-type"},
		// 4xx responses skip the content-type check — status-code error fires
		// first. Origin may serve an error page with any content-type.
		{"404 bypasses guard", "application/json", 404, true, "unexpected status 404"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte("<html><body>ok</body></html>"))
			}))
			defer srv.Close()

			f := fetcher.NewHTTPFetcher(srv.Client())
			body, err := f.Fetch(context.Background(), srv.URL)

			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errContains)
				assert.Empty(t, body)
				return
			}
			require.NoError(t, err)
			assert.Contains(t, body, "ok")
		})
	}
}
