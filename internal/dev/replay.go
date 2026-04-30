package dev

import (
	"fmt"
	"net/http"
	"net/url"
	"path"
)

// ReplayTransport rewrites outbound requests to point at a local
// fixture-server (cmd/dev/fixture-server) so workers can run end-to-end
// against captured fixtures with zero real-site traffic. Mirror of
// CaptureTransport: capture writes <dir>/<host>/<FixturePath>; replay
// reads from <fixtureBase>/<host>/<FixturePath>.
type ReplayTransport struct {
	base    http.RoundTripper
	baseURL *url.URL
}

func NewReplayTransport(base http.RoundTripper, fixtureBaseURL string) (*ReplayTransport, error) {
	parsed, err := url.Parse(fixtureBaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse fixture-base URL: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("fixture-base URL must include scheme and host: %q", fixtureBaseURL)
	}
	if base == nil {
		base = http.DefaultTransport
	}
	return &ReplayTransport{base: base, baseURL: parsed}, nil
}

func (t *ReplayTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rewritten := req.Clone(req.Context())
	rewritten.URL = &url.URL{
		Scheme: t.baseURL.Scheme,
		Host:   t.baseURL.Host,
		Path:   path.Join("/", req.URL.Host, FixturePath(req.URL)),
	}
	rewritten.Host = t.baseURL.Host
	return t.base.RoundTrip(rewritten)
}

// WrapClientReplay installs a ReplayTransport on c when fixtureBase is
// non-empty. Returns c unchanged on empty fixtureBase so callers can wire
// it unconditionally.
func WrapClientReplay(c *http.Client, fixtureBase string) (*http.Client, error) {
	if c == nil || fixtureBase == "" {
		return c, nil
	}
	base := c.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	rt, err := NewReplayTransport(base, fixtureBase)
	if err != nil {
		return nil, err
	}
	c.Transport = rt
	return c, nil
}
