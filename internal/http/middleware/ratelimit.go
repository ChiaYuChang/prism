package middleware

import (
	"container/list"
	"net"
	"net/http"
	"strings"
	"sync"

	"golang.org/x/time/rate"
)

// IPLimiter decides whether a request from the given client IP may proceed.
type IPLimiter interface {
	Allow(ip string) bool
}

// NoOpIPLimiter always allows. Suitable as the default when rate limiting is disabled.
type NoOpIPLimiter struct{}

func (NoOpIPLimiter) Allow(string) bool { return true }

// InMemoryIPLimiter is a per-IP token-bucket limiter backed by an LRU map.
//
// Each unique IP gets its own *rate.Limiter (rps requests per second, burst).
// The map is bounded by maxEntries; on overflow the least-recently-used IP is
// evicted. Safe for concurrent use.
type InMemoryIPLimiter struct {
	rps        rate.Limit
	burst      int
	maxEntries int

	mu      sync.Mutex
	entries map[string]*list.Element
	order   *list.List // front = MRU, back = LRU
}

type ipLimiterEntry struct {
	ip      string
	limiter *rate.Limiter
}

// NewInMemoryIPLimiter constructs a limiter. rps and burst must be > 0;
// maxEntries must be > 0.
func NewInMemoryIPLimiter(rps float64, burst, maxEntries int) *InMemoryIPLimiter {
	if rps <= 0 {
		rps = 5
	}
	if burst <= 0 {
		burst = 10
	}
	if maxEntries <= 0 {
		maxEntries = 4096
	}
	return &InMemoryIPLimiter{
		rps:        rate.Limit(rps),
		burst:      burst,
		maxEntries: maxEntries,
		entries:    make(map[string]*list.Element, maxEntries),
		order:      list.New(),
	}
}

func (l *InMemoryIPLimiter) Allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if elem, ok := l.entries[ip]; ok {
		l.order.MoveToFront(elem)
		return elem.Value.(*ipLimiterEntry).limiter.Allow()
	}

	lim := rate.NewLimiter(l.rps, l.burst)
	entry := &ipLimiterEntry{ip: ip, limiter: lim}
	elem := l.order.PushFront(entry)
	l.entries[ip] = elem

	if l.order.Len() > l.maxEntries {
		oldest := l.order.Back()
		if oldest != nil {
			l.order.Remove(oldest)
			delete(l.entries, oldest.Value.(*ipLimiterEntry).ip)
		}
	}
	return lim.Allow()
}

// ClientIP extracts the request's client IP. It honors the leftmost
// X-Forwarded-For entry when present, otherwise falls back to RemoteAddr.
func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if comma := strings.IndexByte(xff, ','); comma >= 0 {
			xff = xff[:comma]
		}
		if ip := strings.TrimSpace(xff); ip != "" {
			return ip
		}
	}
	if ip, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return ip
	}
	return r.RemoteAddr
}

// RateLimit returns a middleware that rejects requests above the limiter's
// budget for the client IP with 429 Too Many Requests.
func RateLimit(limiter IPLimiter) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !limiter.Allow(ClientIP(r)) {
				w.Header().Set("Retry-After", "1")
				http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
