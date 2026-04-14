# Human Review Gates

## Overview

Adds human approval checkpoints to the VXD pipeline so you can review plans before dispatch and review PRs before merge. Three configurable modes: `auto` (current behavior), `plan_only` (approve plan, auto-merge PRs), and `manual` (approve both plan and each PR). Per-run override via `--review`/`--auto` flags on `vxd resume`.

## Review Modes

### Config

```yaml
merge:
  auto_merge: true           # existing (kept for backward compat)
  review_mode: "auto"        # NEW: "auto" | "manual" | "plan_only"
  base_branch: "main"
  pr_template: "..."
```

| Mode | Plan Gate | PR Gate | Use Case |
|------|-----------|---------|----------|
| `auto` | None — resume dispatches immediately | Auto-merged | Internal work, trusted agent |
| `plan_only` | Must `vxd approve-plan` before resume dispatches | Auto-merged | Review plan, trust execution |
| `manual` | Must approve plan | Must `vxd approve` each PR | Client projects, production repos |

### Per-Run Override

```
vxd resume <req-id> --review    # forces manual for this run
vxd resume <req-id> --auto      # forces auto for this run
vxd resume <req-id>             # uses config default
```

The chosen mode is persisted as `EventReviewModeSet` in the event log, so it survives process restarts.

### Backward Compatibility

If `review_mode` is not set in config, falls back to `auto_merge` field:
- `auto_merge: true` → `"auto"`
- `auto_merge: false` → `"manual"`

## Plan Review Gate

Active when `review_mode` is `"plan_only"` or `"manual"`.

### Flow

1. `vxd req "requirement"` — plans and prints stories as today
2. Output includes: "Review mode: plan approval required before dispatch."
3. User runs `vxd approve-plan <req-id>` → emits `EventPlanApproved`
4. User runs `vxd resume <req-id>` → checks for `EventPlanApproved`, dispatches if found
5. If no approval exists: error "Plan approval required. Run 'vxd approve-plan <req-id>' first."

### Reject and Re-Plan

`vxd reject-plan <req-id> "feedback"`:
- Emits `EventPlanRejected` with feedback in payload
- Re-runs `Planner.Plan()` with rejection feedback appended as a constraint
- Prints the new plan for review
- User can approve or reject again

## PR Review Gate

Active when `review_mode` is `"manual"`.

### Flow

After QA passes, instead of auto-merging:

1. Monitor creates PR as usual (push branch, `gh pr create`)
2. Emits `EventStoryPRCreated` (existing)
3. Emits `EventStoryAwaitingApproval` (NEW)
4. Story status becomes `"awaiting_approval"`
5. Pipeline pauses for this story — waits for human

### Review Command

`vxd review <story-id>` — shows story details and diff:

```
Story: Setup OAuth middleware
ID:     r-001-s-001
Status: awaiting_approval
PR:     #42 — https://github.com/acme/api/pull/42
Branch: vxd/r-001-s-001
Agent:  junior-r-001-1
Complexity: 3 | Wave: 1 | Escalations: 0

─── Diff ──────────────────────────────────────
+ internal/auth/middleware.go  (new, 45 lines)
+ internal/auth/middleware_test.go  (new, 62 lines)
  go.mod  (modified, +1 -0)
─── End Diff ──────────────────────────────────

[o] Open PR in browser  [a] Approve  [r] Reject  [q] Quit
```

Interactive single-key prompt. Non-interactive: `vxd review <story-id> --open` to open PR in browser.

### Approve

`vxd approve <story-id>`:
- Validates story is in `awaiting_approval`
- Emits `EventStoryApproved`
- Triggers merge (calls merger)
- Monitor detects merge, cleans up, dispatches next wave

`vxd approve --all <req-id>`:
- Finds all stories in `awaiting_approval` for this requirement
- Approves and merges each in dependency order
- Prints summary: "Approved N stories. Merging..."

### Reject with Feedback

`vxd reject <story-id> "fix error handling in auth.go"`:
- Validates story is in `awaiting_approval`
- Emits `EventStoryRejected` with feedback in payload
- Resets story to `draft` status
- Feedback injected into retry agent's prompt as "Human review feedback: ..."
- Normal retry/escalation flow takes over

## New Events

| Event | Emitted By | Payload |
|-------|-----------|---------|
| `EventReviewModeSet` | `vxd resume` | `{mode, req_id}` |
| `EventPlanApproved` | `vxd approve-plan` | `{req_id}` |
| `EventPlanRejected` | `vxd reject-plan` | `{req_id, feedback}` |
| `EventStoryAwaitingApproval` | Monitor (after QA pass in manual mode) | `{story_id, pr_url}` |
| `EventStoryApproved` | `vxd approve` | `{story_id, approved_by: "human"}` |
| `EventStoryRejected` | `vxd reject` | `{story_id, feedback}` |

## New Story Status

`"awaiting_approval"` — between `qa` (QA passed) and `merged`.

Displayed in `vxd status`:
```
  1. [awaiting_approval] Setup OAuth middleware
     PR: #42 (https://github.com/acme/api/pull/42)
     Run 'vxd review r-001-s-001' to review
```

## Architecture

### New Files

| File | Purpose |
|------|---------|
| `internal/engine/review_gate.go` | `ReviewGate` — resolves mode, checks approvals |
| `internal/engine/review_gate_test.go` | Tests for gate logic |
| `internal/cli/approve.go` | `vxd approve`, `vxd approve --all`, `vxd approve-plan` |
| `internal/cli/reject.go` | `vxd reject`, `vxd reject-plan` |
| `internal/cli/review.go` | `vxd review` — diff display, interactive prompt |

### Modified Files

| File | Change |
|------|--------|
| `internal/config/config.go` | Add `ReviewMode` to `MergeConfig`, validation |
| `internal/config/loader.go` | Default `ReviewMode` to `""` (backward compat fallback) |
| `internal/state/events.go` | Add 6 new event type constants |
| `internal/state/sqlite.go` | Handle `awaiting_approval` in projections |
| `internal/engine/monitor.go` | After QA pass, check review mode before merging |
| `internal/cli/resume.go` | Add `--review`/`--auto` flags, emit `EventReviewModeSet`, plan gate check |
| `internal/cli/root.go` | Register 5 new commands |

### ReviewGate Struct

```go
type ReviewGate struct {
    events state.EventStore
    proj   *state.SQLiteStore
}

func (g *ReviewGate) ResolveMode(reqID string, cfg config.MergeConfig) string
func (g *ReviewGate) PlanApproved(reqID string) bool
func (g *ReviewGate) StoryApproved(storyID string) bool
func (g *ReviewGate) PendingApprovals(reqID string) ([]state.Story, error)
```

### Monitor Integration

In `postExecutionPipeline`, after QA passes:

```go
mode := m.reviewGate.ResolveMode(reqID, m.config.Merge)
if mode == "manual" {
    // Create PR but don't merge
    result, err := m.merger.CreatePROnly(...)
    emit(EventStoryPRCreated)
    emit(EventStoryAwaitingApproval)
    return // stop pipeline, wait for human
}
// auto/plan_only: merge as before
```

### Approve Triggers Merge

When `vxd approve <story-id>` runs:
1. Emits `EventStoryApproved`
2. Calls `merger.MergeExistingPR(storyID)` — merges the already-created PR
3. Emits `EventStoryMerged`
4. If monitor is running, it picks up the merge event and dispatches next wave
5. If monitor is not running, user re-runs `vxd resume` to continue

## New CLI Commands

| Command | Arguments | Flags |
|---------|-----------|-------|
| `vxd approve-plan <req-id>` | Requirement ID | — |
| `vxd reject-plan <req-id> "feedback"` | Requirement ID + feedback string | — |
| `vxd review <story-id>` | Story ID | `--open` (open PR in browser) |
| `vxd approve <story-id>` | Story ID | `--all <req-id>` (batch approve) |
| `vxd reject <story-id> "feedback"` | Story ID + feedback string | — |

## Out of Scope

- Web dashboard integration for approve/reject buttons (future)
- Automatic notification when stories are awaiting approval (email/Slack)
- Per-story mode override (all stories in a requirement use the same mode)
- Review gate for escalation decisions (keep automated for now)
