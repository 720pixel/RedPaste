// Package cleanup is the background sweeper that deletes expired/revoked data, so
// retention holds even for rows nobody ever requests again.
package cleanup

import (
	"context"
	"log/slog"
	"time"

	"github.com/720pRed/redpaste/internal/store"
)

// Worker periodically drops expired rows from the store.
type Worker struct {
	store    store.Store
	log      *slog.Logger
	interval time.Duration
	onDelete func(int64)
}

// New builds a cleanup worker. onDelete may be nil.
func New(s store.Store, log *slog.Logger, interval time.Duration, onDelete func(int64)) *Worker {
	if interval <= 0 {
		interval = time.Minute
	}
	return &Worker{store: s, log: log, interval: interval, onDelete: onDelete}
}

// Run sweeps on every tick and blocks until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	w.sweep(ctx) // sweep once at startup
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.sweep(ctx)
		}
	}
}

func (w *Worker) sweep(ctx context.Context) {
	sweepCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	n, err := w.store.DeleteExpired(sweepCtx, time.Now().UTC())
	if err != nil {
		w.log.Error("cleanup sweep failed", slog.String("err", err.Error()))
		return
	}
	if n > 0 {
		w.log.Info("cleanup removed expired rows", slog.Int64("count", n))
		if w.onDelete != nil {
			w.onDelete(n)
		}
	}
}
