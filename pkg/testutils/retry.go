package testutils

import (
	"context"
	"errors"
	"time"
)

var ErrNilAttempt = errors.New("nil retry attempt")

// WithExponentialBackoff retries attempt until it succeeds, maxFailures is
// reached, or ctx is cancelled. maxFailures excludes the initial attempt.
func WithExponentialBackoff(ctx context.Context, maxFailures int, initialDelay time.Duration, attempt func() error) error {
	if attempt == nil {
		return ErrNilAttempt
	}
	if initialDelay <= 0 {
		initialDelay = time.Millisecond
	}
	if maxFailures < 0 {
		maxFailures = 0
	}

	delay := initialDelay
	var lastErr error

	for failure := 0; failure <= maxFailures; failure++ {
		err := attempt()
		if err == nil {
			return nil
		}
		lastErr = err

		if failure == maxFailures {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		delay *= 2
	}

	return lastErr
}
