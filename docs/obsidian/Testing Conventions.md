---
tags: [quality, testing]
---

# Testing Conventions

VXD treats tests as load-bearing: features must be proven **activated**, not just
present.

## Principles
- **TDD** — write tests first.
- **Pure functions for logic, thin adapters for I/O** — e.g. `Adapter.Prepare`
  is pure and unit-tested; `Runner.Run` is the I/O edge. See [[Runtime and Adapters]].
- **Internal test package preferred** (`package engine`) over external
  (`engine_test`) when white-box access helps.

## Wiring tests
`internal/engine/wiring_test.go` (30+ tests) verify that features are wired into
the pipeline — most importantly that **every emitted event type is handled in
`sqlite.go Project()`**. The `default` case silently ignores unknown events, so a
wiring test is mandatory when adding an event type. See [[Event Sourcing]].

## Hermetic tests
Tests must not depend on host configuration. Concretely:
- Git-using tests must not depend on the runner's `commit.gpgsign` — CI sets
  `commit.gpgsign false`.
- "Invalid path" assertions use a file-as-parent-directory (`ENOTDIR`) rather
  than `/nonexistent`, which **root** could create.
- `detectExistingCodebase` no longer treats a git error as "greenfield" — it
  falls through to the source-file heuristic.

## CI
`go test ./...` runs **every** package under `-race` on each push (the old
`internal/cli` / `internal/improve` exclusions were removed after their
non-hermetic deps were fixed). An advisory `govulncheck` job scans dependencies.
See [[Security Audit 2026-06]].

## Related
- [[Documentation Enforcement]] — docs are tested too.
