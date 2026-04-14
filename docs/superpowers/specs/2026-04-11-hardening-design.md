# VXD Hardening: Crash Recovery, Lock File, Security

**Date:** 2026-04-11
**Status:** Approved
**Scope:** 3 features (crash recovery, lock file, security hardening) + NXD port

---

## 1. Crash Recovery

### Problem
When the Mac sleeps mid-pipeline, force-quits, or loses power, VXD can leave stories in inconsistent states:
- Stories stuck in `in_progress` with no tmux session and no worktree (fully lost work)
- Stories stuck in `merging` (partial rebase/push/merge — unclear what completed)
- No checkpoint of the monitor's `RunContext`, so resume must reconstruct everything

### Design

#### 1.1 Checkpoint File
Location: `<state_dir>/checkpoint.json`

Written at each phase transition:
```json
{
  "req_id": "abc123",
  "phase": "monitoring",
  "wave_number": 2,
  "active_agents": [{"story_id": "...", "session": "...", "worktree": "..."}],
  "timestamp": "2026-04-11T10:00:00Z",
  "pid": 12345
}
```

Phases: `dispatching` → `monitoring` → `merging:<story_id>` → `monitoring` → `completed`

The checkpoint is written by the monitor at:
- Wave dispatch start
- Before each merge attempt (phase = `merging:<story_id>`)
- After each successful merge (phase back to `monitoring`)
- On graceful exit

#### 1.2 Consistency Check on Resume
New function `CheckConsistency(stories, config) []Issue` runs before dispatch:

| Condition | Action |
|-----------|--------|
| Story `in_progress`, no tmux, no worktree | Reset to `draft` via `STORY_RESET` event |
| Story `in_progress`, no tmux, worktree exists | Existing orphan recovery (already works) |
| Story `merging`, branch pushed, PR exists | Resume merge from PR |
| Story `merging`, branch pushed, no PR | Create PR and merge |
| Story `merging`, branch not pushed | Reset to `review_passed`, re-enter merge pipeline |
| Checkpoint stale (>6h) | Warn user, don't block |

#### 1.3 Merge Atomicity
The existing `mergeMu` serialization is good. Add checkpoint writes before/after:
```
checkpoint("merging:story-1")
  rebase → push → create PR → merge PR
checkpoint("monitoring")
```

If resume finds checkpoint stuck at `merging:<id>`, it inspects git/GitHub state to determine where to resume.

#### 1.4 New Event Types
- `STORY_RESET` — story reset to draft after crash recovery
- `RECOVERY_COMPLETED` — emitted after consistency check completes with fixes

#### 1.5 Files
- `internal/engine/recovery.go` — `CheckConsistency()`, `RecoveryIssue` type
- `internal/engine/checkpoint.go` — `Checkpoint` type, `WriteCheckpoint()`, `ReadCheckpoint()`
- `internal/engine/recovery_test.go` — test consistency checks
- `internal/engine/checkpoint_test.go` — test checkpoint read/write

---

## 2. Multi-Requirement Lock File

### Problem
Running two `vxd resume` instances simultaneously corrupts SQLite and events.jsonl.

### Design

#### 2.1 Lock File
Location: `<state_dir>/vxd.lock`

Contents:
```json
{"pid": 12345, "started_at": "2026-04-11T10:00:00Z", "req_id": "abc123"}
```

#### 2.2 Lifecycle
- **Acquire** at start of `runResume` (before any state access)
- **Release** on exit via `defer`
- **Stale check**: If lock file exists, check if PID is alive via `syscall.Kill(pid, 0)`
  - Dead PID → reclaim lock, log warning
  - Live PID → error: "another VXD instance is running (PID %d, started %s)"
- **Force override**: `--force` flag on `vxd resume` reclaims lock regardless

#### 2.3 Files
- `internal/engine/lockfile.go` — `AcquireLock()`, `ReleaseLock()`, `LockInfo` type
- `internal/engine/lockfile_test.go` — test acquire/release/stale detection

---

## 3. Security Hardening

### Problem
- Model name in `BuildCommand` is unquoted — shell injection if model name contains metacharacters
- Runtime args concatenated without quoting
- No input validation on values that flow into shell commands

### Design

#### 3.1 Sanitize Inputs
New `internal/runtime/sanitize.go`:

```go
// ValidModelName checks model names against allowlist pattern.
var validModelPattern = regexp.MustCompile(`^[a-zA-Z0-9._:/-]+$`)

func ValidateModelName(model string) error
func ValidateSessionName(name string) error
func ValidateShellArg(arg string) error
```

Rejected characters: `;`, `|`, `&`, `$`, `` ` ``, `(`, `)`, `{`, `}`, `<`, `>`, `\n`

#### 3.2 Fix BuildCommand
```go
// Before (vulnerable):
cmdStr += " --model " + cfg.Model

// After (safe):
if err := ValidateModelName(cfg.Model); err != nil {
    return "", fmt.Errorf("invalid model name: %w", err)
}
cmdStr += fmt.Sprintf(" --model %q", cfg.Model)
```

Also quote runtime args:
```go
for _, arg := range c.args {
    if err := ValidateShellArg(arg); err != nil {
        return "", fmt.Errorf("invalid runtime arg %q: %w", arg, err)
    }
    cmdStr += " " + shellescape(arg)
}
```

#### 3.3 Validation Points
- `BuildCommand()` — model name, runtime args
- `tmux.CreateSession()` — session name
- `Spawn()` — all SessionConfig string fields

#### 3.4 Files
- `internal/runtime/sanitize.go` — validation functions
- `internal/runtime/sanitize_test.go` — test all patterns
- `internal/runtime/registry.go` — apply validation in BuildCommand

---

## 4. NXD Port (Cost Estimation + Report)

Copy `internal/cli/estimate.go`, `internal/cli/report.go`, `internal/engine/cost.go`, `internal/engine/report.go` and their tests to nexus-dispatch. Adjust imports from `tzone85/vortex-dispatch` to `tzone85/nexus-dispatch`. No functional changes needed — these features are LLM-provider-agnostic.

---

## Testing Strategy

Each feature gets unit tests. Integration coverage via the existing wiring test pattern.

| Feature | Test Count (est.) | Key Scenarios |
|---------|-------------------|---------------|
| Crash Recovery | 8-10 | Each consistency condition, checkpoint read/write |
| Lock File | 5-6 | Acquire, release, stale PID, force override, concurrent |
| Security | 8-10 | Valid/invalid model names, session names, args, shell escaping |
| NXD Port | Existing tests copied | Verify they pass in NXD context |
