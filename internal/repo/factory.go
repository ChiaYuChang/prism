package repo

import "context"

type Closer interface {
	Close() error
}

type CloseFunc func() error

func (f CloseFunc) Close() error {
	return f()
}

type Factory interface {
	NewRepository(ctx context.Context) (Repository, Closer, error)
}
