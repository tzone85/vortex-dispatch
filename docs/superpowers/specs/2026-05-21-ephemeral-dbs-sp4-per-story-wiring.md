# SP4 — Per-Story Executor Wiring

**Parent:** `2026-05-21-ephemeral-dbs-master-design.md`
**Depends on:** SP1, and at least one of SP2/SP3
**Status:** Draft
**Scope:** Hook `devdb.Lifecycle` into VXD's story execution pipeline. Agents transparently get a DB.

## Purpose

This is the wiring PR. After it lands, a story dispatched against a project with `devdb.provider != null` will:

1. Have a DB forked from the template *before* its agent spawns.
2. Have `.vxd-db/connect.env` written into its worktree.
3. Emit `STORY_DB_CREATED` so dashboards see it.
4. Have the DB deleted (or retained per policy) after the story finishes.
5. Be visible to orphan recovery on `vxd resume`.

No agent-facing API changes. Just an env var that wasn't there before.

## Touchpoints (existing files modified)

| File | Change |
|------|--------|
| `internal/engine/executor.go` | Inject `*devdb.Lifecycle`; call `Provision()` in `spawn()` before agent dispatch; call `Release()` in post-execution pipeline |
| `internal/engine/post_execution.go` (or wherever STORY_COMPLETED/STORY_FAILED is emitted) | Release DB after merge/retain decision |
| `internal/engine/artifact_protection.go` | Add `.vxd-db/` to `stripVXDArtifactsFromBranch` cleanup list |
| `internal/engine/resume.go` | Wire orphan recovery on resume |
| `internal/engine/monitor.go` | On terminal escalation (tier-4 pause), call `Release(OutcomePaused)` |
| `internal/runtime/registry.go` | No change — DSN is read from `.vxd-db/connect.env`, not env vars (avoids tmux env propagation issues) |
| `internal/preflight/checks.go` | Two new checks: `CheckDevDBProviderReachable`, `CheckDevDBTemplateExists` |
| `internal/cli/root.go` | Wire Lifecycle construction from config |

## Lifecycle integration

### In `Executor.spawn()`

Just before existing prompt-rendering:

```go
// Pseudocode. Real code preserves existing return shape.
func (e *Executor) spawn(repoDir string, a Assignment, story PlannedStory) SpawnResult {
    worktreeDir, err := e.git.PrepareWorktree(...)
    if err != nil { return e.failSpawn(...) }

    // NEW: provision DB before writing CLAUDE.md / prompt
    var db devdb.DB
    if e.lifecycle != nil {
        db, err = e.lifecycle.Provision(ctx, story.ID, e.projectName, worktreeDir)
        if err != nil {
            // STORY_DB_FAILED already emitted by Lifecycle on Provision error;
            // we treat this as a degraded spawn — write a sentinel into worktree
            // and let the agent proceed. Cleaner than failing the whole story.
            _ = devdb.WriteFallbackNotice(worktreeDir, err)
        }
    }

    // Existing: write prompt, CLAUDE.md, spawn tmux session
    // ...
    return SpawnResult{StoryID: story.ID, DB: db}
}
```

`SpawnResult.DB` is new (zero-value when devdb disabled).

### Post-execution hook

Wherever `STORY_COMPLETED` / `STORY_FAILED` is finalized, after merge or rejection:

```go
if e.lifecycle != nil && spawnResult.DB.ID != "" {
    outcome := devdb.OutcomeSuccess
    if storyFailed { outcome = devdb.OutcomeFailed }
    if err := e.lifecycle.Release(ctx, spawnResult.DB, outcome); err != nil {
        // Don't block pipeline. Emit STORY_DB_FAILED as a "release_failed" subtype
        // so GC can pick it up later.
        log.Warn().Err(err).Str("story", story.ID).Msg("devdb release failed; will GC later")
    }
}
```

### Resume / orphan recovery

In `resume.go`, after lock acquisition + crash check:

```go
if cfg.DevDB.Provider != "" && cfg.DevDB.Provider != "null" {
    activeStoryIDs := projectionStore.ActiveStoryIDs(ctx, reqID)
    orphans, err := devdb.FindOrphans(ctx, lifecycle.Provider(), naming.Prefix(), activeStoryIDs)
    if err != nil {
        log.Warn().Err(err).Msg("devdb orphan scan failed; continuing")
    } else {
        deleted, kept, _ := devdb.ReleaseOrphans(ctx, lifecycle.Provider(), orphans, cfg.DevDB.OnFailure.RetainHours)
        log.Info().Int("deleted", len(deleted)).Int("kept", len(kept)).Msg("orphan DB recovery")
    }
}
```

### Pause / SLA-breach handling

When `monitor.go` decides a story is dead (tier-4 escalation, SLA breach), it currently emits `STORY_SLA_BREACHED` + `AGENT_TERMINATED`. We extend:

```go
// After terminating agent, decide DB fate.
outcome := devdb.OutcomePaused // default: keep for postmortem (per default policy)
if cfg.DevDB.OnFailure.KeepDB {
    outcome = devdb.OutcomePaused // explicit
} else {
    outcome = devdb.OutcomeFailed
}
_ = lifecycle.Release(ctx, db, outcome)
```

## `.vxd-db/` and artifact protection

Add to `stripVXDArtifactsFromBranch`:

```go
// Before review/merge, ensure these never appear in PR diff.
removePaths := []string{
    ".vxd-prompts/",
    ".serena/",
    "WAVE_CONTEXT.md",
    ".vxd-db/",           // NEW
}
```

Existing `engine/artifact_protection_test.go` gets two new cases:
- `TestStripVXDArtifacts_RemovesVXDDB`
- `TestStripVXDArtifacts_PreservesUserDBFiles` — confirm we only strip `.vxd-db/`, not `db/` or `database/` or anything the project owns.

## Preflight checks

Two new in `internal/preflight/checks.go`:

```go
func CheckDevDBProviderReachable(cfg config.Config) Result {
    if cfg.DevDB.Provider == "" || cfg.DevDB.Provider == "null" {
        return Result{Severity: SeverityInfo, OK: true, Message: "devdb disabled"}
    }
    provider, err := lifecycle.ResolveProvider(cfg) // builds ghost or docker
    if err != nil {
        return Result{Severity: SeverityCritical, OK: false, Message: err.Error()}
    }
    if err := provider.Ping(ctx); err != nil {
        return Result{Severity: SeverityCritical, OK: false, Message: "devdb provider unreachable: " + err.Error()}
    }
    return Result{Severity: SeverityInfo, OK: true, Message: "devdb provider OK"}
}

func CheckDevDBTemplateExists(cfg config.Config) Result {
    // List, scan for cfg.DevDB.Template.
    // WARNING (not CRITICAL) if missing — allows preflight to run before template setup.
}
```

`--skip-devdb` flag bypasses both. `vxd req` and `vxd resume` already chain preflight; SP4 just ensures the new checks plug in.

## Configuration discovery flow

Config loading already chains: repo `vxd.yaml` → `~/.vxd/config.yaml` → defaults. SP4 adds:

1. Construct provider from final merged config.
2. Provider is nil-safe — if `provider: null`, `Lifecycle` is nil, executor short-circuits.
3. Executor.SpawnAll passes Lifecycle pointer (or nil) — same pattern as `SetArtifactStore`.

```go
func (e *Executor) SetDevDBLifecycle(l *devdb.Lifecycle) { e.lifecycle = l }
```

## Events emitted from this SP

- `STORY_DB_CREATED` — already defined in SP1; emitted by `Lifecycle.Provision()`.
- `STORY_DB_FAILED` — already defined; emitted on Provision/Release error. SP4 adds `"phase": "provision" | "release"` to payload.
- `STORY_DB_DELETED` — emitted by `Lifecycle.Release()`.

## Tests (Wave 1 for SP4)

Wiring tests (the critical layer — `internal/engine/wiring_test.go`):

| Test | Asserts |
|------|---------|
| `TestWiring_Executor_ProvisionsDB_BeforeSpawn` | Provision called with worktree path; spawn cmd contains DSN reference |
| `TestWiring_Executor_NoDevDB_WhenNullProvider` | `null.Provider` → no Provision call, no events |
| `TestWiring_PostExecution_ReleasesDB_OnSuccess` | After STORY_COMPLETED, Release(Success) called |
| `TestWiring_PostExecution_ReleasesDB_OnFailure_WithKeepDB` | KeepDB=true → status=retained |
| `TestWiring_PostExecution_ReleasesDB_OnFailure_WithoutKeepDB` | KeepDB=false → status=deleted |
| `TestWiring_Resume_RecoversOrphans` | Orphans deleted on resume |
| `TestWiring_Monitor_SLABreach_ReleasesDB` | SLA breach triggers Release |
| `TestWiring_Strip_VXDDB_NotInPRDiff` | Worktree has `.vxd-db/` but `git diff origin/main...HEAD` does not |
| `TestExecutor_FailedProvision_DegradesNotFails` | Provision error → fallback notice written → spawn proceeds |
| `TestPreflight_DevDBProviderReachable_OK` | Mocked provider OK → OK result |
| `TestPreflight_DevDBProviderReachable_Down` | Down → CRITICAL |
| `TestPreflight_DevDBTemplateExists_Missing` | Template absent → WARNING |

Use `null.Provider` for all wiring tests (SP1). No Docker/Ghost needed.

## NXD port

Mirror in nexus-dispatch:
- Same touchpoints (`internal/engine/executor.go`, `monitor.go`, `resume.go`).
- `devdb.docker` is the only provider (no Ghost).
- Wiring tests identical.

## Open questions

- DSN env var name — `DATABASE_URL` is the de-facto standard but some frameworks expect `DB_URL`, `POSTGRES_URL`, etc. Decision: write `DATABASE_URL` always; let projects symlink/alias if needed. Phase-2 could add per-project alias config.
- What about agents that don't read env vars (Codex, Gemini)? They read files. `.vxd-db/connect.env` is a file. They can `cat` it from the agent prompt. The README inside `.vxd-db/` tells them so.
