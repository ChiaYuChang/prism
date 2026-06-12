// Package middleware provides minimal HTTP middleware for the prism API server.
// Middlewares are plain http.Handler decorators — no framework dependency.
package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

// ctxKey is unexported to avoid collisions with other packages using context.
type ctxKey int

const (
	ctxKeyRequestID ctxKey = iota
)

// RequestIDHeader is the HTTP header that carries the request identifier.
// Clients may supply one; otherwise the middleware generates a UUIDv7.
const RequestIDHeader = "X-Request-Id"

// Middleware is a decorator applied to http.Handler.
type Middleware func(http.Handler) http.Handler

// Chain composes middlewares so the leftmost runs outermost:
//
//	Chain(logger, recoverer)(h) == logger(recoverer(h))
func Chain(mws ...Middleware) Middleware {
	return func(h http.Handler) http.Handler {
		for i := len(mws) - 1; i >= 0; i-- {
			h = mws[i](h)
		}
		return h
	}
}

// RequestID extracts the inbound X-Request-Id header or generates a UUIDv7,
// places it into the request context, and echoes it in the response header.
func RequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(RequestIDHeader)
			if id == "" {
				if u, err := uuid.NewV7(); err == nil {
					id = u.String()
				}
			}
			w.Header().Set(RequestIDHeader, id)
			ctx := context.WithValue(r.Context(), ctxKeyRequestID, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequestIDFromContext returns the request ID set by the RequestID middleware,
// or an empty string if none is present.
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(ctxKeyRequestID).(string)
	return id
}

// statusRecorder captures the response status code for logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status != 0 {
		return
	}
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.WriteHeader(http.StatusOK)
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

func (s *statusRecorder) statusCode() int {
	if s.status == 0 {
		return http.StatusOK
	}
	return s.status
}
