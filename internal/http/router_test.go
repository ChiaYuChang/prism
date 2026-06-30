package prismhttp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ChiaYuChang/prism/internal/http/middleware"
	"github.com/stretchr/testify/require"
)

func TestRouter_RouteInheritsParentAndScopesChildMiddleware(t *testing.T) {
	parentMW := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Parent", "1")
			next.ServeHTTP(w, r)
		})
	}
	childMW := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Child", "1")
			next.ServeHTTP(w, r)
		})
	}

	rootRouter := NewRouter(parentMW)
	rootRouter.Route("/api/v1", func(apiV1Router *Router) {
		apiV1Router.HandleFunc("GET /public", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("public"))
		})
		apiV1Router.Route("/admin", func(adminRouter *Router) {
			adminRouter.HandleFunc("GET /private", func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("private"))
			})
		}, childMW)
	})

	publicRec := httptest.NewRecorder()
	rootRouter.Handler().ServeHTTP(publicRec, httptest.NewRequest(http.MethodGet, "/api/v1/public", nil))
	require.Equal(t, http.StatusOK, publicRec.Code)
	require.Equal(t, "1", publicRec.Header().Get("X-Parent"))
	require.Empty(t, publicRec.Header().Get("X-Child"))

	privateRec := httptest.NewRecorder()
	rootRouter.Handler().ServeHTTP(privateRec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/private", nil))
	require.Equal(t, http.StatusOK, privateRec.Code)
	require.Equal(t, "1", privateRec.Header().Get("X-Parent"))
	require.Equal(t, "1", privateRec.Header().Get("X-Child"))
}

func TestRouter_RouteCanScopeTokenAuth(t *testing.T) {
	rootRouter := NewRouter()
	rootRouter.Route("/api/v1", func(apiV1Router *Router) {
		apiV1Router.HandleFunc("GET /public", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("public"))
		})
		apiV1Router.Route("/admin", func(adminRouter *Router) {
			adminRouter.HandleFunc("GET /private", func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("private"))
			})
		}, middleware.TokenListAuth(map[string]struct{}{"secret": {}}))
	})

	publicRec := httptest.NewRecorder()
	rootRouter.Handler().ServeHTTP(publicRec, httptest.NewRequest(http.MethodGet, "/api/v1/public", nil))
	require.Equal(t, http.StatusOK, publicRec.Code)

	deniedRec := httptest.NewRecorder()
	rootRouter.Handler().ServeHTTP(deniedRec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/private", nil))
	require.Equal(t, http.StatusUnauthorized, deniedRec.Code)

	allowedReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/private", nil)
	allowedReq.Header.Set(middleware.TokenAuthHeader, "secret")
	allowedRec := httptest.NewRecorder()
	rootRouter.Handler().ServeHTTP(allowedRec, allowedReq)
	require.Equal(t, http.StatusOK, allowedRec.Code)
}
