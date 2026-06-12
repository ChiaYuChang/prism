package middleware

import (
	"fmt"
	"net/http"
	"strings"
)

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
