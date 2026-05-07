package gemini_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// cassette captures one HTTP response from a real API call so subsequent
// runs can replay it offline. We deliberately do NOT store the request
// (URL, headers, body) — request data may carry the API key (header or
// query string) and we never want secrets on disk.
type cassette struct {
	Status int    `json:"status"`
	Body   string `json:"body"`
}

func cassettePath(name string) string {
	return filepath.Join("testdata", "cassettes", name+".json")
}

func loadCassette(t *testing.T, name string) cassette {
	t.Helper()
	raw, err := os.ReadFile(cassettePath(name))
	require.NoError(t, err, "cassette %q missing — run with -tags=manual PRISM_GEMINI_RECORD=1 to capture", name)
	var c cassette
	require.NoError(t, json.Unmarshal(raw, &c))
	return c
}

func saveCassette(t *testing.T, name string, c cassette) {
	t.Helper()
	dir := filepath.Dir(cassettePath(name))
	require.NoError(t, os.MkdirAll(dir, 0o755))
	raw, err := json.MarshalIndent(c, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(cassettePath(name), raw, 0o644))
}

// recordingTransport delegates to base, captures response into a cassette.
// The captured body is restored on the response so the live SDK can still
// consume it during the recording run.
type recordingTransport struct {
	t    *testing.T
	base http.RoundTripper
	name string
}

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := r.base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return nil, fmt.Errorf("recordingTransport: read body: %w", readErr)
	}
	saveCassette(r.t, r.name, cassette{Status: resp.StatusCode, Body: string(body)})
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return resp, nil
}

// replayTransport answers every request with one canned cassette,
// regardless of method / path / body. Each test owns its cassette so no
// matching layer is needed.
type replayTransport struct {
	c cassette
}

func (r *replayTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		Status:     fmt.Sprintf("%d", r.c.Status),
		StatusCode: r.c.Status,
		Body:       io.NopCloser(bytes.NewReader([]byte(r.c.Body))),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Request:    req,
	}, nil
}

// recordingEnabled reports whether the manual tests should hit the real
// API and persist responses. Replay-only runs leave this unset.
func recordingEnabled() bool {
	return os.Getenv("PRISM_GEMINI_RECORD") == "1"
}
