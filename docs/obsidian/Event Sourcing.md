---
tags: [architecture, state]
---

# Event Sourcing

VXD's source of truth is an **append-only event log**; queryable state is a
**derived projection**.

## Two stores
- **Event store** — `events.jsonl` (append-only, fsync'd) in
  `internal/state/filestore.go`. This is authoritative.
- **Projection** — SQLite with WAL mode in `internal/state/sqlite.go`. A
  materialized cache for queries (`GetStory`, `ListStories`, …).

Every state change is an `Event` (`internal/state/events.go`) appended to the
log and then applied to the projection via `Project(evt)`.

## The golden rule
> New event types **must** be handled in `sqlite.go Project()`'s switch — the
> `default` case only logs a warning and otherwise ignores them. Always add a
> wiring test (see [[Testing Conventions]]).

## Rebuildability (recovery)
The projection is a cache and **must be rebuildable from the log**. This is
implemented by `SQLiteStore.Rebuild(EventStore)`: it clears the projection
tables and replays every event in order inside a transaction. Operators run it
via `vxd rebuild`.

Use it when the projection diverges from the log — e.g. after a crash between a
durable `Append` and its `Project` call. Projections are **idempotent**
(`INSERT OR IGNORE`), so replay is safe.

## Concurrency
- SQLite is opened with `_busy_timeout=5000` so concurrent pipeline writers wait
  for the lock instead of failing with `SQLITE_BUSY`.
- The `STORY_STARTED` guard (`guardedStartStory`) is a single atomic conditional
  `UPDATE`, closing a prior check-then-act race.

## Notable events
- `STORY_ESCALATED`, `STORY_SPLIT`, `STORY_REWRITTEN` — see [[Escalation Chain]].
- `STORY_SLA_BREACHED` — per-complexity duration limit exceeded.
- `STORY_CONFLICT_*` — see [[Conflict Resolution]].
- `COORDINATOR_STOPPED` — autoresearch loop hit a budget/circuit-breaker stop.
