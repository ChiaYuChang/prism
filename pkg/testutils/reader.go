package testutils

import (
	"errors"
	"io"
	"time"
)

// ErrTest provides a standard error for unit testing failure scenarios.
var ErrTest = errors.New("test error")

// DelayedReader simulates network latency or slow data streams.
type DelayedReader struct {
	Data  []byte
	Delay time.Duration
}

// Read implements the io.Reader interface with a one-time delay.
func (r *DelayedReader) Read(p []byte) (int, error) {
	if r.Delay > 0 {
		time.Sleep(r.Delay)
		r.Delay = 0 // Delay only occurs on the first read
	}

	if len(r.Data) == 0 {
		return 0, io.EOF
	}

	n := copy(p, r.Data)
	r.Data = r.Data[n:]
	return n, nil
}

// ErrorReader always returns an error during the read operation.
type ErrorReader struct{}

func (r ErrorReader) Read(p []byte) (int, error) {
	return 0, ErrTest
}

// ErrorReadCloser combines ErrorReader with a failing Close operation.
type ErrorReadCloser struct{ ErrorReader }

func (r ErrorReadCloser) Close() error {
	return ErrTest
}

// HookReader allows execution of a callback function when reaching the end of the stream.
type HookReader struct {
	io.Reader
	Hook func()
}

// Read implements io.Reader and triggers the Hook when io.EOF is encountered.
func (r HookReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err == io.EOF && r.Hook != nil {
		r.Hook()
	}
	return n, err
}
