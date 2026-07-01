// Package app wires up the HTTP router: middleware, health checks, the versioned
// browser API, SEO endpoints, and the embedded SPA with a fallback that keeps URL
// fragments intact on secret routes.
package app

import (
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	"github.com/720pRed/redpaste/internal/config"
	"github.com/720pRed/redpaste/internal/httpapi"
	"github.com/720pRed/redpaste/internal/middleware"
	"github.com/720pRed/redpaste/internal/observability"
	"github.com/720pRed/redpaste/internal/ratelimit"
	"github.com/720pRed/redpaste/internal/securityheaders"
	"github.com/720pRed/redpaste/internal/store"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

// Deps is what Handler needs to build the HTTP handler.
type Deps struct {
	Config    *config.Config
	Store     store.Store
	Metrics   *observability.Metrics
	Logger    *slog.Logger
	AssetFS   fs.FS // embedded frontend (dist)
	PasteAPI  *httpapi.Handler
	LiveAPI   *httpapi.LiveHandler
	RateLimit *ratelimit.Manager
}

// Handler builds the public HTTP handler.
func Handler(d Deps) http.Handler {
	r := chi.NewRouter()

	// Outermost: shed load past the in-flight cap so a flood can't exhaust the box.
	r.Use(middleware.MaxInFlight(d.Config.MaxInFlight))
	r.Use(middleware.RequestID)
	r.Use(middleware.Recover(d.Logger))
	r.Use(securityheaders.Base)
	r.Use(middleware.Logger(d.Logger))
	r.Use(middleware.BodyLimit(d.Config.MaxCiphertextBytes + 65536))
	// Ciphertext is already high-entropy (compressing it is wasted CPU), but the
	// JS/CSS bundle and JSON responses compress well and are the bulk of what a
	// browser fetches on first paint.
	r.Use(chimiddleware.Compress(5, "text/html", "text/css", "application/javascript", "application/json", "text/plain", "image/svg+xml"))

	// health/readiness, never indexed
	r.Get("/healthz", observability.Health())
	r.Get("/readyz", observability.Ready(d.Store))

	// SEO for the public pages only; secret routes are left out here and also carry
	// noindex headers and meta tags.
	r.Get("/robots.txt", robotsTxt(d.Config.BaseURL))
	r.Get("/sitemap.xml", sitemapXML(d.Config.BaseURL))

	r.Route("/api/v1", func(api chi.Router) {
		if d.RateLimit != nil {
			api.Use(rateLimit(d.RateLimit))
		}
		api.Get("/health", observability.Health())
		if d.PasteAPI != nil {
			d.PasteAPI.Mount(api)
		}
		if d.LiveAPI != nil {
			d.LiveAPI.Mount(api)
		}
	})

	// Everything else falls through to the SPA. The client router owns
	// /, /p/{slug}, /live/{slug}, /s/{code}, /manage/{slug}, /diff, /security, /privacy.
	r.NotFound(spaHandler(d.AssetFS))
	r.Get("/*", spaHandler(d.AssetFS))

	return r
}

// spaHandler serves a real asset when one exists, otherwise falls back to
// index.html so client-side routing and URL fragments survive.
func spaHandler(assets fs.FS) http.HandlerFunc {
	fileServer := http.FileServer(http.FS(assets))
	return func(w http.ResponseWriter, r *http.Request) {
		clean := strings.TrimPrefix(r.URL.Path, "/")
		if clean == "" {
			serveIndex(w, r, assets)
			return
		}
		if f, err := assets.Open(clean); err == nil {
			_ = f.Close()
			// Vite fingerprints every filename under assets/ with a content hash, so
			// a given URL's bytes never change — safe to cache for a year and skip
			// revalidation entirely.
			if strings.HasPrefix(clean, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			fileServer.ServeHTTP(w, r)
			return
		}
		serveIndex(w, r, assets)
	}
}

func serveIndex(w http.ResponseWriter, r *http.Request, assets fs.FS) {
	data, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		http.Error(w, "frontend not built", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Non-secret pages only: secret routes already get a harder no-store from
	// securityheaders.Base. This lets the shell be revalidated cheaply (304)
	// instead of re-downloaded, while never serving a stale index after a deploy.
	if _, ok := w.Header()["Cache-Control"]; !ok {
		w.Header().Set("Cache-Control", "no-cache")
	}
	_, _ = w.Write(data)
}
