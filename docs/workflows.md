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

## Pre-Pipeline: Environment Checks

Before the pipeline starts, VXD validates the execution environment.

### Pre-Flight Validation

**Trigger:** Implicit before `vxd req` and `vxd resume`, or explicitly via `vxd preflight`

Pre-flight runs 12 checks across 3 severity tiers:

| Tier | Behavior | Checks |
|------|----------|--------|
| CRITICAL | Blocks execution | tmux installed, claude CLI available, git repo detected, LLM provider reachable |
| WARNING | Proceeds with notice | gh auth, network connectivity, stale tmux sessions, Google API key |
| INFO | Diagnostic only | config loaded, project detected, state directory exists, billing configured |

Use `--skip-preflight` on `vxd req` or `vxd resume` to bypass these checks.

### Cost Estimation

**Trigger:** `vxd estimate "<requirement>"` (standalone, does not start the pipeline)

Estimate the cost before committing to a run:

```bash
# Quick heuristic (no LLM call, instant)
vxd estimate "Build a REST API with CRUD" --quick

# Full LLM-based estimate with per-story breakdown
vxd estimate "Build a REST API with CRUD"

# Output as JSON, override hourly rate
vxd estimate "Build a REST API with CRUD" --json --rate 200

# Save the estimate as a REQ_ESTIMATED event
vxd estimate "Build a REST API with CRUD" --save
```

The estimator uses a Fibonacci-to-hours mapping (configurable in `billing` config): 1 -> 0.5h, 2 -> 1h, 3 -> 2h, 5 -> 4h, 8 -> 8h, 13 -> 16h.

## Stage 1: Intake

**Trigger:** `vxd req "<requirement>"`

The CLI accepts a natural-language requirement and emits a `REQ_SUBMITTED` event. The requirement text and repo directory are captured for the planning stage. Pre-flight validation runs automatically unless `--skip-preflight` is passed.

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

## Stage 2.5: Plan Review (Human Review Gates)

When running in `plan_only` or `manual` review mode, the pipeline pauses after planning and waits for human approval before dispatching.

```bash
# Start with review mode
vxd resume <req-id> --review

# Review the plan
vxd status --req <req-id>

# Approve or reject
vxd approve-plan <req-id>
vxd reject-plan <req-id>
```

Three review modes control pipeline behavior:

| Mode | Plan Gate | PR Gate |
|------|-----------|---------|
| `auto` | Skip | Auto-merge |
| `plan_only` | Require human approval | Auto-merge |
| `manual` | Require human approval | Require human approval |

Set the mode via:
- CLI flags: `vxd resume --review` (manual) or `vxd resume --auto` (auto)
- Config: `merge.review_mode` in `vxd.yaml`
- Default: `auto` if `merge.auto_merge` is true, otherwise `manual`

**Events emitted:** `REVIEW_MODE_SET`, `PLAN_APPROVED` or `PLAN_REJECTED`

## Stage 3: Dispatch

**Trigger:** `vxd resume <req-id>` (or automatic after planning/approval)

On resume, VXD first acquires a lock file (prevents concurrent runs), runs a consistency check for crash recovery, then dispatches the next wave.

The Dispatcher performs topological sort on the DAG and identifies the next **wave** — the set of stories whose dependencies are all satisfied.

### Complexity Routing

Each story is routed to an agent role based on its Fibonacci score:

| Complexity | Role | Default Model |
|------------|------|---------------|
| 1-3 | Junior | Google Gemma 4 (free tier) / Claude Haiku |
| 4-5 | Intermediate | Google Gemma 4 / Claude Sonnet |
| 6-13 | Senior | Claude Opus |

Thresholds are configurable via `routing.junior_max_complexity` and `routing.intermediate_max_complexity`. Google AI Studio free tier is used for execution roles (Junior, Intermediate), with Anthropic Claude as fallback on quota exhaustion.

### Per-Story Isolation

For each assigned story, the Executor:
1. Creates a git worktree at a unique path
2. Creates a feature branch: `vxd/<story-id>`
3. Writes CLAUDE.md and WAVE_CONTEXT.md into the worktree
4. Uses the Adapter to prepare the command (pure function)
5. Uses the Runner to spawn execution (tmux session, Docker container, or SSH)
6. Injects the role-appropriate system prompt with story context and prior wave context

**Events emitted:** `AGENT_SPAWNED`, `STORY_ASSIGNED`

**Story status:** `in_progress`

## Stage 4: Execution

**Actor:** Junior / Intermediate / Senior agent in tmux session

The agent works autonomously in its isolated worktree. The **Watchdog** monitors the session by:
- Fingerprinting output every `poll_interval_ms` (default: 10s)
- Detecting stuck agents (unchanged fingerprint for `stuck_threshold_s`)
- Auto-approving permission prompts (sends "Y" when `permission_pattern` matches)
- Escaping plan mode (sends Escape when `plan_mode_pattern` matches)

If an agent is stuck, VXD emits `AGENT_STUCK` and triggers the escalation chain. Smart retry analyzes the error output into 8 categories (missing_symbol, syntax, type_error, import, test_failure, build_config, environment, timeout) and provides targeted fix suggestions to the retry agent.

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

## Stage 6.5: PR Review (Human Review Gates)

In `manual` review mode, the pipeline pauses after PR creation and waits for human approval:

```bash
# See pending stories awaiting approval
vxd review <story-id>

# Approve a single story's PR
vxd approve <story-id>

# Reject a story's PR (returns to in_progress)
vxd reject <story-id>

# Batch approve all pending stories for a requirement
vxd approve --all <req-id>
```

**Events emitted:** `STORY_AWAITING_APPROVAL`, `STORY_APPROVED` or `STORY_REJECTED`

**Story status:** `awaiting_approval` -> `merged` (on approve) or `in_progress` (on reject)

## Stage 7: Merge

**Actor:** Merger (uses `gh` CLI)

The Merger:
1. Pushes the story branch to origin
2. Creates a PR using the configured `merge.pr_template`
3. In `auto` or `plan_only` mode: squash-merges and deletes the source branch
4. In `manual` mode: pauses and waits for `vxd approve` before merging

**Events emitted:** `STORY_PR_CREATED`, `STORY_MERGED` (if auto-merge or approved)

**Story status:** `pr_submitted` -> `merged` (or `awaiting_approval` in manual mode)

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
              ▲               │         │                        ▲
              └───────────────┘         │                        │
              (review failed)           │     awaiting_approval ─┘
              ▲                         │         ▲         │
              └─────────────────────────┘         │         │
              ▲   (QA failed)                     │         ▼
              │                              (PR gate)   in_progress
              │                                         (rejected)
              └─── (escalation: retry / rewrite / split)
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

## Post-Pipeline: Reporting and Analysis

### Client Delivery Reports

After a requirement completes, generate a delivery report:

```bash
# Markdown report to stdout
vxd report <req-id>

# HTML report saved to file
vxd report <req-id> --html --output report.html

# Internal report (includes agent performance, full timeline, story details)
vxd report <req-id> --internal
```

Client reports include: header, requirement summary, deliverables, timeline, and effort summary. Internal reports add: per-story agent performance, escalation/retry counts, and full event timeline.

Status classification: `DONE`, `DONE_WITH_CONCERNS` (stories had escalations), `BLOCKED`, `NEEDS_CONTEXT`.

### Pipeline Metrics

View aggregate performance metrics:

```bash
vxd metrics [--req <req-id>]
```

Metrics include: success rates, first-pass rate, average timing per phase (planning, execution, review, QA), escalation breakdown by tier, and agent activity from trace analysis (tool calls, file edits, file creates, commands, errors, tests).

### Repo Learning

Build a persistent profile of any repository that agents consume at dispatch time:

```bash
# Learn the current repo (all passes)
vxd learn

# Learn a specific repo
vxd learn /path/to/repo

# Run a specific pass only
vxd learn --pass 1

# Force re-run of all passes
vxd learn --force
```

Three analysis passes:
1. **Static scan** — marker files, configs, directory tree, CI detection (no git, no LLM)
2. **Git history** — commit patterns, contributors, churn hotspots
3. **Deep analysis** — LLM-assisted summary and architectural notes

The resulting `repo-profile.json` is injected into agent prompts to eliminate early-iteration codebase archaeology.
