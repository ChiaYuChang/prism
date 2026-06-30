package infra

import (
	"context"
	"time"
)

// NewDrainContext returns an independent bounded context for work that was
// already accepted before shutdown started.
func NewDrainContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(context.Background())
	}
	return context.WithTimeout(context.Background(), timeout)
}
