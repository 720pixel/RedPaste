// Package middleware holds the HTTP middleware: request IDs, body-size limits,
// structured logging that redacts secrets, and panic recovery.
package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

type ctxKey int

const requestIDKey ctxKey = iota

// RequestID attaches a random request ID to the context and response header.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newRequestID()
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFromContext returns the request ID, or "" if unset.
func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}

func newRequestID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// BodyLimit caps the request body so a huge upload can't exhaust memory; oversized
// bodies get a 413.
func BodyLimit(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

// statusRecorder remembers the status code and byte count for the log line.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

// Unwrap exposes the wrapped writer so helpers like http.ResponseController and
// the WebSocket hijacker can reach the underlying connection.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// Logger emits one structured record per request. It deliberately never touches
// the query string, URL fragment, body, or sensitive headers; those can carry secrets.
func Logger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			if rec.status == 0 {
				rec.status = http.StatusOK
			}
			log.LogAttrs(r.Context(), slog.LevelInfo, "http_request",
				slog.String("request_id", RequestIDFromContext(r.Context())),
				slog.String("method", r.Method),
				// path only, never the raw query or fragment
				slog.String("path", redactPath(r.URL.Path)),
				slog.Int("status", rec.status),
				slog.Int("bytes", rec.bytes),
				slog.Duration("duration", time.Since(start)),
				slog.String("remote", clientIP(r)),
			)
		})
	}
}

// redactPath keeps the route shape but swaps the slug for a placeholder; the slug
// is effectively a secret and must not hit the logs.
func redactPath(path string) string {
	for _, p := range []string{"/p/", "/s/", "/live/", "/manage/"} {
		if strings.HasPrefix(path, p) {
			return p + "{slug}"
		}
	}
	return path
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// MaxInFlight caps how many requests are served at once. Past the limit it sheds
// load with 503 rather than letting a flood exhaust memory and goroutines. This
// is app-layer defense in depth; a DDoS-capable edge should still front it.
func MaxInFlight(limit int) func(http.Handler) http.Handler {
	if limit <= 0 {
		limit = 256
	}
	sem := make(chan struct{}, limit)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Live WebSockets are long-lived and bounded elsewhere (per-room and
			// per-IP caps); don't let them tie up in-flight slots.
			if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
				next.ServeHTTP(w, r)
				return
			}
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
				next.ServeHTTP(w, r)
			default:
				w.Header().Set("Retry-After", "5")
				http.Error(w, `{"error":"overloaded"}`, http.StatusServiceUnavailable)
			}
		})
	}
}

// Recover turns a panic into a plain 500 instead of leaking internals or dropping the conn.
func Recover(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					log.LogAttrs(r.Context(), slog.LevelError, "panic_recovered",
						slog.String("request_id", RequestIDFromContext(r.Context())),
						slog.Any("panic", rec),
					)
					http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
