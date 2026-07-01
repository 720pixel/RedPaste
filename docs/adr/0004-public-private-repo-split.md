# 4. Public/private repo split via export script

## Status

Accepted.

## Context

This repo is the working development tree and stays private. A second,
publicly-published repo needs to track it closely — installable, updatable —
while never containing operator-only code: anything gitignored per the
pattern established in ADR 0003 (private-api-isolation), plus any future
operator-only feature built the same way.

Most files never diverge between the two trees. A small number legitimately
do: files on the hot path (request handling, schema, the core create/view UI)
sometimes need a private-only branch that a build tag can't cleanly express —
build tags gate whole files, not "one extra field in a struct" or "one extra
UI toggle."

## Decision

- The public repo lives in a sibling folder, seeded and kept in sync by
  `scripts/export-public.sh`, not by shared git history (no subtree/submodule).
- The script mirrors the current working tree via `rsync --filter=':- .gitignore'`,
  which makes rsync itself gitignore-aware — the exact same exclusion rules
  that already govern what's private (ADR 0003) apply automatically, with
  nothing new to hand-maintain or let drift.
- For the handful of files that need to diverge on the hot path rather than
  bolt on cleanly: `_public-overrides/` holds the public-safe version of just
  those files, tracked in *this* repo's own history. The export script copies
  the working tree, then overlays `_public-overrides/` on top for that specific
  path list. Every other file flows through untouched.
- `_public-overrides/` and `scripts/` themselves are excluded from the export —
  they're private tooling, meaningless without this repo's context.
- The script ends with a standalone smoke build (`make build`, no tags) inside
  the exported folder, so a broken export is caught before it's ever committed
  or pushed from there.
- Pushing from the public folder to its remote is a manual, separate step —
  the script only prepares and locally commits.

## Consequences

- Updating the public repo is "run the script, review the diff, push" — no
  manual re-diffing of the whole tree.
- The override set should stay small. If it grows large, that's a signal the
  diverging feature should be isolated behind a build tag instead (see ADR 0003)
  rather than living in `_public-overrides/`.
