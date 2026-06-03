package dev

import (
	"net/url"
	"path"
	"path/filepath"
	"sort"
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
		p = base + "__" + sanitizeQuery(redactedQuery(u.Query())) + ext
	}
	return p
}

func redactedQuery(values url.Values) string {
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	redacted := make(url.Values, len(values))
	for _, key := range keys {
		vals := append([]string(nil), values[key]...)
		if isSensitiveQueryKey(key) {
			vals = []string{"REDACTED"}
		}
		redacted[key] = vals
	}
	return redacted.Encode()
}

func isSensitiveQueryKey(key string) bool {
	switch strings.ToLower(key) {
	case "key", "api_key", "apikey", "access_token", "token":
		return true
	default:
		return false
	}
}

func sanitizeQuery(q string) string {
	return strings.NewReplacer("&", "_", "=", "-", "/", "_", "?", "_").Replace(q)
}
