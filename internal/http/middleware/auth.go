package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// TokenAuthHeader is the HTTP header used by operator clients to authenticate
// to protected API routes.
const TokenAuthHeader = "X-PRISM-TOKEN"

// AuthTokenHeader is the HTTP header used for token-list authentication.
const AuthTokenHeader = "X-AUTH-TOKEN"

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

// TokenListAuth requires callers to provide AuthTokenHeader with a token that
// exists in tokens. Empty tokens are ignored; an empty set denies all requests.
func TokenListAuth(tokens map[string]struct{}) Middleware {
	allowed := make(map[string]struct{}, len(tokens))
	for token := range tokens {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		allowed[token] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := strings.TrimSpace(r.Header.Get(AuthTokenHeader))
			if _, ok := allowed[token]; token == "" || !ok {
				http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
