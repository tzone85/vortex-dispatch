# Ephemeral Databases for Agents — Master Design

**Date:** 2026-05-21
**Status:** Draft
**Author:** Thando Mini (via Claude Opus 4.7)
**Inspired by:** [ghost.build](https://ghost.build) — "Postgres built for agents"

## TL;DR

Give every VXD/NXD story its own throwaway Postgres database. The agent gets a connection string in its worktree, runs migrations / writes data / breaks things, and the database dies when the story finishes. Same SQL. Different lifecycle.

Two backends behind one interface:
- **VXD → Ghost** (cloud, via `api.ghost.build/v0`).
- **NXD → Docker** (local Postgres + template DBs).

Same agent-facing contract, different infrastructure.

## Why now

VXD already isolates files via git worktrees. It does not isolate databases. Stories touching client Postgres (sample-api, sampleapp) share one dev DB → "agent stepped on another agent's work" failures, lost work on bad migrations, no safe place to test destructive SQL.

ghost.build exists because every agent team had built the same workaround. We have built the file-side of that workaround (worktrees). The DB-side is the gap.

## Goals

1. **Agent-first.** Default behaviour is auto-provision. The agent should not need to think about DB lifecycle — VXD does it.
2. **Human-visible.** Dashboard panel, CLI commands, metrics, audit trail. Humans can see what every story has, connect with psql, postmortem failed stories.
3. **Backend-pluggable.** Same `devdb.Provider` interface for Ghost (cloud) and Docker (offline). NXD stays offline-first.
4. **Cost-controlled.** Per-requirement DB-hours tracked. Auto-delete on story completion. Configurable retention for failed stories.
5. **Opt-in per project.** Default disabled (`null` provider). Projects that need it opt in via `vxd.yaml`.

## Non-goals

- Replacing VXD's SQLite state store. VXD's event log + projections stay as-is.
- Cross-story DB sharing within a wave (each story gets its own fork).
- Migration framework (we use whatever the project uses — Flyway, golang-migrate, alembic, etc.).
- Production database management. This is dev/staging-only.

## Architecture overview

```
                ┌──────────────────────────────────────────────┐
                │              Story lifecycle                  │
                │  STORY_ASSIGNED → STORY_DB_CREATED →          │
                │  STORY_STARTED → ... → STORY_COMPLETED →      │
                │  STORY_DB_DELETED                             │
                └──────────────────────────────────────────────┘
                                    │
                                    ▼
            ┌───────────────────────────────────────────────┐
            │           internal/devdb (SP1)                 │
            │  Provider interface: Create/Fork/Delete/...    │
            │  Lifecycle hooks, env injection contract       │
            └───────────────────────────────────────────────┘
                    │             │              │
                    ▼             ▼              ▼
        ┌────────────────┐ ┌──────────────┐ ┌──────────────┐
        │ ghost.Provider │ │docker.Provider│ │null.Provider │
        │   (SP2)        │ │   (SP3)       │ │              │
        │ cloud, VXD     │ │ local, VXD+NXD│ │ no-op, tests │
        └────────────────┘ └──────────────┘ └──────────────┘
                    │             │
                    ▼             ▼
            api.ghost.build    docker daemon
            HTTP API           pg_dump templates
```

## Sub-project decomposition

| SP | Title | Spec file | Depends on |
|----|-------|-----------|-----------|
| 1 | devdb foundation | `2026-05-21-ephemeral-dbs-sp1-devdb-foundation.md` | — |
| 2 | Ghost provider | `2026-05-21-ephemeral-dbs-sp2-ghost-provider.md` | SP1 |
| 3 | Docker provider | `2026-05-21-ephemeral-dbs-sp3-docker-provider.md` | SP1 |
| 4 | Per-story executor wiring | `2026-05-21-ephemeral-dbs-sp4-per-story-wiring.md` | SP1, SP2 \|\| SP3 |
| 5 | QA migration gate | `2026-05-21-ephemeral-dbs-sp5-qa-gate.md` | SP1, SP4 |
| 6 | Human visibility | `2026-05-21-ephemeral-dbs-sp6-visibility.md` | SP1, SP4 |

Test strategy in `2026-05-21-ephemeral-dbs-test-strategy.md` overlays all six.

Each sub-project gets its own PR. Order: SP1 → (SP2 + SP3 parallel) → SP4 → (SP5 + SP6 parallel).

## Cross-cutting decisions

### Config shape

Per-project `vxd.yaml` (and `nxd.yaml`) gains a `devdb` block:

```yaml
devdb:
  provider: ghost          # ghost | docker | null (default: null)
  template: "prod-snapshot" # source DB to fork from
  on_failure:
    keep_db: true          # don't auto-delete failed-story DBs
    retain_hours: 24       # GC after N hours
  ghost:
    api_key_env: GHOST_API_KEY
    space_id: ""           # auto from /spaces if blank
  docker:
    image: "postgres:16"
    template_volume: "/var/lib/postgresql/template"
    network: "vxd-devdb"
```

Config validation (existing `config.Validate`) enforces:
- `provider` must be one of `{ghost, docker, null}`.
- `ghost` provider requires `ghost.api_key_env` set and env var present.
- `docker` provider requires Docker daemon reachable.
- `template` is required when provider ≠ null.

### Event sourcing

Three new events (handled in `internal/state/events.go` and `internal/state/sqlite.go Project()`):

| Event | When | Payload |
|-------|------|---------|
| `STORY_DB_CREATED` | After successful `Provider.Fork()`, before `STORY_STARTED` | `db_id`, `db_name`, `provider`, `template`, `connection_string_hash` (NOT the raw conn string — hash for correlation) |
| `STORY_DB_FAILED` | On provider error | `db_id` (optional), `error`, `attempt` |
| `STORY_DB_DELETED` | After `Provider.Delete()` succeeds | `db_id`, `duration_seconds`, `bytes_used` (best-effort) |

Wiring tests (in `internal/engine/wiring_test.go`) verify each event type updates the SQLite projection. **Critical: add wiring tests now** — previous bugs (STORY_RESET) were caused by events landing in JSONL but not the projection.

### Connection-string injection

Per-story worktree gets a new directory `.vxd-db/` containing:

```
.vxd-db/
├── connect.env          # DATABASE_URL=postgres://...
├── README.md            # agent-readable: "you have a Postgres, here's the contract"
└── psql.sh              # quick-connect helper
```

`.vxd-db/` is added to `stripVXDArtifactsFromBranch` cleanup so it never reaches the PR diff (same pattern as `.vxd-prompts/`).

### Failure modes and recovery

| Failure | Behaviour |
|---------|-----------|
| `Provider.Fork()` fails | Emit `STORY_DB_FAILED`. Retry once with backoff. On second failure, fall back to `null` provider for this story and emit a warning event. Story still runs (degraded). |
| Story crashes mid-flight | Monitor's stuck-threshold escalation runs as normal. DB is *not* deleted until escalation resolves (success → delete; pause → keep until manual or GC). |
| VXD itself crashes | On `vxd resume`, recovery iterates orphaned DBs (provider lists by `vxd-story-<id>` naming convention) and either re-binds them to in-progress stories or deletes them. |
| Ghost API down | Health-check at preflight (`CheckGhostReachable`). If unreachable, surface CRITICAL preflight error; offer `--skip-devdb` to bypass. |
| Docker daemon down | Same pattern via `CheckDockerReachable`. |

### Naming convention

DB names follow: `vxd-<project>-<story-id>` where `<story-id>` is the full VXD story identifier (existing format: 8 chars of reqID + `-` + LLM-ID, e.g. `a8cbef1f-3a`). Example: `vxd-sample-api-a8cbef1f-3a`. NXD uses prefix `nxd-`. Enables provider-side `list-by-prefix` for orphan recovery.

### Secret handling

- Ghost API key from `GHOST_API_KEY` env (or whatever `api_key_env` names). Never logged. Hashed in events.
- Docker DB passwords generated per-DB via `crypto/rand`. Stored only in the worktree's `.vxd-db/connect.env` (file mode 0600). Never committed.
- `internal/sanitize` ValidateShellArg used for any value flowing into shell commands.

### Cost model

`vxd metrics` gains a DB section:

```
Databases:
  Total DB-hours this requirement: 4.2
  Active: 2
  Deleted: 7
  Ghost free-tier remaining: 95.8 hrs / 100 hrs
  Estimated cost (Ghost dedicated, post-free): $0
```

NXD shows storage usage instead of hours (no billing).

## Agent-first vs human-visible

Two distinct interaction modes, both shipped:

| Surface | Audience | Mechanism |
|---------|----------|-----------|
| Auto-provision per story | VXD-orchestrated agents | Executor calls `Provider.Fork()` before spawn; writes `.vxd-db/` into worktree; emits events |
| MCP tools in worktree | Same agents (for ad-hoc ops) | `ghost mcp install` for Ghost; NXD installs a thin `nxd-db` MCP wrapping local provider |
| Dashboard panel | Humans | TUI + web: per-story row shows DB status, age, connect command |
| `vxd db list/connect/logs/delete` | Humans | Wraps provider; `vxd db connect <story-id>` opens psql |
| `vxd metrics` DB section | Humans / billing | DB-hours + cost rolled up per requirement |
| `docs/use-cases/ephemeral-dbs.md` | Humans / prospects | "Shines where / breaks where" matrix |

Why MCP *and* auto-provision? Auto-provision handles the 90% case (story needs a DB, gets one). MCP handles the 10% (agent decides mid-flight it wants to fork its own DB to test something) and the standalone-Claude-Code case (humans using Claude without VXD pipeline).

## NXD parity

NXD is offline-first. Ghost is cloud-only. Resolution:

- NXD ships **only** the Docker provider.
- NXD's `nxd-db` MCP wraps the Docker provider (not Ghost) so agents have the same tool names (`create`, `fork`, `delete`, `sql`, `schema`).
- VXD's `internal/devdb` package is the source of truth; NXD's `internal/devdb` is kept in sync via a port script (we already do this for `engine`, `state`, etc.).
- Test wave 3 runs the full NXD pipeline against Docker provider.

## Use-case matrix (where it shines / where it doesn't)

| Use case | Verdict | Why |
|----------|---------|-----|
| Per-story migration testing | ✅ Killer | Fork prod-snapshot → run migration → discard. Catches breakages early. |
| Schema-aware code generation | ✅ Strong | `ghost schema` (or local equivalent) gives the agent exact table structure. No more "guessed column name" bugs. |
| Destructive SQL testing | ✅ Strong | Agent can `DROP TABLE` freely; blast radius = one story. |
| Multi-agent experimentation | ✅ Strong | Two agents trying different approaches each get a fork. Compare results, keep winner. |
| Long-running data pipelines | ⚠️ Mixed | Per-story DB dies on completion; pipelines spanning multiple stories need requirement-scoped DB (Phase-2 feature). |
| Stateful integration tests | ⚠️ Mixed | Works, but cold-start latency (~1-2s Ghost, ~3-5s Docker) adds to every story start. |
| Pure-frontend stories | ❌ Skip | No DB needed; `null` provider is the right choice. |
| Production migrations | ❌ Don't | Dev/staging only. We do NOT touch prod databases. |
| Offline development | Ghost ❌ / Docker ✅ | Ghost cloud-only; Docker offline. NXD use Docker, VXD with `docker` provider works offline too. |

## Risks

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Ghost API key leaks via logs | M | H | Hash conn strings in events; redact key in any log path; pre-commit scan |
| Docker daemon flakiness | M | M | Health check in preflight; retry once with backoff; fall back to `null` provider |
| Orphan DBs leak (cost / disk) | M | M | GC routine on `vxd resume`; daily cron deletes stale DBs (>retention) |
| Provider interface churn during impl | H | L | Land SP1 first as a separate PR before SP2/3 start; pin interface |
| Live testing burns Ghost free tier | M | L | 100 hrs/mo headroom; auto-delete keeps usage minimal; switch to Docker for stress tests |
| NXD diverges from VXD over time | M | M | Port script in CI; wiring tests in both repos |

## Implementation order + estimated PRs

1. **SP1** — `internal/devdb` foundation (1 PR, ~600 LOC + tests)
2. **SP3** — Docker provider (1 PR, ~800 LOC + tests) — *before SP2 because NXD needs it sooner*
3. **SP2** — Ghost provider (1 PR, ~700 LOC + tests)
4. **SP4** — Per-story executor wiring (1 PR, ~400 LOC + tests + wiring tests)
5. **SP5** — QA migration gate (1 PR, ~300 LOC + tests)
6. **SP6** — Visibility (1 PR, ~500 LOC + tests)
7. **Test waves** — Wave 1 inside each PR; Waves 2 and 3 are separate PRs (live runs documented).
8. **NXD port** — mirror PRs in nexus-dispatch, one per SP1/SP3/SP4/SP5/SP6 (no SP2 in NXD).

Roughly 11–13 PRs total across both repos.

## Open questions (flag, don't block)

1. **Should SP1 export both sync and async (`context.Context`) APIs?** Current VXD code mixes. Decision deferred to SP1 spec.
2. **Postgres version pinning per project?** Some clients are on 13, some on 16. Per-project `devdb.docker.image` allows it. Ghost is whatever Timescale provisions — open question whether they expose a version pin.
3. **Schema snapshot lifecycle?** Templates need refresh as prod schema evolves. Phase-2 feature: scheduled `ghost share` snapshot capture.
4. **pgvector / TimescaleDB extensions?** Ghost ships these. Docker provider needs `docker pull timescale/timescaledb-ha:pg16-all` or custom Dockerfile.

## References

- ghost.build homepage scrape: `.firecrawl/ghost-build-home.md`
- ghost.build docs scrape: `.firecrawl/ghost-build-docs.md`
- Existing VXD artifact-protection pattern: `engine/artifact_protection_test.go`
- Existing per-project state isolation: `~/.vxd/projects/<name>/`
- AgentFlow-inspired declarative criteria: `internal/engine/criteria.go`
- NXD offline-first ethos: `~/Sites/misc/nexus-dispatch/CLAUDE.md`
