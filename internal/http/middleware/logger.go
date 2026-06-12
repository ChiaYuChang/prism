package middleware

import (
	"log/slog"
	"net/http"
	"time"
)

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
