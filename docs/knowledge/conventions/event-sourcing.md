# VXD Event Sourcing — Conventions

## Source of truth
- Append-only file: ~/.vxd/projects/<name>/events.jsonl (fsync'd on every Append)
- Materialized views: SQLite WAL-mode at ~/.vxd/projects/<name>/state.db
- Event log is authoritative; SQLite is rebuildable by replay

## Adding a new event type — step by step

1. Constant in internal/state/events.go:
   ```go
   EventXxxYyy EventType = "XXX_YYY"
   ```

2. Explicit case in internal/state/sqlite.go Project() switch:
   ```go
   case EventXxxYyy:
       return s.projectXxxYyy(payload)  // or `return nil` for informational
   ```
   Without this, the default-WARNING branch silently swallows the event and the
   projection silently lies about state.

3. Wiring test in internal/engine/wiring_test.go:
   ```go
   func TestWiring_XxxYyyEvent_UpdatesProjection(t *testing.T) {
       // construct real store, project the event, assert side effect
   }
   ```

4. Update CLAUDE.md "Critical Events" section if user-facing.

## Payload conventions
- Use a flat map[string]any for payloads
- Common keys: id, repo, class, reason, error
- ULIDs for ids — sortable, unique, ULID timestamps
- DecodePayload returns map[string]any{} on error so handlers don't NPE

## Read-side patterns
- HypothesisBank pattern (autoresearch): pure projection over EventStore.List
  with the type filter. Stateless; rebuilds on every call. OK for low-frequency
  reads; cache if needed.
- Reputation projection (engine): event aggregation feeds dispatcher routing.
- Story state projection (sqlite): updateStoryStatus for state transitions.

## Common pitfalls

### Schema migrations
CREATE TABLE IF NOT EXISTS does NOT add new columns to existing rows. Pair with
ALTER TABLE ADD COLUMN. Backfill from event log if needed (replay all events on
the new schema).

### Event ordering on resume
checkSLA caches per-story start times. On resume, use the LATEST STORY_STARTED,
not the first — otherwise resumed stories get terminated immediately because
"elapsed time" includes the paused window.

### Concurrent projection writes
SQLite WAL allows concurrent readers but not concurrent writers. The Project()
loop is single-threaded by design. Don't add goroutines that call Project()
concurrently without external serialization.
