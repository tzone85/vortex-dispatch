# Ephemeral DBs — 3-Wave Test Strategy

**Parent:** `2026-05-21-ephemeral-dbs-master-design.md`
**Applies to:** SP1–SP6 on both VXD and NXD
**Status:** Draft

## Overview

Per requirement: **at least 3 waves of intense testing on both VXD and NXD.** Each wave is its own merge gate.

| Wave | Scope | Triggers | Gating |
|------|-------|----------|--------|
| 1 | Unit + wiring tests inside each SP's PR | every SP PR | merge of that SP blocked until pass |
| 2 | Live VXD pipeline + Ghost cloud | after SP6 lands | go/no-go for VXD enablement on tracked projects |
| 3 | Live NXD pipeline + Docker | after VXD wave 2 passes | go/no-go for NXD enablement |

## Wave 1 — In-PR unit + wiring

Already specified per-SP. Summary of coverage targets:

| Package | Coverage target | Notes |
|---------|----------------|-------|
| `internal/devdb` | ≥85% | foundation, must be rock-solid |
| `internal/devdb/null` | 100% | trivial, deterministic |
| `internal/devdb/ghost` | ≥80% unit | + manual Wave 2 live |
| `internal/devdb/docker` | ≥80% unit + integration | integration via testcontainers |
| `internal/engine` (SP4 deltas) | maintain existing ≥80% | new wiring tests required |
| `internal/preflight` (SP4 deltas) | ≥80% | two new checks |
| `internal/cli` (SP6 deltas) | maintain | new commands tested |

Mandatory wiring tests (added to `internal/engine/wiring_test.go`):
- `TestWiring_StoryDBCreated_UpdatesProjection`
- `TestWiring_StoryDBDeleted_UpdatesProjection`
- `TestWiring_StoryDBFailed_UpdatesProjection`
- `TestWiring_Executor_ProvisionsDB_BeforeSpawn`
- `TestWiring_Executor_NoDevDB_WhenNullProvider`
- `TestWiring_PostExecution_ReleasesDB_OnSuccess`
- `TestWiring_PostExecution_ReleasesDB_OnFailure_*` (KeepDB on/off)
- `TestWiring_Resume_RecoversOrphans`
- `TestWiring_Monitor_SLABreach_ReleasesDB`
- `TestWiring_Strip_VXDDB_NotInPRDiff`
- `TestDocCoverage_CLICommands_DB` (every new `vxd db` cmd in CLAUDE.md)
- `TestDocCoverage_ConfigSections_DevDB_README`

Pass criteria: `go test ./... -count=1 -race` green; coverage thresholds met; no new lint errors.

## Wave 2 — Live VXD + Ghost

Runs against a real `api.ghost.build` free-tier account. Requires `GHOST_API_KEY`.

### Setup
1. Provision a test project: `vxd-ghost-testbed` (created in this wave, lives long-term).
2. Seed a small template DB on Ghost: `vxd-testbed-template` with 3 tables, ~1k rows.
3. Configure `vxd-ghost-testbed/vxd.yaml`:
   ```yaml
   devdb:
     provider: ghost
     template: vxd-testbed-template
     ghost: { api_key_env: GHOST_API_KEY }
   qa:
     fresh_fork_verification: true
     success_criteria:
       - kind: migration_succeeds
         command: "psql $DATABASE_URL -f migrations/latest.sql"
       - kind: sql_query_returns
         sql: "SELECT 1 FROM information_schema.tables WHERE table_name='users'"
         expected_rows: 1
   ```

### Test scripts
Six progressive scenarios, each documented under `docs/test-runs/wave-2-vxd-ghost/`:

| # | Scenario | Expected |
|---|----------|----------|
| 1 | Single story: "add `users.email` column" | story green; DB created+deleted; PR merged |
| 2 | Two parallel stories on same wave | both get separate forks; both deletes clean |
| 3 | Story with failing migration | story fails QA; DB retained per policy; visible in `vxd db list` |
| 4 | Story crashes mid-flight | monitor SLA breach; DB released as paused; orphan-recovery on resume picks it up |
| 5 | Ghost API outage (simulate by revoking key) | preflight CRITICAL; story doesn't dispatch; clear error |
| 6 | High-volume: 10 stories in a wave | provider rate-limits gracefully; all forks succeed within 60s; no orphans |

### Pass criteria
- All six scenarios complete with documented expected outcomes.
- `vxd db list` after each: zero unexpected DBs.
- Ghost free-tier consumption ≤ 5 hours.
- No CRITICAL events that weren't expected.
- All retained DBs match `qa.fresh_fork_verification` + intentionally-failed scenarios.

### Artifacts
- Event log dumps for each scenario.
- Screenshots / TUI captures of dashboard.
- Cost report from `ghost invoice list`.

## Wave 3 — Live NXD + Docker

Same six scenarios as Wave 2, swapped to NXD + Docker provider. Runs on the same hardware (laptop or dev VM) — no cloud dependency.

### Setup
1. Test project: `nxd-docker-testbed` (in NXD).
2. Seed local Docker container with template: `nxd-testbed-template`.
   ```bash
   nxd db template create nxd-testbed-template --from-stdin < testbed.dump
   ```
3. Configure `nxd-docker-testbed/nxd.yaml`:
   ```yaml
   devdb:
     provider: docker
     template: nxd-testbed-template
     docker:
       image: postgres:16
       host_port_range: "5500-5599"
   qa:
     fresh_fork_verification: true
     success_criteria: # same as Wave 2
   ```

### Scenarios (mirror Wave 2)

| # | Scenario | NXD-specific notes |
|---|----------|--------------------|
| 1 | Single story: add column | host container should already be running |
| 2 | Two parallel stories | two SQL forks ≤1s each; no port conflicts |
| 3 | Failing migration | retained DB visible in `nxd db list` and via `docker ps`-less query |
| 4 | Crash mid-flight | orphan recovery; verify no leaked SQL DBs |
| 5 | Docker daemon down (`sudo systemctl stop docker`) | preflight CRITICAL; recoverable when restarted |
| 6 | High-volume: 10 stories | Postgres SQL serialization on `CREATE DATABASE TEMPLATE` measured; if >10s total, file Phase-2 ticket |

### Pass criteria
Same as Wave 2 plus:
- Disk usage of `~/.vxd/devdb-data` (or NXD equivalent) ≤ 10× template size.
- No orphan containers after `nxd db gc`.
- Host port allocator releases all ports.

## Cross-wave invariants (verified by all waves)

These run on every wave:

1. **No DSN in events.jsonl.** Grep verifies only `conn_string_hash` appears, never `postgres://`.
2. **No DB outlives the requirement** (except intentional retains). `vxd db list` empty after every requirement completes.
3. **Worktree cleanup.** `git diff origin/main...HEAD` never shows `.vxd-db/`.
4. **CLAUDE.md preservation.** Existing artifact-protection test still passes — adding `.vxd-db/` to strip-list must not remove anything else.
5. **Idempotent resume.** `vxd resume <req-id>` twice in a row produces no duplicate DBs or events.

## Failure response

If any wave fails:

1. Snapshot state (`events.jsonl`, sqlite, docker state, ghost list).
2. File a finding in `docs/audits/` with reproducer.
3. Determine which SP owns the bug — fix in that SP's follow-up PR.
4. Re-run the entire wave from scratch (clean testbed) before unlocking the next wave.

## Wave artifact storage

```
docs/test-runs/
├── wave-1/                       # auto-generated by go test -v
├── wave-2-vxd-ghost/
│   ├── scenario-1-single-story.md
│   ├── scenario-2-parallel.md
│   ├── ...
│   └── README.md (summary + go/no-go decision)
└── wave-3-nxd-docker/
    └── (same shape)
```

Mirror under NXD repo: `docs/test-runs/wave-3-nxd-docker/`.

## Out of scope for these waves

- Performance benchmarks beyond "10 parallel forks ≤ 60s". Phase-2.
- Adversarial security testing (cross-DB access, role escapes). Phase-2.
- Multi-region Ghost behaviour. Out of scope (single-region free tier).
- Disaster recovery (template loss, Docker volume corruption). Phase-2 runbook.
