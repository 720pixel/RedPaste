package ratelimit

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"
)

// Manager owns the per-category limiters and works out the real client IP,
// honoring X-Forwarded-For only from configured trusted proxies.
type Manager struct {
	trusted  []*net.IPNet
	limiters map[string]*Limiter
}

func NewManager(trustedCIDRs []string) *Manager {
	m := &Manager{limiters: map[string]*Limiter{}}
	for _, c := range trustedCIDRs {
		if _, n, err := net.ParseCIDR(strings.TrimSpace(c)); err == nil {
			m.trusted = append(m.trusted, n)
		}
	}
	return m
}

// Register wires a named category to a rule. Call during setup, before serving.
func (m *Manager) Register(name string, rule Rule) {
	m.limiters[name] = New(rule)
}

// Allow spends a token for (category, ip). Unknown categories are allowed.
func (m *Manager) Allow(category, ip string) bool {
	l := m.limiters[category]
	if l == nil {
		return true
	}
	return l.Allow(category + ":" + ip)
}

// ClientIP resolves the caller's address. If the direct peer is a trusted proxy
// we walk X-Forwarded-For from the right and take the first address that isn't
// itself a trusted proxy; that's the closest hop we can't forge.
func (m *Manager) ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if !m.isTrusted(host) {
		return host
	}
	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return host
	}
	parts := strings.Split(xff, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		ip := strings.TrimSpace(parts[i])
		if ip != "" && !m.isTrusted(ip) {
			return ip
		}
	}
	return host
}

func (m *Manager) isTrusted(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	// loopback is always considered trusted (single-node dev / same-host proxy)
	if ip.IsLoopback() {
		return true
	}
	for _, n := range m.trusted {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// Limit returns middleware enforcing the given category for the resolved IP.
func (m *Manager) Limit(category string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !m.Allow(category, m.ClientIP(r)) {
				w.Header().Set("Retry-After", "60")
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":"rate_limited"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// StartSweeper periodically evicts idle buckets until ctx is cancelled.
func (m *Manager) StartSweeper(ctx context.Context) {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for _, l := range m.limiters {
				l.sweep(30 * time.Minute)
			}
		}
	}
}
