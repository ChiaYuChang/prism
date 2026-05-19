package middleware_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ChiaYuChang/prism/internal/http/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
}

func TestRequestID_GeneratesWhenMissing(t *testing.T) {
	h := middleware.RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := middleware.RequestIDFromContext(r.Context())
		require.NotEmpty(t, id, "request id should be populated in context")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.NotEmpty(t, rec.Header().Get(middleware.RequestIDHeader))
}

func TestRequestID_PropagatesInboundHeader(t *testing.T) {
	const inbound = "caller-supplied-id"

	h := middleware.RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, inbound, middleware.RequestIDFromContext(r.Context()))
	}))

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set(middleware.RequestIDHeader, inbound)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, inbound, rec.Header().Get(middleware.RequestIDHeader))
}

func TestTokenAuth_AllowsMatchingToken(t *testing.T) {
	called := false
	h := middleware.TokenAuth("secret-token")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set(middleware.TokenAuthHeader, "secret-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestTokenAuth_RejectsMissingToken(t *testing.T) {
	called := false
	h := middleware.TokenAuth("secret-token")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestTokenAuth_RejectsWrongToken(t *testing.T) {
	called := false
	h := middleware.TokenAuth("secret-token")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set(middleware.TokenAuthHeader, "wrong-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestTokenAuth_EmptyTokenNoops(t *testing.T) {
	called := false
	h := middleware.TokenAuth("")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRecoverer_ConvertsPanicTo500(t *testing.T) {
	h := middleware.Recoverer(discardLogger())(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("boom")
	}))

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestLogger_WritesOneLine(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	h := middleware.Logger(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	log := buf.String()
	assert.Contains(t, log, "http_request")
	assert.Contains(t, log, "method=GET")
	assert.Contains(t, log, "path=/test")
	assert.Contains(t, log, "status=200")
	assert.Equal(t, 1, strings.Count(log, "http_request"), "exactly one log line expected")
}

func TestCORS_PreflightShortCircuits(t *testing.T) {
	called := false
	h := middleware.CORS(middleware.CORSOptions{
		AllowOrigins: []string{"https://example.com"},
		AllowMethods: []string{"GET", "POST"},
		AllowHeaders: []string{"Content-Type"},
		MaxAgeSecs:   600,
	})(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodOptions, "/x", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.False(t, called, "preflight should not reach inner handler")
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, "https://example.com", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "GET, POST", rec.Header().Get("Access-Control-Allow-Methods"))
	assert.Equal(t, "600", rec.Header().Get("Access-Control-Max-Age"))
}

func TestCORS_UnlistedOriginNotAllowed(t *testing.T) {
	h := middleware.CORS(middleware.CORSOptions{
		AllowOrigins: []string{"https://allowed.com"},
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Origin", "https://blocked.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestCORS_WildcardAllowAny(t *testing.T) {
	h := middleware.CORS(middleware.CORSOptions{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{"GET"},
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Origin", "https://anywhere.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestChain_OutermostRunsFirst(t *testing.T) {
	var order []string

	a := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "a-in")
			next.ServeHTTP(w, r)
			order = append(order, "a-out")
		})
	}
	b := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "b-in")
			next.ServeHTTP(w, r)
			order = append(order, "b-out")
		})
	}
	inner := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		order = append(order, "inner")
	})

	h := middleware.Chain(a, b)(inner)
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	assert.Equal(t, []string{"a-in", "b-in", "inner", "b-out", "a-out"}, order)
}

func TestRateLimit_AllowsThenBlocks(t *testing.T) {
	limiter := middleware.NewInMemoryIPLimiter(1, 2, 16)
	called := 0
	h := middleware.RateLimit(limiter)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	}))

	mk := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/x", nil)
		r.RemoteAddr = "10.0.0.42:9999"
		return r
	}

	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, mk())
	require.Equal(t, http.StatusOK, rec1.Code)

	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, mk())
	require.Equal(t, http.StatusOK, rec2.Code)

	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, mk())
	require.Equal(t, http.StatusTooManyRequests, rec3.Code)
	require.Equal(t, "1", rec3.Header().Get("Retry-After"))
	require.Equal(t, 2, called)
}

func TestRateLimit_PerIPIsolation(t *testing.T) {
	limiter := middleware.NewInMemoryIPLimiter(1, 1, 16)
	h := middleware.RateLimit(limiter)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	mk := func(ip string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/x", nil)
		r.RemoteAddr = ip + ":1"
		return r
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, mk("10.0.0.1"))
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, mk("10.0.0.2"))
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, mk("10.0.0.1"))
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
}

func TestClientIP_PrefersXForwardedFor(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.1")
	require.Equal(t, "203.0.113.7", middleware.ClientIP(r))
}

func TestClientIP_FallsBackToRemoteAddr(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	require.Equal(t, "10.0.0.1", middleware.ClientIP(r))
}
