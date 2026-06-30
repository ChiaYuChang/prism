package middleware

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	prismauth "github.com/ChiaYuChang/prism/internal/auth"
	authtoken "github.com/ChiaYuChang/prism/internal/auth/token"
	"github.com/google/uuid"
)

// TokenAuthHeader is the HTTP header used by operator clients to authenticate
// to protected API routes.
const TokenAuthHeader = "X-PRISM-TOKEN"

type principalContextKey struct{}

type Principal struct {
	TokenID uuid.UUID
	Type    authtoken.Type
	Source  string
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalContextKey{}).(Principal)
	return p, ok
}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, p)
}

type TokenAuthenticator struct {
	Authenticator *prismauth.Authenticator
	ErrorDetail   AuthErrorDetail
}

type AuthErrorDetail int

const (
	AuthErrorGeneric AuthErrorDetail = iota
	AuthErrorAdmin
)

func TokenAuthMiddleware(auth TokenAuthenticator) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := strings.TrimSpace(r.Header.Get(TokenAuthHeader))
			principal, err := auth.authenticate(r.Context(), raw)
			if err != nil {
				auth.writeError(w, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), principal)))
		})
	}
}

func (a TokenAuthenticator) authenticate(ctx context.Context, raw string) (Principal, error) {
	if raw == "" || a.Authenticator == nil {
		return Principal{}, prismauth.ErrUnauthorized
	}
	principal, err := a.Authenticator.AuthenticateToken(ctx, raw)
	if err != nil {
		return Principal{}, err
	}
	return Principal{TokenID: principal.TokenID, Type: principal.Type, Source: "db"}, nil
}

func (a TokenAuthenticator) writeError(w http.ResponseWriter, err error) {
	if errors.Is(err, prismauth.ErrForbidden) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	if a.ErrorDetail == AuthErrorAdmin {
		switch {
		case errors.Is(err, prismauth.ErrTokenExpired):
			http.Error(w, prismauth.ErrTokenExpired.Error(), http.StatusUnauthorized)
			return
		case errors.Is(err, prismauth.ErrTokenRevoked):
			http.Error(w, prismauth.ErrTokenRevoked.Error(), http.StatusUnauthorized)
			return
		}
	}
	http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
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

// TokenListAuth requires callers to provide TokenAuthHeader with a token that
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
			token := strings.TrimSpace(r.Header.Get(TokenAuthHeader))
			if _, ok := allowed[token]; token == "" || !ok {
				http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
