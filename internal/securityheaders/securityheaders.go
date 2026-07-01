// Package securityheaders sets the HTTP security headers: a strict CSP plus
// per-route no-index/no-cache controls on secret routes.
package securityheaders

import (
	"net/http"
	"strings"
)

// Strict CSP. 'wasm-unsafe-eval' is only here because the Argon2id WASM build
// (passphrase key derivation) needs it.
const contentSecurityPolicy = "default-src 'none'; " +
	"base-uri 'none'; " +
	"form-action 'self'; " +
	"frame-ancestors 'none'; " +
	"script-src 'self' 'wasm-unsafe-eval'; " +
	// 'unsafe-inline' for styles only: Radix UI writes inline style attributes for
	// positioning. Scripts stay locked to 'self', so this is a small surface.
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob:; " +
	"font-src 'self'; " +
	"connect-src 'self' wss: stun: turn:; " +
	"worker-src 'self' blob:; " +
	"manifest-src 'self'; " +
	"object-src 'none'; " +
	"media-src 'none'; " +
	"upgrade-insecure-requests"

const permissionsPolicy = "camera=(), microphone=(), geolocation=(), payment=(), usb=(), serial=(), bluetooth=()"

// X-Robots-Tag for secret routes.
const robotsSecret = "noindex, nofollow, noarchive, nosnippet, noimageindex"

// Prefixes that must never be indexed or cached.
var secretPrefixes = []string{"/p/", "/live/", "/s/", "/manage/", "/api/"}

// Base sets the always-on security headers on every response.
func Base(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Permissions-Policy", permissionsPolicy)
		// HSTS only matters over TLS, but it's harmless to always send and hardened
		// deployments behind a TLS terminator expect it.
		h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")

		if isSecretRoute(r.URL.Path) {
			h.Set("X-Robots-Tag", robotsSecret)
			h.Set("Cache-Control", "no-store, private, max-age=0")
			h.Set("Pragma", "no-cache")
		}
		next.ServeHTTP(w, r)
	})
}

func isSecretRoute(path string) bool {
	for _, p := range secretPrefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// IsSecretRoute exposes isSecretRoute to tests and other middleware.
func IsSecretRoute(path string) bool { return isSecretRoute(path) }
