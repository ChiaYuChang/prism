package dev_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ChiaYuChang/prism/internal/dev"
	"github.com/stretchr/testify/require"
)

func TestCaptureTransport_TeesSuccessfulBodyToDisk(t *testing.T) {
	const FixtureResponseText = "<html><body>hello</body></html>"

	srv := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(FixtureResponseText))
		}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	client := &http.Client{
		Transport: dev.NewCaptureTransport(http.DefaultTransport, dir, nil),
	}

	resp, err := client.Get(srv.URL + "/news/123")
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, FixtureResponseText, string(body))

	host := srv.Listener.Addr().String()
	saved, err := os.ReadFile(filepath.Join(dir, host, "news", "123"))
	require.NoError(t, err)
	require.Equal(t, body, saved)
}

func TestCaptureTransport_SkipsNon2xx(t *testing.T) {
	srv := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "nope", http.StatusNotFound)
		}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	client := &http.Client{Transport: dev.NewCaptureTransport(http.DefaultTransport, dir, nil)}

	resp, err := client.Get(srv.URL + "/missing")
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)

	entries, _ := os.ReadDir(dir)
	require.Empty(t, entries, "no fixture should be written for non-2xx responses")
}

func TestCaptureTransport_FixturePathRules(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want string // path under <dir>/<host>/
	}{
		{"trailing slash", "/news/", "news/index.html"},
		{"empty path", "", "index.html"},
		{"no extension", "/news/123", "news/123"},
		{"already .html", "/news/123.html", "news/123.html"},
		{"non-html ext", "/news/123.aspx", "news/123.aspx"},
		{"with query", "/list?page=2&size=10", "list__page-2_size-10"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = w.Write([]byte("body"))
				}))
			t.Cleanup(srv.Close)

			dir := t.TempDir()
			client := &http.Client{
				Transport: dev.NewCaptureTransport(http.DefaultTransport, dir, nil)}

			resp, err := client.Get(srv.URL + tc.url)
			require.NoError(t, err)
			_ = resp.Body.Close()

			host := srv.Listener.Addr().String()
			full := filepath.Join(dir, host, tc.want)
			_, err = os.Stat(full)
			require.NoErrorf(t, err, "expected fixture at %s", full)
		})
	}
}

func TestWrapClient_NoOpWhenDirEmpty(t *testing.T) {
	c := &http.Client{}
	got := dev.WrapClient(c, "", nil)
	require.Same(t, c, got)
	require.Nil(t, c.Transport, "Transport should remain unset when dir is empty")
}

func TestWrapClient_InstallsTransport(t *testing.T) {
	c := &http.Client{}
	dev.WrapClient(c, "/tmp/fixtures", nil)
	_, ok := c.Transport.(*dev.CaptureTransport)
	require.True(t, ok, "Transport should be a CaptureTransport")
}
