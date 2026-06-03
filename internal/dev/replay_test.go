package dev_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ChiaYuChang/prism/internal/dev"
	"github.com/stretchr/testify/require"
)

func TestReplayTransport_RewritesToFixtureServer(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			_, _ = w.Write([]byte("served:" + r.URL.Path))
		}))
	t.Cleanup(srv.Close)

	rt, err := dev.NewReplayTransport(http.DefaultTransport, srv.URL)
	require.NoError(t, err)
	client := &http.Client{Transport: rt}

	resp, err := client.Get("https://www.dpp.org.tw/media/00")
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	require.Equal(t, "/www.dpp.org.tw/media/00", gotPath)
	require.Equal(t, "served:/www.dpp.org.tw/media/00", string(body))
}

func TestReplayTransport_DirectoryPathGetsIndexHtml(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
		}))
	t.Cleanup(srv.Close)

	rt, _ := dev.NewReplayTransport(http.DefaultTransport, srv.URL)
	client := &http.Client{Transport: rt}
	_, _ = client.Get("https://example.com/")
	require.Equal(t, "/example.com/index.html", gotPath)
}

func TestReplayTransport_QueryStringEncoded(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
	}))
	t.Cleanup(srv.Close)

	rt, _ := dev.NewReplayTransport(http.DefaultTransport, srv.URL)
	client := &http.Client{Transport: rt}
	_, _ = client.Get("https://api.example.com/feeds?start-index=1&max-results=10")
	require.Equal(t, "/api.example.com/feeds__max-results-10_start-index-1", gotPath)
}

func TestNewReplayTransport_RejectsInvalidURL(t *testing.T) {
	_, err := dev.NewReplayTransport(http.DefaultTransport, "not-a-url")
	require.Error(t, err)
}

func TestWrapClientReplay_NoOpWhenEmpty(t *testing.T) {
	c := &http.Client{}
	got, err := dev.WrapClientReplay(c, "")
	require.NoError(t, err)
	require.Same(t, c, got)
	require.Nil(t, c.Transport)
}

func TestWrapClientReplay_InstallsTransport(t *testing.T) {
	c := &http.Client{}
	_, err := dev.WrapClientReplay(c, "http://localhost:9999")
	require.NoError(t, err)
	_, ok := c.Transport.(*dev.ReplayTransport)
	require.True(t, ok)
}
