package fetcher_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/collector/fetcher"
	"github.com/ChiaYuChang/prism/pkg/testutils"
	"github.com/stretchr/testify/require"
)

func TestRetryFetcher_SuccessOnFirstAttempt(t *testing.T) {
	srv := testutils.MustNewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>ok</html>"))
	}))
	defer srv.Close()

	f := fetcher.NewRetryFetcher(fetcher.NewHTTPFetcher(srv.Client()), 3, 10*time.Millisecond)
	body, err := f.Fetch(context.Background(), srv.URL)
	require.NoError(t, err)
	require.Contains(t, body, "ok")
}

func TestRetryFetcher_RetriesOnServerError(t *testing.T) {
	attempts := 0
	srv := testutils.MustNewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>recovered</html>"))
	}))
	defer srv.Close()

	f := fetcher.NewRetryFetcher(fetcher.NewHTTPFetcher(srv.Client()), 3, time.Millisecond)
	body, err := f.Fetch(context.Background(), srv.URL)
	require.NoError(t, err)
	require.Equal(t, 3, attempts)
	require.Contains(t, body, "recovered")
}

func TestRetryFetcher_FailFastOn404(t *testing.T) {
	attempts := 0
	srv := testutils.MustNewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	f := fetcher.NewRetryFetcher(fetcher.NewHTTPFetcher(srv.Client()), 3, time.Millisecond)
	_, err := f.Fetch(context.Background(), srv.URL)
	require.Error(t, err)
	require.ErrorIs(t, err, fetcher.ErrClientError)
	require.Equal(t, 1, attempts, "404 should not be retried")
}

func TestRetryFetcher_CustomHandler(t *testing.T) {
	attempts := 0
	srv := testutils.MustNewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	// Override 403 to retry instead of fail fast.
	f := fetcher.NewRetryFetcher(fetcher.NewHTTPFetcher(srv.Client()), 2, time.Millisecond).
		Handle(http.StatusForbidden, fetcher.RetryHandler)

	_, err := f.Fetch(context.Background(), srv.URL)
	require.Error(t, err)
	require.ErrorIs(t, err, fetcher.ErrMaxRetries)
	require.Equal(t, 3, attempts, "403 should be retried up to maxRetries+1 times")
}

func TestRetryFetcher_ExceedsMaxRetries(t *testing.T) {
	attempts := 0
	srv := testutils.MustNewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	f := fetcher.NewRetryFetcher(fetcher.NewHTTPFetcher(srv.Client()), 2, time.Millisecond)
	_, err := f.Fetch(context.Background(), srv.URL)
	require.Error(t, err)
	require.ErrorIs(t, err, fetcher.ErrMaxRetries)
	require.Equal(t, 3, attempts)
}

func TestRetryFetcher_ContextCancellation(t *testing.T) {
	srv := testutils.MustNewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	f := fetcher.NewRetryFetcher(fetcher.NewHTTPFetcher(srv.Client()), 3, 10*time.Millisecond)
	_, err := f.Fetch(ctx, srv.URL)
	require.Error(t, err)
}
