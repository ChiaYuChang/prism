package middleware

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

// IPFilter returns a Middleware that filters requests by client IP against
// a whitelist (allowedIPs) and a blacklist (blockedIPs).
// Supports individual IPs (e.g., "192.168.1.1") and CIDR ranges (e.g., "10.0.0.0/8").
// If the whitelist is non-empty, only matching IPs are allowed.
// If the blacklist is non-empty, any matching IP is blocked.
func IPFilter(allowedIPs, blockedIPs []string) (Middleware, error) {
	allowed, err := parseIPFilterList(allowedIPs)
	if err != nil {
		return nil, fmt.Errorf("allowed IPs: %w", err)
	}
	blocked, err := parseIPFilterList(blockedIPs)
	if err != nil {
		return nil, fmt.Errorf("blocked IPs: %w", err)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			clientIPStr := ClientIP(r)
			clientIP := net.ParseIP(clientIPStr)
			if clientIP == nil {
				http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
				return
			}

			// Blacklist check (deny if matched)
			if len(blocked) > 0 && matchIP(clientIP, blocked) {
				http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
				return
			}

			// Whitelist check (deny if not matched)
			if len(allowed) > 0 && !matchIP(clientIP, allowed) {
				http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}, nil
}

func parseIPFilterList(list []string) ([]*net.IPNet, error) {
	var nets []*net.IPNet
	for _, item := range list {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if !strings.Contains(item, "/") {
			ip := net.ParseIP(item)
			if ip == nil {
				return nil, fmt.Errorf("invalid IP: %q", item)
			}
			var mask net.IPMask
			if ip.To4() != nil {
				mask = net.CIDRMask(32, 32)
			} else {
				mask = net.CIDRMask(128, 128)
			}
			nets = append(nets, &net.IPNet{IP: ip, Mask: mask})
		} else {
			_, ipNet, err := net.ParseCIDR(item)
			if err != nil {
				return nil, fmt.Errorf("invalid CIDR %q: %w", item, err)
			}
			nets = append(nets, ipNet)
		}
	}
	return nets, nil
}

func matchIP(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
