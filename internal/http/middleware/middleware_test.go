package middleware_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ChiaYuChang/prism/internal/http/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
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

func TestTokenListAuth_AllowsKnownToken(t *testing.T) {
	called := false
	h := middleware.TokenListAuth(map[string]struct{}{"token-a": {}, "token-b": {}})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set(middleware.TokenAuthHeader, "token-b")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestTokenListAuth_RejectsMissingToken(t *testing.T) {
	called := false
	h := middleware.TokenListAuth(map[string]struct{}{"token-a": {}})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	assert.False(t, called)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestTokenListAuth_RejectsUnknownToken(t *testing.T) {
	called := false
	h := middleware.TokenListAuth(map[string]struct{}{"token-a": {}})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set(middleware.TokenAuthHeader, "token-b")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestTokenListAuth_EmptySetDeniesAll(t *testing.T) {
	called := false
	h := middleware.TokenListAuth(nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set(middleware.TokenAuthHeader, "token-a")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestTokenListAuth_ClonesTokenSet(t *testing.T) {
	tokens := map[string]struct{}{"token-a": {}}
	h := middleware.TokenListAuth(tokens)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	delete(tokens, "token-a")

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set(middleware.TokenAuthHeader, "token-a")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

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

func TestHTTPMetrics_RecordsRequest(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, meterProvider.Shutdown(context.Background())) })
	metrics, err := middleware.NewHTTPMetrics(meterProvider.Meter("test"))
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /test", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("hello"))
	})

	h := middleware.HTTPMetrics(metrics)(mux)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code)

	rm := collectHTTPMetrics(t, reader)
	attrs := map[string]string{
		"method":      http.MethodGet,
		"route":       "GET /test",
		"status_code": "201",
		"result":      "success",
	}
	require.Equal(t, int64(1), httpCounterValue(t, rm, "prism.http.server.requests", attrs))
	require.Equal(t, uint64(1), httpFloatHistogramCount(t, rm, "prism.http.server.request.duration", attrs))
	require.Equal(t, uint64(1), httpIntHistogramCount(t, rm, "prism.http.server.response.size", attrs))
}

func TestHTTPMetrics_NoopsWhenNil(t *testing.T) {
	called := false
	h := middleware.HTTPMetrics(nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/test", nil))
	require.True(t, called)
	require.Equal(t, http.StatusNoContent, rec.Code)
}

func TestCORS_PreflightShortCircuits(t *testing.T) {
	called := false
	h := middleware.CORS(middleware.CORSOptions{
		AllowOrigins: []string{"https://example.com"},
		AllowMethods: []string{http.MethodGet, http.MethodPost},
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

func collectHTTPMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	return rm
}

func httpCounterValue(t *testing.T, rm metricdata.ResourceMetrics, name string, attrs map[string]string) int64 {
	t.Helper()
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, dp := range sum.DataPoints {
				if httpAttributesMatch(dp.Attributes, attrs) {
					total += dp.Value
				}
			}
		}
	}
	return total
}

func httpFloatHistogramCount(t *testing.T, rm metricdata.ResourceMetrics, name string, attrs map[string]string) uint64 {
	t.Helper()
	var total uint64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			histogram, ok := m.Data.(metricdata.Histogram[float64])
			require.True(t, ok)
			for _, dp := range histogram.DataPoints {
				if httpAttributesMatch(dp.Attributes, attrs) {
					total += dp.Count
				}
			}
		}
	}
	return total
}

func httpIntHistogramCount(t *testing.T, rm metricdata.ResourceMetrics, name string, attrs map[string]string) uint64 {
	t.Helper()
	var total uint64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			histogram, ok := m.Data.(metricdata.Histogram[int64])
			require.True(t, ok)
			for _, dp := range histogram.DataPoints {
				if httpAttributesMatch(dp.Attributes, attrs) {
					total += dp.Count
				}
			}
		}
	}
	return total
}

func httpAttributesMatch(set attribute.Set, attrs map[string]string) bool {
	for key, want := range attrs {
		got, found := set.Value(attribute.Key(key))
		if !found || got.AsString() != want {
			return false
		}
	}
	return true
}
