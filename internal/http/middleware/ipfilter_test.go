package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ChiaYuChang/prism/internal/http/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIPFilter_InvalidConfig(t *testing.T) {
	_, err := middleware.IPFilter([]string{"invalid-ip"}, nil)
	assert.Error(t, err)

	_, err = middleware.IPFilter(nil, []string{"invalid-cidr/"})
	assert.Error(t, err)
}

func TestIPFilter_Whitelist(t *testing.T) {
	mw, err := middleware.IPFilter([]string{"192.168.1.100", "10.0.0.0/24"}, nil)
	require.NoError(t, err)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		name       string
		remoteAddr string
		xff        string
		wantStatus int
	}{
		{
			name:       "exact whitelist match",
			remoteAddr: "192.168.1.100:1234",
			wantStatus: http.StatusOK,
		},
		{
			name:       "subnet whitelist match",
			remoteAddr: "10.0.0.5:1234",
			wantStatus: http.StatusOK,
		},
		{
			name:       "xff exact match",
			remoteAddr: "1.1.1.1:1234",
			xff:        "192.168.1.100",
			wantStatus: http.StatusOK,
		},
		{
			name:       "no match blocked",
			remoteAddr: "192.168.1.101:1234",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "invalid IP blocked",
			remoteAddr: "invalid-ip",
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
		})
	}
}

func TestIPFilter_Blacklist(t *testing.T) {
	mw, err := middleware.IPFilter(nil, []string{"192.168.1.100", "10.0.0.0/24"})
	require.NoError(t, err)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		name       string
		remoteAddr string
		wantStatus int
	}{
		{
			name:       "not blocked allowed",
			remoteAddr: "192.168.1.101:1234",
			wantStatus: http.StatusOK,
		},
		{
			name:       "exact blacklist match blocked",
			remoteAddr: "192.168.1.100:1234",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "subnet blacklist match blocked",
			remoteAddr: "10.0.0.5:1234",
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
		})
	}
}

func TestIPFilter_WhitelistAndBlacklist(t *testing.T) {
	// Whitelist 192.168.1.0/24, but blacklist 192.168.1.100
	mw, err := middleware.IPFilter([]string{"192.168.1.0/24"}, []string{"192.168.1.100"})
	require.NoError(t, err)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		name       string
		remoteAddr string
		wantStatus int
	}{
		{
			name:       "whitelisted not blacklisted allowed",
			remoteAddr: "192.168.1.50:1234",
			wantStatus: http.StatusOK,
		},
		{
			name:       "whitelisted but blacklisted blocked",
			remoteAddr: "192.168.1.100:1234",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "neither whitelisted nor blacklisted blocked",
			remoteAddr: "10.0.0.1:1234",
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
		})
	}
}
