package testutils

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

type RoundTripFunc func(*http.Request) (*http.Response, error)

func (f RoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func Logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func MustNewServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()

	var srv *httptest.Server
	defer func() {
		if r := recover(); r != nil {
			t.Skipf("httptest server unavailable in this environment: %v", r)
		}
	}()

	srv = httptest.NewServer(handler)
	return srv
}
