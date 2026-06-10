//go:build manual

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

// recordingEnabled reports whether the manual tests should hit the real API
// and persist responses. Replay-only runs leave this unset.
func recordingEnabled() bool {
	return os.Getenv("PRISM_GEMINI_RECORD") == "1"
}
