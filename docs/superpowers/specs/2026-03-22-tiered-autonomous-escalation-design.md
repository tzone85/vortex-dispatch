# Tiered Autonomous Escalation for VXD

**Date:** 2026-03-22
**Status:** Draft
**Author:** Design collaboration

## Problem Statement

When a story exhausts its retry limit, VXD pauses the entire requirement and waits for human intervention. This breaks the autonomous execution model. Transient failures (bad CLI args, missing deps), structural issues (overly complex stories), and environment problems all dead-end at the same "pause and wait" state.

The system needs a graduated, autonomous escalation chain that diagnoses failures and takes corrective action before resorting to human intervention.

## Design Goals

1. **Fully autonomous recovery** for all recoverable failure classes
2. **Cost-efficient** — try cheap options first, escalate to expensive models only when needed
3. **Bounded** — worst-case cost and retry count are predictable
4. **Observable** — all escalation activity visible via CLI and dashboard
5. **Safe** — loop prevention, split depth limits, and a human safety net as final fallback

## Escalation Chain

Five tiers, executed in order:

| Tier | Agent | Mode | Retries | Action on Exhaustion |
|------|-------|------|---------|---------------------|
| 0 | Original role (junior/intermediate) | CLI | `max_retries_before_escalation` (default 2) | Escalate to tier 1 |
| 1 | Senior | CLI | `max_senior_retries` (default 2) | Escalate to tier 2 |
| 2 | Manager | API | `max_manager_attempts` (default 2) | Escalate to tier 3 |
| 3 | Tech Lead | API | 1 re-plan attempt | Escalate to tier 4 |
| 4 | — | — | — | Pause requirement (human fallback) |

### Tier Determination

The current tier for a story is derived from `STORY_ESCALATED` events. The highest `to_tier` value across all such events for a story is the current tier (tier 0 if no escalation events exist).

### Per-Tier Retry Counting

Retry counts are scoped per tier. To count retries at the current tier:

1. Find the timestamp of the most recent `STORY_ESCALATED` event for this story (or epoch zero if none)
2. Count `EventStoryReviewFailed` events for this story with timestamp > that escalation timestamp

This requires extending `EventFilter` with an `After time.Time` field (see Integration Points).

### QA Failure Escalation

QA failures (`EventStoryQAFailed`) follow the same tier system as review failures. A QA failure at any tier resets the story to draft at the *same tier* and counts against that tier's retry counter (using `EventStoryReviewFailed` as the unified retry signal). The existing `max_qa_failures_before_escalation` is retired in favor of the unified per-tier counter.

### Retry Counter Resets

Each tier has its own counter. Moving up a tier starts a fresh counter. The manager can also explicitly reset to a lower tier (e.g., after fixing an environment issue), giving the story a clean start.

## Migration: Retiring `ESCALATION_CREATED`

The existing `EventEscalationCreated` (`ESCALATION_CREATED`) event and the current escalation logic in `handleReviewFailure()` are **superseded** by the new tier system.

**Migration steps:**
1. Remove `EventEscalationCreated` constant from `events.go`
2. Remove `handleReviewFailure()` from `monitor.go` — its logic is absorbed by the tier-aware `resetStoryToDraft()`
3. Update `routeStory()` in `dispatcher.go` to read tier from `STORY_ESCALATED` events instead of counting `ESCALATION_CREATED` events
4. Existing `ESCALATION_CREATED` events in the event log are ignored — they predate the tier system and have no `to_tier` field. The projection logic skips unknown event types gracefully.
5. Existing `EventStoryReviewFailed` events (emitted before the tier system) have no corresponding `STORY_ESCALATED` timestamp. The per-tier counter treats the absence of any `STORY_ESCALATED` event as tier 0 with `After = epoch zero`, meaning all pre-existing review failures count toward tier 0's limit. For in-flight requirements, this may cause an immediate escalation to tier 1 on the first new failure. This is acceptable: it is the conservative (safer) behavior, and only affects requirements that were already failing before the upgrade.

## Manager Role

### Definition

| Property | Value |
|----------|-------|
| Role constant | `RoleManager` |
| Position in hierarchy | Between senior and tech_lead |
| Execution mode | API (inline in monitor pipeline, not a tmux agent) |
| Default model | Configured via `models.manager.model` in `vxd.yaml` (ships as `claude-sonnet-4-20250514`) |
| Max tokens | 8000 |
| Provider | Configured via `models.manager.provider` (ships as `anthropic`) |

Sonnet is chosen because the manager needs analytical reasoning (diagnose failure causes, produce structured actions) but does not write code. Opus would be unnecessary cost.

### Diagnostic Context Package (Input)

The manager receives a comprehensive context to diagnose failures:

1. **Story details** — title, description, acceptance criteria, complexity, owned files, `split_depth`
2. **Requirement context** — original requirement text, all sibling stories and their statuses
3. **Agent logs** — full output log from the last failed attempt (`~/.vxd/logs/{story_id}.log`)
4. **Event history** — all events for this story (attempts, review feedback, error messages)
5. **Worktree state** — `git status`, `git log --oneline -5`, file listing of the worktree
6. **Dependency context** — diffs/commits from predecessor stories

### Structured Action Response (Output)

The manager returns a JSON action:

```json
{
  "diagnosis": "<human-readable explanation of what went wrong>",
  "category": "environment | structural | complexity | transient | unknown",
  "action": "retry | rewrite | split | escalate_to_techlead",
  "retry_config": {
    "target_role": "junior | intermediate | senior",
    "reset_tier": 0,
    "worktree_reset": true,
    "env_fixes": ["<natural language description of what to fix>"]
  },
  "rewrite_config": {
    "title": "...",
    "description": "...",
    "acceptance_criteria": "...",
    "complexity": 3,
    "owned_files": ["..."]
  },
  "split_config": {
    "children": [
      {
        "suffix": "a",
        "title": "...",
        "description": "...",
        "acceptance_criteria": "...",
        "complexity": 2,
        "owned_files": ["..."]
      },
      {
        "suffix": "b",
        "title": "...",
        "description": "...",
        "acceptance_criteria": "...",
        "complexity": 2,
        "owned_files": ["..."]
      }
    ],
    "dependency_edges": [
      ["{parentID}-a", "{parentID}-b"]
    ]
  }
}
```

Only the config matching the chosen `action` is populated.

### `env_fixes` Design (Deferred)

In v1, `env_fixes` is an array of natural-language strings logged for observability. The manager's diagnosis informs the retry — the fix is that the story gets re-dispatched at a different tier/role or with a rewritten description that avoids the environmental issue. Structured env_fixes (config patching, dep installation) are deferred to a future iteration when the failure taxonomy is better understood from real-world data.

### `retry_config.worktree_reset`

When `true`, the worktree is deleted and recreated from the base branch before re-dispatch. This ensures agents don't inherit a broken worktree state (bad commits, corrupt files) from a previous failed attempt. When `false`, the existing worktree is reused. Defaults to `true`.

### Manager Actions

**retry** — Reset retry counter to `reset_tier`, optionally reset the worktree, re-dispatch at `target_role`.

**rewrite** — Update the story's title, description, acceptance criteria, complexity, and/or owned files. Emit `STORY_REWRITTEN` event. Reset worktree (rewrite implies the old work is invalid). Reset `escalation_tier` to 0 in the projection (the story re-enters the escalation chain from scratch). Reset to draft for re-dispatch at tier 0 with fresh retry counters.

**split** — Decompose the failing story into smaller child stories:
- Parent story status transitions to `"split"` (new terminal status)
- Child story IDs: `{parentID}-{suffix}` where suffix comes from the split config (e.g., `01KM9HTJ-s-001-a`)
- Children inherit parent's inbound dependencies (anything parent `depends_on`)
- Stories that depended on the parent now depend on *all* children
- Inter-child dependency edges reference full child IDs (e.g., `["01KM9HTJ-s-001-a", "01KM9HTJ-s-001-b"]`)
- Children inherit parent's `split_depth + 1`
- DAG updated via the DAG mutation protocol (see below)

**escalate_to_techlead** — Skip to tier 3 when the manager recognizes the problem is structural (bad decomposition) and beyond its ability to fix.

### Split Validation

Before executing a split action, `escalation.go` validates:
1. No overlapping `owned_files` between children (same validation as planner)
2. All IDs in `dependency_edges` reference either sibling children or existing stories in the plan
3. `split_depth` of parent < 2 (max split depth)
4. Each child's complexity ≤ `MaxStoryComplexity`

If validation fails, the action is rejected and counts as a failed manager attempt.

## DAG Mutation Protocol

The in-memory DAG (`*graph.DAG`) lives inside `RunContext` and is shared across goroutines via `postExecutionPipeline()`. Concurrent mutations are unsafe.

### Serialization

All DAG mutations (splits from manager or tech_lead) are serialized through a new `dagMu sync.Mutex` on the `Monitor` struct, analogous to the existing `mergeMu`. The flow:

1. Manager returns a `split` action
2. `escalation.go` validates the split
3. Acquires `m.dagMu.Lock()`
4. Creates child story events (`STORY_CREATED` per child)
5. Emits `STORY_SPLIT` event (with `StoryID = parentStoryID`)
6. Projects all events to update SQLite
7. Mutates the DAG: `dag.AddNode(childID)` for each child, `dag.AddEdge(from, to)` for each dependency edge, rewires parent's dependents to children
8. Marks parent as `"split"` in the DAG (treated as completed for `ReadyNodes()`)
9. Releases `m.dagMu.Unlock()`

### Lock Ordering: `dagMu` and `mergeMu`

The monitor has two mutexes: `dagMu` (DAG mutations) and `mergeMu` (rebase/merge operations). These must **never be held simultaneously**. This is guaranteed by the pipeline structure:

- The merge path (`rebaseAndMerge`) only runs for stories that passed review and QA — these stories are not in an escalation state and will never trigger a split.
- The escalation path (manager/tech_lead → split/rewrite) only runs for stories that *failed* — these stories never reach the merge path in the same pipeline invocation.

Since the two paths are mutually exclusive within `postExecutionPipeline()`, no goroutine will ever need both locks. This invariant must be maintained: **never call escalation logic from within the merge path, and never call merge logic from within the escalation path.**

### `dispatchNextWave()` Completion Check

**Code location:** `internal/engine/monitor.go`, `dispatchNextWave()` function, lines ~562-567 (the `allDone` and `completed` map logic).

The current code checks `s.Status == "merged" || s.Status == "pr_submitted"`. This must be extended to include `"split"`:

```go
if s.Status == "merged" || s.Status == "pr_submitted" || s.Status == "split" {
    completed[s.ID] = true
} else {
    allDone = false
}
```

Without this change, requirements containing split stories will never complete — the parent story's `"split"` status will perpetually set `allDone = false`.

### `rc.PlannedStories` Sync After Split

When the manager or tech_lead creates child stories via a split, the new stories exist in SQLite and the DAG but not in `rc.PlannedStories`. The `DispatchWave()` function builds a `storyMap` from `rc.PlannedStories` and only dispatches stories found in that map.

**Solution:** During DAG mutation (step 7 of the dagMu block), append child stories to `rc.PlannedStories`. The `escalation.go` module receives a `*RunContext` pointer and can mutate the slice directly while holding `dagMu`. This keeps the in-memory state consistent without requiring `DispatchWave()` changes.

## Pre-Dispatch Tier Interception

Before `DispatchWave()` routes a story, the monitor must check whether the story requires inline handling (tiers 2-3) rather than agent dispatch.

### Flow in `dispatchNextWave()`

1. `DispatchWave()` returns assignments for ready stories
2. Before spawning, the monitor iterates assignments and checks each story's current tier
3. Stories at tier ≥ 2: removed from the spawn list, handled inline:
   - Tier 2: `manager.Diagnose()` called, action executed
   - Tier 3: `techLead.RePlan()` called
4. Stories at tier 0-1: spawned normally via executor

This separation ensures `routeStory()` only handles tier 0-1 routing (by complexity or to senior), and the monitor handles tier 2+ inline.

**Wave number handling:** Tier 2+ stories should be filtered out *before* calling `DispatchWave()`, not after. The monitor queries current tiers for all ready stories, removes tier 2+ stories from the candidate set, passes only tier 0-1 stories to `DispatchWave()`, and then handles tier 2+ stories inline. This avoids wave number drift from `DispatchWave()` accounting for stories it never spawns.

### `routeStory()` Changes

- Remove the existing `EventEscalationCreated` check
- Tier 0: route by complexity (existing logic)
- Tier 1: route to `RoleSenior` (check for `STORY_ESCALATED` with `to_tier=1`)
- Tiers 2+: should not reach `routeStory()` due to pre-dispatch interception. If reached (defensive), return `RoleSenior` and log a warning.

## Tech Lead Re-Plan (Tier 3)

When escalated to tier 3, the tech lead receives:
- The failing story and its full failure history
- The original requirement
- All sibling stories and their statuses

### New Method: `Planner.RePlan()`

The existing `Planner.Plan()` creates a full requirement. A new `Planner.RePlan(storyID, reqID, context)` method:
- Takes a single failing story, not a full requirement
- Emits only `STORY_CREATED` events for replacement stories (no `REQ_SUBMITTED` or `REQ_PLANNED`)
- Emits `STORY_SPLIT` for the original story (same mechanism as manager split)
- Follows the same DAG mutation protocol
- Enforces `MaxStoryComplexity` on generated stories. If the tech_lead cannot decompose below the threshold, it should split into more (smaller) stories rather than exceeding it.

Fresh retry counters start after re-plan. The tech lead gets one re-plan attempt. If the re-planned stories also exhaust their escalation chain, the requirement pauses (tier 4).

## Integration Points

### Modified: `EventFilter` in store.go

Add an `After time.Time` field:

```go
type EventFilter struct {
    Type    EventType
    AgentID string
    StoryID string
    After   time.Time  // only return events with timestamp > After
}
```

Both `FileStore.List()` and `SQLiteStore` query methods must respect this field. This enables per-tier retry counting.

### Modified: `resetStoryToDraft()` in monitor.go

Becomes tier-aware:
1. Query current tier: find max `to_tier` from `STORY_ESCALATED` events for this story
2. Find timestamp of that escalation event (or epoch zero for tier 0)
3. Count `EventStoryReviewFailed` events after that timestamp
4. If count < max for current tier: emit `EventStoryReviewFailed`, reset to draft
5. If count ≥ max: call `escalateStory(storyID, currentTier, currentTier+1, reason)`

### Modified: `postExecutionPipeline()` in monitor.go

- QA failures now call `resetStoryToDraft()` (unified with review failure path)
- Git diff errors call `resetStoryToDraft()` (already fixed in this session)

### Modified: `routeStory()` in dispatcher.go

- Remove `EventEscalationCreated` check
- Tier 0: route by complexity
- Tier 1: route to `RoleSenior`
- Tier 2+: defensive fallback (should be intercepted by monitor)

### Modified: `Project()` in sqlite.go

Handle new event types:
- `EventStoryEscalated` → update story `escalation_tier` column, insert into `escalations` table
- `EventStoryRewritten` → update story fields in `stories` table: `title`, `description`, `acceptance_criteria`, `complexity`, `owned_files`, and reset `escalation_tier` to 0
- `EventStorySplit` → create child stories, update `story_deps`, set parent status to `"split"`

### Modified: config.go

Add to `RoutingConfig`:
- `MaxSeniorRetries int` (default 2)
- `MaxManagerAttempts int` (default 2)

Add to `ModelsConfig`:
- `Manager ModelConfig`

### Modified: roles.go

Add `RoleManager` constant and `ModelConfig()` mapping.

### Schema Migrations in sqlite.go

Add to `NewSQLiteStore()` after table creation:

```sql
ALTER TABLE stories ADD COLUMN escalation_tier INTEGER DEFAULT 0;
ALTER TABLE stories ADD COLUMN split_depth INTEGER DEFAULT 0;

ALTER TABLE escalations ADD COLUMN from_tier INTEGER DEFAULT 0;
ALTER TABLE escalations ADD COLUMN to_tier INTEGER DEFAULT 0;
```

Using the existing pattern of `ALTER TABLE ... ADD COLUMN` with error suppression for idempotency (column already exists is silently ignored).

### Modified: CLI and Dashboard (variable-depth story IDs)

Split children produce IDs with additional suffixes (e.g., `01KM9HTJ-s-001-a`). Files that parse or display story IDs must handle variable-depth formats:

- `internal/cli/status.go` — story list display
- `internal/cli/escalations.go` — escalation display
- `internal/cli/helpers.go` — any ID parsing utilities
- `internal/dashboard/stories.go` — dashboard story rendering
- `internal/dashboard/escalations.go` — dashboard escalation rendering

Review each for hardcoded ID format assumptions. The safe pattern is to treat story IDs as opaque strings rather than parsing their structure.

### Modified: Planner interface (if applicable)

If a planner interface exists (for testing), it must be extended to include `RePlan()`. The `escalation.go` module calls `planner.RePlan()` for tier 3 escalation. If the planner is passed as an interface to the escalation module, the interface must include the new method.

## New Files

| File | Purpose |
|------|---------|
| `internal/engine/manager.go` | Manager agent: builds diagnostic context, calls LLM, parses structured action |
| `internal/engine/escalation.go` | Escalation state machine: tier tracking, counter management, action execution (retry/rewrite/split), DAG mutation, split validation |

## New Events

| Event | Payload | Effect |
|-------|---------|--------|
| `STORY_ESCALATED` | `StoryID` field set to story ID. Data: `{from_tier, to_tier, reason}` | Tracks tier transitions, populates `escalations` table, updates `stories.escalation_tier` |
| `STORY_REWRITTEN` | `StoryID` field set to story ID. Data: `{old_title, new_title, changes, reason}` | Audit trail, updates story fields in projection |
| `STORY_SPLIT` | `StoryID` field set to parent story ID. Data: `{child_story_ids: [...], reason}` | Creates child stories, updates DAG, marks parent as `"split"` |

Note: `STORY_SPLIT` uses the `Event.StoryID` field for the parent (consistent with all other story events), so existing filters that key on `StoryID` work correctly. Child creation uses separate `STORY_CREATED` events emitted before `STORY_SPLIT`.

## Retired Events

| Event | Replacement |
|-------|-------------|
| `ESCALATION_CREATED` | `STORY_ESCALATED` with `from_tier`/`to_tier` fields |
| `STORY_QA_FAILED` | `STORY_REVIEW_FAILED` with agent `"qa"` — unified retry signal |

Existing `ESCALATION_CREATED` and `STORY_QA_FAILED` events in the log are ignored by the new projection logic.

**QA path unification:** The QA failure path in `postExecutionPipeline()` must be changed to call `resetStoryToDraft(storyID, "qa", reason)` instead of directly emitting events. This ensures QA failures go through the tier-aware retry counter. The QA path must emit `EventStoryReviewFailed` (not `EventStoryQAFailed`) so the unified counter counts it. Remove the `EventStoryQAFailed` constant from `events.go` and remove its `case` from `Project()` in `sqlite.go`. The `"qa_failed"` story status is retired — QA failures now transition to `"draft"` (same as review failures) via `resetStoryToDraft()`.

## Error Handling & Safety

### Manager Failure Modes

| Failure | Response |
|---------|----------|
| Invalid JSON from LLM | Retry once with stricter prompt. If still invalid, count as failed attempt |
| LLM API error (rate limit, timeout) | Retry with backoff (1 attempt). If still fails, count as failed attempt |
| Fatal API error (auth, billing) | Pause requirement immediately |
| Invalid action (e.g., split validation fails) | Reject action, count as failed manager attempt |
| Manager attempts exhausted | Escalate to tier 3 (tech_lead) |

### Loop Prevention

**Manager retry loop:** Capped at `max_manager_attempts` (default 2). After that, escalate to tech_lead.

**Recursive splitting:** Stories track `split_depth` in the `stories` table (parent=0, child=1, grandchild=2). Maximum depth of 2. Beyond that, the manager must choose `rewrite` or `escalate_to_techlead` instead of `split`. If a `split` action is returned for a story at max depth, it is rejected and counts as a failed attempt.

**Tech lead re-plan loop:** One re-plan attempt per original story. If re-planned stories also exhaust their full escalation chain, pause the requirement.

**Tech lead complexity constraint:** `MaxStoryComplexity` is enforced on tech_lead re-plans. The tech_lead must decompose within the threshold. If the problem domain is intrinsically above the threshold, the tech_lead should produce more stories, not higher-complexity ones.

### Cost Guardrails

Worst case for a single story before pausing:

```
Tier 0: 2 agent runs (junior/intermediate model)
Tier 1: 2 agent runs (senior model)
Tier 2: 2 API calls (Sonnet)
Tier 3: 1 API call (Opus)
Tier 4: pause (zero cost)
```

Total: ~4 agent sessions + 3 API calls. Bounded and predictable.

## Observability

- `vxd status --req <id>` — shows story tiers, escalation state, and split relationships
- `vxd dashboard` — escalation events in the event feed
- `vxd escalations` — populated from the `escalations` table (currently exists but unpopulated)
- Manager logs — `~/.vxd/logs/{story_id}-manager.log` (same directory as agent logs, subject to same log retention)

## New Story Status

`"split"` — terminal status for stories that the manager or tech_lead decomposed into children. Split stories are not re-dispatched. Treated as "complete" for DAG dependency resolution and the `allDone` wave completion check.

## Child Story ID Convention

Split children use the suffix scheme: `{parentID}-{suffix}` where suffix is a lowercase letter from the split config (e.g., `01KM9HTJ-s-001-a`, `01KM9HTJ-s-001-b`). This creates variable-depth IDs. CLI display and log parsing must handle IDs with one or two hyphenated suffixes.

## Configuration

New fields in `vxd.yaml`:

```yaml
models:
  manager:
    provider: anthropic
    model: claude-sonnet-4-20250514
    max_tokens: 8000

routing:
  max_retries_before_escalation: 2    # tier 0 (existing)
  max_senior_retries: 2               # tier 1 (new)
  max_manager_attempts: 2             # tier 2 (new)
```

The existing `max_qa_failures_before_escalation` is retired. QA failures now count against the unified per-tier retry counter.

## Out of Scope

- Automatic cost tracking/budgeting per requirement
- Cross-requirement learning (manager doesn't learn from past escalations)
- Structured `env_fixes` execution (deferred to future iteration; v1 uses natural-language descriptions)
- UI for reviewing manager decisions (CLI observability is sufficient for now)
