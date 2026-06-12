package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"
)

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
