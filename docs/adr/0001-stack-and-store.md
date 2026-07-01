# ADR 0001: Backend stack and storage

**Status:** accepted

## Context

RedPaste needs to be easy to self-host (ideally one binary, no dependencies) but
also production-ready for a real deployment behind a domain.

## Decision

- **Go** with `net/http` + chi for routing. Small, fast, single-binary friendly.
- **Storage behind a `store.Store` interface** with two backends:
  - **SQLite** (`modernc.org/sqlite`, pure Go, no CGO) as the zero-dependency
    default; this is what makes `./redpaste-server` just work.
  - **PostgreSQL** (`pgx/v5`) for multi-instance production, selected when
    `REDPASTE_DATABASE_URL` is set.
- **Frontend embedded** into the binary via `go:embed`, so there's no separate
  asset server and no Node at runtime.

## Consequences

- Two SQL dialects to keep in sync, worth it for the onboarding win.
- The single-writer SQLite connection is fine for a hobby/small deployment;
  Postgres is the answer when you outgrow it.
- Node is a build-time tool only; the shipped artifact is one static binary.
