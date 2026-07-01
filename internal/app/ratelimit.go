package app

import (
	"net/http"
	"strings"

	"github.com/720pRed/redpaste/internal/ratelimit"
)

// Category names must match what main.go registers.
const (
	catCreate     = "create"
	catRedeem     = "redeem"
	catMetadata   = "metadata"
	catLiveCreate = "livecreate"
	catLiveJoin   = "livejoin"
	catAPI        = "api"
)

// categorize maps a request to a rate-limit bucket by method + path shape. The
// tighter categories guard the expensive/abusable actions; everything else falls
// back to a coarse per-IP cap.
func categorize(r *http.Request) string {
	p := r.URL.Path
	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(p, "/pastes"):
		return catCreate
	case r.Method == http.MethodPost && strings.HasSuffix(p, "/live"):
		return catLiveCreate
	case r.Method == http.MethodPost && strings.HasSuffix(p, "/redeem"):
		return catRedeem
	case strings.HasSuffix(p, "/signal"):
		return catLiveJoin
	case r.Method == http.MethodGet && (strings.Contains(p, "/pastes/") || strings.Contains(p, "/shortlinks/")):
		return catMetadata
	default:
		return catAPI
	}
}

// rateLimit applies the categorized per-IP limit to the /api/v1 group.
func rateLimit(m *ratelimit.Manager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cat := categorize(r)
			if !m.Allow(cat, m.ClientIP(r)) {
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
