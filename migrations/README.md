# Migrations

Right now each backend keeps its schema inline (`internal/sqlite` and
`internal/postgres`) and applies it idempotently on startup via `Migrate()`. For
the current single-table schema that's simpler than a migration runner and keeps
the two dialects visibly side by side.

When the schema starts changing in incompatible ways (post Phase 1), versioned
migration files land here and `Migrate()` switches to applying them in order.
