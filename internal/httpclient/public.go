package httpclient

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"time"
)

const DefaultTimeout = 30 * time.Second

var ErrBlockedIP = errors.New("blocked non-public IP")

type config struct {
	allowPrivate bool
}

// Option configures outbound HTTP clients created by this package.
type Option func(*config)

// WithPrivateNetworks permits private, loopback, and otherwise non-public
// destinations. Use only for explicit local fixture or test transports.
func WithPrivateNetworks() Option {
	return func(c *config) {
		c.allowPrivate = true
	}
}

// NewPublicClient returns an HTTP client with a timeout and a transport that
// refuses to dial non-public IP addresses after DNS resolution.
func NewPublicClient(timeout time.Duration, opts ...Option) *http.Client {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: NewPublicTransport(nil, opts...),
	}
}

// NewPublicTransport clones base and installs a public-network-only dialer.
func NewPublicTransport(base *http.Transport, opts ...Option) *http.Transport {
	cfg := config{}
	for _, opt := range opts {
		opt(&cfg)
	}

	if base == nil {
		if defaultTransport, ok := http.DefaultTransport.(*http.Transport); ok {
			base = defaultTransport
		} else {
			base = &http.Transport{}
		}
	}

	transport := base.Clone()
	if cfg.allowPrivate {
		return transport
	}

	dialer := &publicDialer{}
	transport.DialContext = dialer.DialContext
	return transport
}

type publicDialer struct {
	Resolver *net.Resolver
	Dialer   *net.Dialer
}

func (d *publicDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("split dial address %q: %w", address, err)
	}

	resolver := d.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	dialer := d.Dialer
	if dialer == nil {
		dialer = &net.Dialer{}
	}

	ips, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", host, err)
	}

	var lastDialErr error
	for _, ip := range ips {
		addr, ok := netip.AddrFromSlice(ip.IP)
		if !ok || !IsPublicAddr(addr) || !networkAllowsIP(network, addr) {
			continue
		}

		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(addr.String(), port))
		if err == nil {
			return conn, nil
		}
		lastDialErr = err
	}
	if lastDialErr != nil {
		return nil, lastDialErr
	}

	return nil, fmt.Errorf("%w: %s", ErrBlockedIP, host)
}

// IsPublicAddr reports whether addr is routable on the public internet.
func IsPublicAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
		return false
	}

	for _, prefix := range blockedPrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

func networkAllowsIP(network string, addr netip.Addr) bool {
	switch network {
	case "tcp4":
		return addr.Is4()
	case "tcp6":
		return addr.Is6()
	default:
		return true
	}
}

var blockedPrefixes = []netip.Prefix{
	mustPrefix("0.0.0.0/8"),
	mustPrefix("10.0.0.0/8"),
	mustPrefix("100.64.0.0/10"),
	mustPrefix("127.0.0.0/8"),
	mustPrefix("169.254.0.0/16"),
	mustPrefix("172.16.0.0/12"),
	mustPrefix("192.0.0.0/24"),
	mustPrefix("192.0.2.0/24"),
	mustPrefix("192.168.0.0/16"),
	mustPrefix("198.18.0.0/15"),
	mustPrefix("198.51.100.0/24"),
	mustPrefix("203.0.113.0/24"),
	mustPrefix("224.0.0.0/4"),
	mustPrefix("240.0.0.0/4"),
	mustPrefix("::/128"),
	mustPrefix("::1/128"),
	mustPrefix("64:ff9b:1::/48"),
	mustPrefix("100::/64"),
	mustPrefix("2001:2::/48"),
	mustPrefix("2001:db8::/32"),
	mustPrefix("fc00::/7"),
	mustPrefix("fe80::/10"),
}

func mustPrefix(s string) netip.Prefix {
	prefix, err := netip.ParsePrefix(s)
	if err != nil {
		panic(err)
	}
	return prefix
}
