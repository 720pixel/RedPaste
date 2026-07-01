// Command redpaste-server serves the embedded frontend and the zero-knowledge
// browser API from one self-contained binary.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/720pRed/redpaste/internal/app"
	"github.com/720pRed/redpaste/internal/cleanup"
	"github.com/720pRed/redpaste/internal/config"
	"github.com/720pRed/redpaste/internal/httpapi"
	"github.com/720pRed/redpaste/internal/live"
	"github.com/720pRed/redpaste/internal/observability"
	"github.com/720pRed/redpaste/internal/paste"
	"github.com/720pRed/redpaste/internal/privateapi"
	"github.com/720pRed/redpaste/internal/ratelimit"
	"github.com/720pRed/redpaste/internal/storefactory"
	"github.com/720pRed/redpaste/web"
)

// maxConnsPerIP caps how many live sockets a single client address can hold, so
// one host can't hog every room slot.
const maxConnsPerIP = 8

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", slog.String("err", err.Error()))
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := observability.NewLogger(cfg.LogLevel)
	log.Info("starting redpaste-server",
		slog.String("env", string(cfg.Env)),
		slog.String("listen", cfg.Listen),
		slog.Bool("sqlite", cfg.UsesSQLite()),
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := storefactory.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	if err := st.Migrate(ctx); err != nil {
		return err
	}

	metrics := observability.NewMetrics()
	assets, err := web.Dist()
	if err != nil {
		return err
	}

	pasteSvc := paste.NewService(st, cfg.ServerPepper, paste.Limits{
		MaxCiphertextBytes: cfg.MaxCiphertextBytes,
		MaxRetention:       cfg.MaxRetention,
		MaxViews:           cfg.MaxViews,
		ShortLinksEnabled:  cfg.EnableShortLinks,
	})
	pasteAPI := httpapi.NewHandler(pasteSvc, cfg.BaseURL, cfg.ShortBase, metricsAdapter{metrics})

	// Live collaboration: optional, gated by config. The hub is in-memory; the
	// store owns durability. Connections tie their lifetime to ctx so they close
	// gracefully on shutdown.
	var liveAPI *httpapi.LiveHandler
	var liveHub *live.Hub
	if cfg.EnableLive {
		liveSvc := live.NewService(st, cfg.ServerPepper, live.Limits{
			MaxParticipants:   cfg.LiveMaxParticipants,
			MaxUpdateBytes:    cfg.LiveMaxUpdateBytes,
			MaxStorageBytes:   cfg.LiveMaxStorageBytes,
			MaxRetention:      cfg.MaxRetention,
			ShortLinksEnabled: cfg.EnableShortLinks,
		})
		liveHub = live.NewHub(cfg.LiveMaxParticipants, maxConnsPerIP)
		liveAPI = httpapi.NewLiveHandler(liveSvc, liveHub, cfg.BaseURL, cfg.ShortBase, ctx, log)
	}

	// Per-IP rate limits. These are the first line against abuse; a hardened edge
	// (Cloudflare/nginx) should sit in front in production. Numbers are tunable.
	rl := ratelimit.NewManager(cfg.TrustedProxyCIDRs)
	rl.Register("create", ratelimit.Rule{Every: 90 * time.Second, Burst: 10})          // ~10 per 15 min
	rl.Register("redeem", ratelimit.Rule{Every: 2 * time.Second, Burst: 30})           // 30/min
	rl.Register("metadata", ratelimit.Rule{Every: 500 * time.Millisecond, Burst: 120}) // 120/min
	rl.Register("livecreate", ratelimit.Rule{Every: 20 * time.Minute, Burst: 3})       // 3/hr
	rl.Register("livejoin", ratelimit.Rule{Every: 20 * time.Second, Burst: 30})        // 30 per 10 min
	rl.Register("api", ratelimit.Rule{Every: 200 * time.Millisecond, Burst: 300})
	go rl.StartSweeper(ctx)

	handler := app.Handler(app.Deps{
		Config:    cfg,
		Store:     st,
		Metrics:   metrics,
		Logger:    log,
		AssetFS:   assets,
		PasteAPI:  pasteAPI,
		LiveAPI:   liveAPI,
		RateLimit: rl,
	})

	// background workers
	worker := cleanup.New(st, log, time.Minute, func(n int64) {
		metrics.CleanupDeleted.Add(float64(n))
	})
	go worker.Run(ctx)
	go observability.ServeMetrics(ctx, cfg.MetricsAddr, metrics, log)

	// Private automation API: disabled by default and, unless this binary was
	// built with -tags privateapi, a no-op stub. Runs on its own listener.
	if cfg.PrivateAPIEnabled {
		go func() {
			err := privateapi.Serve(ctx, privateapi.Deps{
				PasteSvc:     pasteSvc,
				Pepper:       cfg.ServerPepper,
				Listen:       cfg.PrivateAPIListen,
				AllowedCIDRs: cfg.PrivateAPIAllowCIDRs,
				RequireMTLS:  cfg.PrivateAPIRequireMTL,
				TokensFile:   cfg.PrivateAPITokensFile,
				BaseURL:      cfg.BaseURL,
				ShortBase:    cfg.ShortBase,
				Production:   cfg.Env == config.EnvProd,
				DevInsecure:  cfg.DevInsecure,
				Log:          log,
			})
			if err != nil {
				log.Error("private API stopped", slog.String("err", err.Error()))
			}
		}()
	}

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 8 * time.Second, // slow-loris guard
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("http listener starting", slog.String("addr", cfg.Listen))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutdown signal received")
	}

	// Drop live connections before draining HTTP so hijacked sockets don't linger.
	if liveHub != nil {
		liveHub.Close()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// bridges the prometheus metrics to the small interface the API layer wants.
type metricsAdapter struct{ m *observability.Metrics }

func (a metricsAdapter) IncCreated()  { a.m.PastesCreated.Inc() }
func (a metricsAdapter) IncRedeemed() { a.m.Redemptions.Inc() }
