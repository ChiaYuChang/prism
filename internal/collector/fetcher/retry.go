package fetcher

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/ChiaYuChang/prism/internal/collector"
)

var (
	ErrRetryable   = errors.New("retryable error")
	ErrClientError = errors.New("client error")
	ErrMaxRetries  = errors.New("max retries exceeded")
)

// ResponseHandler decides what to do with an HTTP response.
// Returns (retry=true, err) for retryable failures, (retry=false, nil) for success,
// and (retry=false, err) to fail fast without retrying.
type ResponseHandler func(ctx context.Context, resp *http.Response) (retry bool, err error)

// Built-in handlers.

// RetryHandler marks any response as retryable.
var RetryHandler ResponseHandler = func(_ context.Context, _ *http.Response) (bool, error) {
	return true, ErrRetryable
}

// FailFastHandler marks any response as a non-retryable failure.
var FailFastHandler ResponseHandler = func(_ context.Context, r *http.Response) (bool, error) {
	return false, fmt.Errorf("%w: status %d", ErrClientError, r.StatusCode)
}

// RetryAfterHandler retries after honoring the Retry-After header (seconds).
// Falls back to immediate retry signal if the header is absent or unparseable.
var RetryAfterHandler ResponseHandler = func(ctx context.Context, r *http.Response) (bool, error) {
	if raw := r.Header.Get("Retry-After"); raw != "" {
		if secs, err := strconv.Atoi(raw); err == nil && secs > 0 {
			timer := time.NewTimer(time.Duration(secs) * time.Second)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return false, ctx.Err()
			case <-timer.C:
			}
		}
	}
	return true, ErrRetryable
}

// defaultResponseHandler applies sensible defaults:
//   - 2xx           → success
//   - 429           → retry with Retry-After
//   - 5xx           → retry
//   - anything else → fail fast
var defaultResponseHandler ResponseHandler = func(ctx context.Context, r *http.Response) (bool, error) {
	switch {
	case r.StatusCode >= 200 && r.StatusCode < 300:
		return false, nil
	case r.StatusCode == 429:
		return RetryAfterHandler(ctx, r)
	case r.StatusCode >= 500:
		return true, fmt.Errorf("%w: status %d", ErrRetryable, r.StatusCode)
	default:
		return FailFastHandler(ctx, r)
	}
}

// RetryFetcher wraps a Fetcher with per-status-code response handling and
// exponential backoff retry. Unregistered status codes fall through to the
// default handler. Network-level errors (no response) are always retried.
type RetryFetcher struct {
	inner      *HTTPFetcher
	maxRetries int
	backoff    time.Duration
	handlers   map[int]ResponseHandler
	defaultH   ResponseHandler
}

var _ collector.Fetcher = (*RetryFetcher)(nil)

func (*RetryFetcher) String() string { return "RetryFetcher" }

func NewRetryFetcher(inner *HTTPFetcher, maxRetries int, initialBackoff time.Duration) *RetryFetcher {
	return &RetryFetcher{
		inner:      inner,
		maxRetries: maxRetries,
		backoff:    initialBackoff,
		handlers:   make(map[int]ResponseHandler),
		defaultH:   defaultResponseHandler,
	}
}

// Handle registers a ResponseHandler for a specific HTTP status code.
// Returns the fetcher for chaining.
func (f *RetryFetcher) Handle(statusCode int, h ResponseHandler) *RetryFetcher {
	f.handlers[statusCode] = h
	return f
}

// WithDefaultHandler replaces the default handler used for unregistered status codes.
func (f *RetryFetcher) WithDefaultHandler(h ResponseHandler) *RetryFetcher {
	f.defaultH = h
	return f
}

func (f *RetryFetcher) Fetch(ctx context.Context, url string) (string, error) {
	backoff := f.backoff
	var lastErr error

	for attempt := 0; attempt <= f.maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(backoff):
				backoff *= 2
			}
		}

		body, statusCode, header, err := f.inner.fetchWithStatus(ctx, url)
		if err != nil {
			// Network-level error (no HTTP response): always retry.
			lastErr = err
			continue
		}

		handler, ok := f.handlers[statusCode]
		if !ok {
			handler = f.defaultH
		}

		// Synthesise a minimal response for the handler.
		resp := &http.Response{StatusCode: statusCode, Header: header.Clone()}
		retry, handlerErr := handler(ctx, resp)
		if handlerErr == nil {
			return body, nil
		}
		if !retry {
			return "", handlerErr
		}
		lastErr = handlerErr
	}

	return "", fmt.Errorf("%w after %d attempts: %w", ErrMaxRetries, f.maxRetries, lastErr)
}
