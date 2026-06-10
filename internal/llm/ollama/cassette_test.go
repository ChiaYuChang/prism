package ollama_test

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

// cassette captures one HTTP response from a real Ollama call so
// subsequent runs can replay it offline. Request data is never persisted
// — Ollama is local and unauthenticated, but keeping symmetry with the
// other providers keeps recorders auditable.
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
	require.NoError(t, err, "cassette %q missing — run with -tags=manual PRISM_OLLAMA_RECORD=1 to capture", name)
	var c cassette
	require.NoError(t, json.Unmarshal(raw, &c))
	return c
}

// replayTransport answers every request with one canned cassette,
// regardless of method / path / body.
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
