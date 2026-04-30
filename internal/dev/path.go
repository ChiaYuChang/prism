package dev

import (
	"net/url"
	"path"
	"path/filepath"
	"strings"
)

// FixturePath maps a URL into a deterministic on-disk filename rooted at
// the URL's host. Convention matches cmd/dev/downloader: no .html
// extension is appended; a directory-like path (empty or trailing slash)
// gets index.html so http.FileServer can serve it; query strings encode
// as __<sanitized> before the extension. Used by both CaptureTransport
// (write side) and ReplayTransport (read side) so capture and replay
// round-trip consistently.
func FixturePath(u *url.URL) string {
	p := u.Path
	if p == "" || strings.HasSuffix(p, "/") {
		p = path.Join(p, "index.html")
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
