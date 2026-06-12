package httpclient_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/httpclient"
	"github.com/stretchr/testify/require"
)

func TestIsPublicAddr(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want bool
	}{
		{name: "public IPv4", addr: "8.8.8.8", want: true},
		{name: "public IPv6", addr: "2606:4700:4700::1111", want: true},
		{name: "loopback", addr: "127.0.0.1", want: false},
		{name: "private", addr: "10.0.0.1", want: false},
		{name: "metadata", addr: "169.254.169.254", want: false},
		{name: "cgnat", addr: "100.64.0.1", want: false},
		{name: "documentation", addr: "203.0.113.1", want: false},
		{name: "unique local IPv6", addr: "fc00::1", want: false},
		{name: "IPv6 loopback", addr: "::1", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr := netip.MustParseAddr(tt.addr)
			require.Equal(t, tt.want, httpclient.IsPublicAddr(addr))
		})
	}
}

func TestNewPublicClientBlocksLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	client := httpclient.NewPublicClient(time.Second)
	resp, err := client.Get(srv.URL)
	if resp != nil {
		_ = resp.Body.Close()
	}
	require.Error(t, err)
	require.True(t, errors.Is(err, httpclient.ErrBlockedIP), "error = %v", err)
}

func TestNewPublicClientCanAllowPrivateNetworks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	client := httpclient.NewPublicClient(time.Second, httpclient.WithPrivateNetworks())
	resp, err := client.Get(srv.URL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestNewPublicClientTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	client := httpclient.NewPublicClient(time.Millisecond, httpclient.WithPrivateNetworks())
	resp, err := client.Get(srv.URL)
	if resp != nil {
		_ = resp.Body.Close()
	}
	require.Error(t, err)
	require.ErrorContains(t, err, "Client.Timeout exceeded")
}
