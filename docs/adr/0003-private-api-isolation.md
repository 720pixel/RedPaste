# ADR 0003: Private API isolation and the open-source split

**Status:** accepted

## Context

RedPaste is going open source, but the private automation API (for the operator's
own scripts) shouldn't ship in the public repo, not the code, not the docs.

## Decision

- The private API lives in `internal/privateapi`, guarded by a `privateapi` build
  tag. A tiny no-op stub with no build tag keeps the public tree compiling; its
  `Enabled()` always returns false.
- `.gitignore` excludes `internal/privateapi/`, `docs/private-api/`, and the token
  tooling in `cmd/redpaste-admin/tokens*.go`. The public repo builds and passes CI
  without them.
- The private API runs on its own listener (loopback by default), is disabled by
  default, accepts ciphertext only, and is never reachable through the public
  ingress.

## Consequences

- Contributors to the public repo can't build the private listener; intended.
- The operator keeps the private pieces in a separate location and drops them in
  with the build tag when deploying.
- Nothing about the private API is discoverable from the public site or docs.
