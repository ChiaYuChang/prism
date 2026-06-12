// Package middleware provides minimal HTTP middleware for the prism API server.
// Middlewares are plain http.Handler decorators — no framework dependency.
package middleware

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

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

// TokenAuthHeader is the HTTP header used by operator clients to authenticate
// to protected API routes.
const TokenAuthHeader = "X-PRISM-TOKEN"

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

// TokenAuth requires callers to provide TokenAuthHeader with the configured
// token. Empty configured token disables the check so callers can compose it
// unconditionally while auth configuration is still optional.
func TokenAuth(token string) Middleware {
	token = strings.TrimSpace(token)
	if token == "" {
		return func(next http.Handler) http.Handler { return next }
	}
	expected := []byte(token)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := []byte(r.Header.Get(TokenAuthHeader))
			if subtle.ConstantTimeCompare(got, expected) != 1 {
				http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
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

// Logger logs one structured line per request: method, path, status, bytes, elapsed.
func Logger(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			logger.LogAttrs(r.Context(), slog.LevelInfo, "http_request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rec.statusCode()),
				slog.Int("bytes", rec.bytes),
				slog.Duration("elapsed", time.Since(start)),
				slog.String("request_id", RequestIDFromContext(r.Context())),
				slog.String("remote", r.RemoteAddr),
			)
		})
	}
}

// Recoverer catches panics in downstream handlers, logs a stack trace, and
// returns a 500 so a single bad request does not crash the server.
func Recoverer(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.ErrorContext(r.Context(), "panic in handler",
						slog.Any("panic", rec),
						slog.String("request_id", RequestIDFromContext(r.Context())),
						slog.String("stack", string(debug.Stack())),
					)
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// CORSOptions configures the CORS middleware. Empty AllowOrigins means no CORS
// headers are emitted (browser will block cross-origin requests by default).
type CORSOptions struct {
	AllowOrigins []string // exact match; "*" allows any origin
	AllowMethods []string // e.g. GET, POST, PUT, DELETE
	AllowHeaders []string // e.g. Content-Type, Authorization
	MaxAgeSecs   int      // preflight cache
}

// CORS applies Access-Control-Allow-* headers. Preflight (OPTIONS) short-circuits
// with 204 when the Origin matches; other methods fall through to the handler.
func CORS(opts CORSOptions) Middleware {
	allowed := make(map[string]bool, len(opts.AllowOrigins))
	allowAny := false
	for _, o := range opts.AllowOrigins {
		if o == "*" {
			allowAny = true
		}
		allowed[o] = true
	}
	methods := strings.Join(opts.AllowMethods, ", ")
	headers := strings.Join(opts.AllowHeaders, ", ")
	maxAge := ""
	if opts.MaxAgeSecs > 0 {
		maxAge = fmt.Sprintf("%d", opts.MaxAgeSecs)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && (allowAny || allowed[origin]) {
				if allowAny {
					w.Header().Set("Access-Control-Allow-Origin", "*")
				} else {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Add("Vary", "Origin")
				}
				if methods != "" {
					w.Header().Set("Access-Control-Allow-Methods", methods)
				}
				if headers != "" {
					w.Header().Set("Access-Control-Allow-Headers", headers)
				}
				if maxAge != "" {
					w.Header().Set("Access-Control-Max-Age", maxAge)
				}
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
