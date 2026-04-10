# Client Project Isolation Design

**Date:** 2026-04-10
**Status:** Approved
**Approach:** Per-project state directories with automatic resolution + auto-migration

## Overview

Isolate VXD state (events, projections, worktrees, logs) per project so multiple clients can be served simultaneously without data bleed. Projects are identified by git repository name (auto-detected) or explicit `--project` flag. Existing data is auto-migrated on first run. Global config provides defaults, per-project `vxd.yaml` overrides.

## Project Directory Structure

```
~/.vxd/
├── config.yaml                    # Global defaults (models, routing, runtimes)
├── projects/
│   ├── acme-corp-api/             # Auto-named from repo
│   │   ├── events.jsonl           # Isolated event log
│   │   ├── vxd.db                 # Isolated SQLite projections
│   │   ├── worktrees/             # Per-project worktrees
│   │   ├── logs/                  # Per-project agent logs
│   │   └── metadata.json          # Project name, repo path, created_at
│   ├── client-b-frontend/
│   │   ├── events.jsonl
│   │   ├── vxd.db
│   │   └── ...
│   └── _legacy/                   # Auto-migrated old data
│       ├── events.jsonl
│       ├── vxd.db
│       └── metadata.json
├── self-improve/                  # Global (not per-project)
│   └── launchd.log
```

### Project Name Derivation

1. `git remote get-url origin` → extract last segment: `github.com/tzone85/acme-corp-api.git` → `acme-corp-api`
2. If no remote → use directory name of repo root
3. Sanitize: lowercase, replace non-alphanumeric with `-`, trim dashes

Override with `--project` flag or `VXD_PROJECT` environment variable.

**Collision handling:** If two repos produce the same name (e.g., two forks of `api`), append a short hash of the remote URL: `api-a3f2`. The `--project` flag always takes priority for explicit control.

### Config Resolution Order

1. Per-project `vxd.yaml` in repo root (highest priority)
2. Global `~/.vxd/config.yaml` (fallback defaults)
3. Built-in `DefaultConfig()` (hardcoded fallback)

Fields present in the per-project config override the global. Fields absent in per-project fall through to global. This matches how Git handles `.gitconfig` + per-repo config.

## Project Resolution Logic

When any `vxd` command runs:

```
1. Check --project flag → use that name directly
2. Check VXD_PROJECT env var → use that name
3. Detect git repo from cwd:
   a. git rev-parse --show-toplevel → repo root
   b. git remote get-url origin → remote URL
   c. Extract project name from remote (or directory name fallback)
4. If no git repo → error: "Not in a git repository. Use --project or run vxd init first."
```

### State Loading

`loadStores()` changes from:
```go
stateDir := expandHome(cfg.Workspace.StateDir)  // ~/.vxd/
es := NewFileStore(stateDir + "/events.jsonl")
ps := NewSQLiteStore(stateDir + "/vxd.db")
```

To:
```go
project := resolveProject(cmd)
projectDir := filepath.Join(expandHome("~/.vxd"), "projects", project)
os.MkdirAll(projectDir, 0o755)
es := NewFileStore(filepath.Join(projectDir, "events.jsonl"))
ps := NewSQLiteStore(filepath.Join(projectDir, "vxd.db"))
```

The config `Workspace.StateDir` field becomes the project state directory, computed at runtime rather than statically configured.

## Auto-Migration

### Trigger

`~/.vxd/events.jsonl` exists AND `~/.vxd/projects/` directory does not exist.

### Steps

1. Create `~/.vxd/projects/_legacy/`
2. Move `~/.vxd/events.jsonl` → `~/.vxd/projects/_legacy/events.jsonl`
3. Move `~/.vxd/vxd.db` → `~/.vxd/projects/_legacy/vxd.db`
4. Move `~/.vxd/worktrees/` → `~/.vxd/projects/_legacy/worktrees/` (if exists)
5. Move `~/.vxd/logs/` → `~/.vxd/projects/_legacy/logs/` (if exists)
6. Write `~/.vxd/projects/_legacy/metadata.json`:
   ```json
   {
     "name": "_legacy",
     "migrated_from": "~/.vxd",
     "migrated_at": "2026-04-10T06:00:00Z",
     "note": "Auto-migrated from pre-isolation layout"
   }
   ```
7. If the current repo can be resolved, symlink `~/.vxd/projects/<repo-name>` → `~/.vxd/projects/_legacy/` so the current project resolves naturally
8. Log: `"Migrated existing VXD data to ~/.vxd/projects/_legacy/"`

### Idempotency

If `~/.vxd/projects/` already exists, skip migration entirely. No risk of double-migration.

## CLI Changes

### New Global Flag

```
vxd --project <name> <any-command>
```

Added as `rootCmd.PersistentFlags().String("project", "", "Project name (auto-detected from git repo if not specified)")`.

### New Command: `vxd projects`

```bash
vxd projects              # List all projects
```

Output:
```
PROJECT              REPO PATH                                STORIES  MERGED  STATUS
acme-corp-api        /Users/you/Sites/acme-corp-api          12       8       active
client-b-frontend    /Users/you/Sites/client-b               5        3       active
_legacy              (migrated from ~/.vxd)                   73       46      archived
```

Reads `metadata.json` from each project directory and counts stories from each `vxd.db`.

### Existing Commands — Behavior Changes

| Command | Before (global) | After (project-scoped) |
|---------|----------------|----------------------|
| `vxd req "..."` | Stores in `~/.vxd/events.jsonl` | Stores in `~/.vxd/projects/<project>/events.jsonl` |
| `vxd status` | Shows all requirements (filtered by cwd) | Shows only current project's requirements |
| `vxd resume <id>` | Loads from global state | Loads from project state |
| `vxd metrics` | Computes from global state | Computes from current project's state |
| `vxd dashboard` | Shows current repo (ReqFilter) | Shows current project only |
| `vxd agents` | All agents | Current project's agents |
| `vxd events` | All events | Current project's events |

### Cross-Project Access

```bash
vxd metrics --project acme-corp        # View another project's metrics
vxd status --project _legacy           # View migrated legacy data
```

### Not Project-Scoped

These remain global:
- `vxd-improve` (self-improvement engine)
- `vxd opportunity` (revenue pipeline)
- `vxd memory --web` (memory dashboard)

## Project Metadata

`~/.vxd/projects/<name>/metadata.json`:

```json
{
  "name": "acme-corp-api",
  "repo_path": "/Users/you/Sites/acme-corp-api",
  "remote_url": "git@github.com:tzone85/acme-corp-api.git",
  "created_at": "2026-04-10T08:00:00Z",
  "last_activity": "2026-04-10T15:30:00Z"
}
```

Written on first `vxd init` or first `vxd req` in a new project. Updated `last_activity` on every command that writes state.

## Files Changed

| File | Action | Description |
|------|--------|-------------|
| `internal/engine/project.go` | **New** | `ResolveProjectName()`, `MigrateOldLayout()`, `ProjectMetadata` type, `ListProjects()` |
| `internal/engine/project_test.go` | **New** | Migration tests, name resolution, idempotency, sanitization |
| `internal/cli/helpers.go` | **Major edit** | `loadStores()` gains project resolution + auto-migration trigger |
| `internal/cli/root.go` | **Edit** | Add `--project` persistent flag, register `newProjectsCmd()` |
| `internal/cli/projects.go` | **New** | `vxd projects` command |
| `internal/config/loader.go` | **Edit** | `LoadConfig()` checks `~/.vxd/config.yaml` as global fallback |
| `internal/engine/wiring_test.go` | **Edit** | Wiring tests for project resolution and isolation |

## Constraints

- **Zero breaking changes** — existing commands work identically if you never use `--project`
- **Auto-migration preserves all data** — 73 stories, 20 requirements, feedback loop data all preserved
- **Self-improvement and opportunities stay global** — they're account-level, not project-level
- **No changes to state package** — FileStore and SQLiteStore already accept paths; isolation is at CLI layer
- **Project names are filesystem-safe** — sanitized to lowercase alphanumeric + dashes
