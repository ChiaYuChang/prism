package client_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	httpclient "github.com/ChiaYuChang/prism/internal/http/client"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
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
	var netErr net.Error
	require.ErrorAs(t, err, &netErr)
	require.True(t, netErr.Timeout())
}

func TestNewPublicClientTracesOutboundRequests(t *testing.T) {
	previousProvider := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(previousProvider) })

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	otel.SetTracerProvider(tp)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	client := httpclient.NewPublicClient(time.Second, httpclient.WithPrivateNetworks())
	resp, err := client.Get(srv.URL)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	require.Equal(t, "HTTP GET", spans[0].Name)
}
