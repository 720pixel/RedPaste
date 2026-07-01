// Package storefactory picks and opens the configured backend. It sits outside
// the store package so the backends can import store's types without a cycle.
package storefactory

import (
	"context"

	"github.com/720pRed/redpaste/internal/config"
	"github.com/720pRed/redpaste/internal/postgres"
	"github.com/720pRed/redpaste/internal/sqlite"
	"github.com/720pRed/redpaste/internal/store"
)

// Open opens Postgres when a database URL is set, else the embedded SQLite default.
func Open(ctx context.Context, cfg *config.Config) (store.Store, error) {
	if cfg.UsesSQLite() {
		return sqlite.Open(ctx, cfg.SQLitePath)
	}
	return postgres.Open(ctx, cfg.DatabaseURL)
}
