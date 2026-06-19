package middleware

import (
	"net/http"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const httpServerOperation = "http.server"

// HTTPTracing traces inbound HTTP requests and extracts any incoming trace context.
func HTTPTracing() Middleware {
	return func(next http.Handler) http.Handler {
		return otelhttp.NewHandler(
			next,
			httpServerOperation,
			otelhttp.WithFilter(traceableRequest),
			otelhttp.WithSpanNameFormatter(func(_ string, _ *http.Request) string {
				return httpServerOperation
			}),
		)
	}
}

func traceableRequest(r *http.Request) bool {
	path := r.URL.Path
	return path != "/metrics" && !strings.HasPrefix(path, "/debug/pprof")
}
