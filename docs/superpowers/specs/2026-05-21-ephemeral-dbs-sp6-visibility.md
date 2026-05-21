# SP6 — Human Visibility (CLI, Dashboard, Metrics, MCP, Docs)

**Parent:** `2026-05-21-ephemeral-dbs-master-design.md`
**Depends on:** SP1, SP4 (SP2/SP3 in production deploys, but tests can use null)
**Status:** Draft
**Scope:** Everything humans see and touch: CLI commands, dashboard panels, metrics, MCP install integration, use-case docs.

## Purpose

Per the user requirement: agent-first, but with human visibility. SP1–5 cover agents. SP6 covers humans.

## New CLI commands

All in `internal/cli/`. Cobra-rooted under `vxd db ...`.

| Command | Purpose |
|---------|---------|
| `vxd db list` | List all ephemeral DBs, with `--all`, `--status`, `--story`, `--project` filters |
| `vxd db connect <story-id>` | Open psql against the story's DB |
| `vxd db logs <story-id>` | Stream DB logs (provider-specific) |
| `vxd db sql <story-id> "<query>"` | Run a one-shot SQL query |
| `vxd db schema <story-id>` | Print agent-friendly schema dump |
| `vxd db delete <story-id>` | Manually delete a retained DB |
| `vxd db gc` | Run orphan recovery on demand |
| `vxd db template list` | List templates registered with the provider |
| `vxd db template create <name> --from <dump-file>` | Seed a template |
| `vxd db template refresh <name> --from <dump-file>` | Atomic template swap |
| `vxd db ping` | Provider health check (alias for relevant preflight subset) |

Each adds an entry in CLAUDE.md CLI table (mandatory per existing `TestDocCoverage_CLICommands`).

## Dashboard

### TUI (`internal/dashboard/`)

Add a DB indicator column to the story table:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ STORY                  STATUS       AGENT      TIER  DB                      │
├─────────────────────────────────────────────────────────────────────────────┤
│ a8cbef1f-add-users     in_progress  claude-c   T0    ✓ vxd-mukuru-…-3a  4m  │
│ 3b8901cd-migrate-pay   review       codex      T1    ✓ vxd-mukuru-…-1f  9m  │
│ 7c2334e1-static-html   in_progress  gemini     T0    -                       │
└─────────────────────────────────────────────────────────────────────────────┘
```

`✓` = DB active. `-` = no DB (null provider or pure-frontend story). `✗` = DB failed/released.

Keystroke `d` opens a side panel showing connection string (truncated), age, DB-hours used.

### Web (`internal/web/`)

Same panel, exposed via existing WebSocket protocol. New WS message types:

```jsonc
{ "type": "db_status_update", "story_id": "...", "db": { "id": "...", "name": "...", "status": "...", "age_seconds": 482 } }
```

A new `/api/db` endpoint (read-only) lists DBs for the current project.

## Metrics

`vxd metrics` (existing command) gets a "Databases" section per requirement:

```
=== Requirement: 7d2f...  Add user profile fields ===
  Stories: 6 done, 0 in_progress, 1 paused
  Avg duration: 7m
  Escalations: 1 (T1→T2)

  Databases:
    Created:  7
    Deleted:  6
    Retained: 1 (paused story)
    Total DB-hours: 0.83
    Provider: ghost
    Free-tier remaining (this month): 95.8h / 100h
```

Implementation: query the new `story_databases` projection (from SP1) joined to `stories`.

## MCP install

On `vxd init` (or new `vxd db init`):

1. If `cfg.DevDB.Provider == "ghost"` and `ghost` binary on PATH: run `ghost mcp install claude-code` to register the Ghost MCP for the user's Claude Code config.
2. If `cfg.DevDB.Provider == "docker"`: write a thin VXD-owned MCP wrapper config that exposes the Docker provider's tools (`create`, `fork`, `sql`, `schema`, `delete`).

For Docker mode, we ship a small MCP server in `cmd/vxd-db-mcp/` (or `cmd/nxd-db-mcp/`) that links the existing `internal/devdb` package. It implements the MCP stdio protocol and exposes:

```
Tools:
  devdb_create
  devdb_fork
  devdb_delete
  devdb_list
  devdb_sql
  devdb_schema
  devdb_ping
```

Same tool names as Ghost where they match — so agents writing prompts that say "use `ghost_create`" mostly work either way. Mapping documented in the use-case doc.

### Per-worktree MCP

For VXD-orchestrated agents, the spawn pipeline (SP4) writes `.vxd-db/mcp.json` into the worktree, picked up by the agent CLI:

```jsonc
{
  "mcpServers": {
    "devdb": {
      "command": "/usr/local/bin/ghost",
      "args": ["mcp", "start", "stdio"]
    }
  }
}
```

For Docker provider:

```jsonc
{
  "mcpServers": {
    "devdb": {
      "command": "/Users/mn/.local/bin/vxd-db-mcp",
      "args": ["start"]
    }
  }
}
```

This is **opt-in** per project (`devdb.expose_mcp: true`). Default off — many stories don't need MCP-driven DB ops; the env var is enough.

## Use-case documentation

New file: `docs/use-cases/ephemeral-dbs.md` (mirror in `docs/` of NXD).

Sections (one short paragraph each):
1. What it is — 3-sentence summary lifting from master spec TL;DR.
2. When it shines — per-story migration testing, schema-aware codegen, destructive SQL, multi-agent experimentation.
3. When to skip — pure-frontend, prod-touching ops, long-running pipelines spanning stories.
4. Provider choice — Ghost vs Docker decision matrix.
5. Cost expectations — Ghost free tier, Docker disk usage.
6. Getting started — minimum vxd.yaml block + one-command init.
7. Troubleshooting — common errors and fixes.

This doc is what we point prospects/users to. It's also where we keep the "Ghost MCP tool names" mapping for prompt-engineering reference.

## Arch diagram updates

`docs/diagrams/devdb-flow.d2` (new) — sequence diagram of story → fork → run → release.

`docs/diagrams/arch-overview.d2` (existing, modified) — add `internal/devdb` box between executor and external services.

`docs/diagrams/render.sh` (existing) — runs D2 + dot to regenerate SVGs in CI. SP6 just adds the new D2 source; render script already iterates.

## Obsidian vault updates

VXD vault (`/Users/mncedimini/Documents/Obsidian Vault/VXD/`):
- New: `14 Ephemeral Databases.md` — readable narrative paralleling the use-case doc.
- Update: `00 Index.md` — add link.
- Update: `01 Architecture.md` — add devdb to the layered diagram description.
- Update: `10 Diagrams.md` — embed the new SVG.

NXD vault (`/Users/mncedimini/Documents/Obsidian Vault/NXD/`):
- New: `10 Ephemeral Databases.md` (Docker-only flavour).
- Update: `00 Index.md`, `01 Architecture.md`.

"Vortex Dispatch" folder (mirror of docs/): re-mirror after VXD repo changes.

## Tests (Wave 1 for SP6)

| Test | Asserts |
|------|---------|
| `TestDB_List_Filters` | filter by status / story / project |
| `TestDB_Connect_BuildsPsqlCommand` | correct args, env passes through |
| `TestDB_Delete_ConfirmsBeforeDestructive` | prompt or `--confirm` |
| `TestDB_Template_Create_Refresh_List` | full template lifecycle (uses null provider with in-memory fake) |
| `TestMetrics_DBSection_Renders` | hours + free-tier line present |
| `TestMCPInstall_Ghost_DelegatesToGhostBinary` | `ghost mcp install` invoked when binary present |
| `TestMCPInstall_Docker_WritesWrapperConfig` | `.vxd-db/mcp.json` shape correct |
| `TestDashboard_DBColumn_Renders` | golden TUI snapshot |
| `TestWeb_DBStatusUpdate_Broadcasts` | WS message shape matches schema |
| `TestDocCoverage_CLICommands_DB` | all new `vxd db` commands in CLAUDE.md |
| `TestDocCoverage_ConfigSections_DevDB_README` | `devdb` block documented in README |

## Open questions

- TUI column width — at 7 active stories the table gets tight. Decision deferred to impl; may need a per-row collapsible.
- Web dashboard auth — DB conn strings are sensitive. Current web dashboard is localhost-only by default; SP6 must preserve that. Surface a config error if `--web --bind=0.0.0.0` is used with `devdb` non-null without explicit `--unsafe-bind`.
- MCP tool naming clash — if a worktree already has another MCP server named `devdb`, we should not silently overwrite. Detect and warn.
