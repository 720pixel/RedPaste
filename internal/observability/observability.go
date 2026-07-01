// Package observability holds the liveness/readiness endpoints and Prometheus
// metrics. Metrics only ever go out on a private listener.
package observability

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Pinger is all the readiness check needs from the store.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Metrics holds the Prometheus collectors. Slugs must never appear as label
// values; that would leak secrets into metrics.
type Metrics struct {
	Registry        *prometheus.Registry
	RequestsTotal   *prometheus.CounterVec
	RequestDuration *prometheus.HistogramVec
	PastesCreated   prometheus.Counter
	Redemptions     prometheus.Counter
	CleanupDeleted  prometheus.Counter
}

// NewMetrics builds and registers the collectors.
func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()
	m := &Metrics{
		Registry: reg,
		RequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "redpaste_http_requests_total",
			Help: "Total HTTP requests by route template and status.",
		}, []string{"route", "status"}),
		RequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "redpaste_http_request_duration_seconds",
			Help:    "HTTP request duration by route template.",
			Buckets: prometheus.DefBuckets,
		}, []string{"route"}),
		PastesCreated: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "redpaste_pastes_created_total", Help: "Total pastes created.",
		}),
		Redemptions: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "redpaste_redemptions_total", Help: "Total successful redemptions.",
		}),
		CleanupDeleted: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "redpaste_cleanup_deleted_total", Help: "Total expired rows deleted.",
		}),
	}
	reg.MustRegister(m.RequestsTotal, m.RequestDuration, m.PastesCreated, m.Redemptions, m.CleanupDeleted)
	return m
}

// Health handles GET /healthz: liveness, 200 as long as we're serving.
func Health() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}
}

// Ready handles GET /readyz: 200 only if the store answers a ping.
func Ready(p Pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		w.Header().Set("Content-Type", "application/json")
		if err := p.Ping(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"unavailable"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	}
}

// ServeMetrics runs the private metrics listener on addr until ctx is cancelled.
// Empty addr means don't bother.
func ServeMetrics(ctx context.Context, addr string, m *Metrics, log *slog.Logger) {
	if addr == "" {
		return
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{}))
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	log.Info("metrics listener starting", slog.String("addr", addr))
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Error("metrics listener failed", slog.String("err", err.Error()))
	}
}

// NewLogger builds the app-wide structured JSON logger.
func NewLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
}
