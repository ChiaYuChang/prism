// Package dev contains development-only shims that embed inside worker
// binaries to support fixture capture and error-injection during the
// integration test plan (docs/integration-test-plan.md). Not for production
// use; gated by explicit flags on the host binary.
package dev

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// CaptureTransport wraps an http.RoundTripper and tees successful response
// bodies to <dir>/<host>/<path>.html. Used during Phase 1 of the
// integration test plan to build a local HTML fixture corpus from one
// real-site run; subsequent phases replay against the captured fixtures
// via fixture-server, so this transport is only ever active when the
// worker is started with --capture-dir set.
//
// Capture failures (mkdir / write) are logged at WARN and do not affect
// the upstream request — the response always returns to the caller as if
// the transport were a plain pass-through.
type CaptureTransport struct {
	base   http.RoundTripper
	dir    string
	logger *slog.Logger
}

func NewCaptureTransport(base http.RoundTripper, dir string, logger *slog.Logger) *CaptureTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &CaptureTransport{base: base, dir: dir, logger: logger}
}

func (t *CaptureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return resp, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp, nil
	}
	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return nil, readErr
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))

	if writeErr := t.writeFixture(req.URL, body); writeErr != nil {
		t.logger.Warn("capture: write fixture failed",
			"url", req.URL.String(), "error", writeErr)
	}
	return resp, nil
}

func (t *CaptureTransport) writeFixture(u *url.URL, body []byte) error {
	full := filepath.Join(t.dir, u.Host, fixturePath(u))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, body, 0o644)
}

// fixturePath maps a URL into a deterministic on-disk filename rooted at
// <host>. Rules: trailing slash or empty path → index.html; non-.html
// paths get .html appended (so /foo.aspx becomes /foo.aspx.html); query
// strings become __<sanitized> before the extension so paginated URLs
// don't collide.
func fixturePath(u *url.URL) string {
	p := u.Path
	if p == "" || strings.HasSuffix(p, "/") {
		p = path.Join(p, "index.html")
	} else if !strings.EqualFold(filepath.Ext(p), ".html") {
		p += ".html"
	}
	if u.RawQuery != "" {
		ext := filepath.Ext(p)
		base := strings.TrimSuffix(p, ext)
		p = base + "__" + sanitizeQuery(u.RawQuery) + ext
	}
	return p
}

func sanitizeQuery(q string) string {
	return strings.NewReplacer("&", "_", "=", "-", "/", "_", "?", "_").Replace(q)
}

// WrapClient installs a CaptureTransport on c when dir is non-empty.
// Returns c unchanged if dir is empty so callers can wire it
// unconditionally on a constructed client.
func WrapClient(c *http.Client, dir string, logger *slog.Logger) *http.Client {
	if c == nil || dir == "" {
		return c
	}
	base := c.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	c.Transport = NewCaptureTransport(base, dir, logger)
	return c
}
