# Pipeline Workflows

VXD processes every requirement through a deterministic pipeline of stages. This guide explains each stage, the events it produces, and how stories transition between states.

## Pipeline Overview

```
Requirement
    │
    ▼
┌──────────┐   REQ_SUBMITTED
│  Intake  │──────────────────► Event Store
└────┬─────┘
     │
     ▼
┌──────────┐   STORY_CREATED (×N)
│ Planning │──────────────────► Event Store
└────┬─────┘   REQ_PLANNED
     │
     ▼
┌──────────┐   AGENT_SPAWNED
│ Dispatch │   STORY_ASSIGNED ─► Event Store
└────┬─────┘   (per wave)
     │
     ▼
┌───────────┐  STORY_STARTED
│ Execution │  STORY_PROGRESS ─► Event Store
└────┬──────┘  STORY_COMPLETED
     │
     ▼
┌──────────┐   STORY_REVIEW_PASSED
│  Review  │──────────────────► Event Store
└────┬─────┘   or STORY_REVIEW_FAILED
     │
     ▼
┌──────────┐   STORY_QA_PASSED
│    QA    │──────────────────► Event Store
└────┬─────┘   or STORY_QA_FAILED
     │
     ▼
┌──────────┐   STORY_PR_CREATED
│  Merge   │   STORY_MERGED ──► Event Store
└────┬─────┘
     │
     ▼
┌──────────┐   WORKTREE_PRUNED
│ Cleanup  │   BRANCH_DELETED ─► Event Store
└──────────┘
```

## Stage 1: Intake

**Trigger:** `vxd req "<requirement>"`

The CLI accepts a natural-language requirement and emits a `REQ_SUBMITTED` event. The requirement text and repo directory are captured for the planning stage.

**Story status:** (none yet — requirement exists but has no stories)

## Stage 2: Planning

**Actor:** Tech Lead agent (Claude Opus by default)

The Planner sends the requirement to the Tech Lead LLM with a system prompt that instructs it to:
- Decompose into atomic, independently-implementable stories
- Assign Fibonacci complexity scores (1, 2, 3, 5, 8, 13)
- Identify inter-story dependencies
- Write clear acceptance criteria per story

The LLM returns structured JSON. VXD parses it into `PlannedStory` objects and builds a dependency DAG using topological sort.

**Events emitted:** `STORY_CREATED` (one per story), `REQ_PLANNED`

**Story status:** `draft`

### Dependency DAG

Stories reference each other by ID in their `depends_on` field. VXD builds a directed acyclic graph and validates it has no cycles. The DAG drives wave-based dispatch — a story can only execute once all its dependencies are `merged`.

## Stage 3: Dispatch

**Trigger:** `vxd resume <req-id>` (or automatic after planning)

The Dispatcher performs topological sort on the DAG and identifies the next **wave** — the set of stories whose dependencies are all satisfied.

### Complexity Routing

Each story is routed to an agent role based on its Fibonacci score:

| Complexity | Role | Default Model |
|------------|------|---------------|
| 1-3 | Junior | Claude Haiku / GPT-4o-mini |
| 4-5 | Intermediate | Claude Sonnet |
| 6-13 | Senior | Claude Opus |

Thresholds are configurable via `routing.junior_max_complexity` and `routing.intermediate_max_complexity`.

### Per-Story Isolation

For each assigned story, VXD:
1. Creates a git worktree at a unique path
2. Creates a feature branch: `vxd/<story-id>`
3. Spawns a tmux session running the configured AI runtime
4. Injects the role-appropriate system prompt with story context

**Events emitted:** `AGENT_SPAWNED`, `STORY_ASSIGNED`

**Story status:** `in_progress`

## Stage 4: Execution

**Actor:** Junior / Intermediate / Senior agent in tmux session

The agent works autonomously in its isolated worktree. The **Watchdog** monitors the session by:
- Fingerprinting output every `poll_interval_ms` (default: 10s)
- Detecting stuck agents (unchanged fingerprint for `stuck_threshold_s`)
- Auto-approving permission prompts (sends "Y" when `permission_pattern` matches)
- Escaping plan mode (sends Escape when `plan_mode_pattern` matches)

If an agent is stuck, VXD emits `AGENT_STUCK` and may escalate to a higher-tier agent after `max_retries_before_escalation` attempts.

**Events emitted:** `STORY_STARTED`, `STORY_PROGRESS`, `STORY_COMPLETED` (or `AGENT_STUCK`)

**Story status:** `in_progress` -> `review` (on completion)

## Stage 5: Review

**Actor:** Senior agent (Claude Sonnet by default)

The Reviewer captures the git diff from the story branch and sends it to a Senior LLM along with the story's acceptance criteria. The LLM returns a structured review:

- **Verdict:** `approve` or `request_changes`
- **Comments:** File, line, severity (critical/major/minor/info), message
- **Summary:** Overall assessment

If the review fails, the story returns to `in_progress` for the agent to address feedback. After `max_retries_before_escalation` failures, the story escalates.

**Events emitted:** `STORY_REVIEW_PASSED` or `STORY_REVIEW_FAILED`

**Story status:** `review` -> `qa` (on pass) or back to `in_progress` (on fail)

## Stage 6: QA

**Actor:** QA pipeline (configurable commands)

QA runs three sequential checks against the story's worktree:

1. **Lint** — e.g., `golangci-lint run`
2. **Build** — e.g., `go build ./...`
3. **Test** — e.g., `go test ./...`

Each check records: name, pass/fail, output, elapsed time. If any check fails, the story returns for rework. After `max_qa_failures_before_escalation` total failures, the story escalates.

**Events emitted:** `STORY_QA_STARTED`, `STORY_QA_PASSED` or `STORY_QA_FAILED`

**Story status:** `qa` -> `pr_submitted` (on pass) or back to `in_progress` (on fail)

## Stage 7: Merge

**Actor:** Merger (uses `gh` CLI)

The Merger:
1. Pushes the story branch to origin
2. Creates a PR using the configured `merge.pr_template`
3. If `merge.auto_merge` is `true`, squash-merges and deletes the source branch

**Events emitted:** `STORY_PR_CREATED`, `STORY_MERGED` (if auto-merge enabled)

**Story status:** `pr_submitted` -> `merged`

## Stage 8: Cleanup

**Actor:** Reaper

Post-merge cleanup based on config:

- **Worktree pruning:** `immediate` (delete right after merge) or `deferred` (keep until GC)
- **Branch deletion:** After `branch_retention_days` (0 = delete immediately)
- **Manual GC:** `vxd gc` scans for old branches and worktrees past retention

**Events emitted:** `WORKTREE_PRUNED`, `BRANCH_DELETED`, `GC_COMPLETED`

## Story Status Transitions

```
draft ──► in_progress ──► review ──► qa ──► pr_submitted ──► merged
              ▲               │         │
              └───────────────┘         │
              (review failed)           │
              ▲                         │
              └─────────────────────────┘
                    (QA failed)
```

## Wave Execution

Waves execute sequentially, but stories within a wave execute in parallel:

```
Wave 1: [STORY-001, STORY-002]  ── parallel ──►
                                                 Wave 2: [STORY-003]  ── parallel ──►
                                                                                      Done
```

A new wave starts only when all stories in the previous wave reach `merged` status (or are escalated/skipped).

## Event Sourcing

Every stage appends events to the immutable event store (`events.jsonl`). The SQLite projection store materializes the current state for fast queries. This means:

- **Full audit trail** — every decision is recorded
- **Replayable** — rebuild projections from events at any time
- **Queryable** — `vxd events` lets you filter by type, story, or count
