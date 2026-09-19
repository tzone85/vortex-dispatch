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
┌──────────┐   STORY_SECURITY_PASSED
│ Security │──────────────────► Event Store
└────┬─────┘   or STORY_SECURITY_FAILED
     │
     ▼
┌──────────┐   STORY_PR_CREATED
│  Merge   │   STORY_MERGED ──► Event Store
└────┬─────┘
     │
     ▼
┌──────────┐   (no events: the monitor removes
│ Cleanup  │    the worktree and branches inline)
└──────────┘
```

## Pre-Pipeline: Environment Checks

Before the pipeline starts, VXD validates the execution environment.

### Pre-Flight Validation

**Trigger:** Implicit before `vxd req` and `vxd resume`, or explicitly via `vxd preflight`

Pre-flight runs 18 checks across 3 severity tiers:

| Tier | Behavior | Checks |
|------|----------|--------|
| CRITICAL | Blocks execution | tmux installed, claude CLI available, git repo detected, LLM provider reachable |
| WARNING | Proceeds with notice | tmux server can create sessions, ANTHROPIC_API_KEY conflict, free disk space for the state dir, gh auth, network connectivity, stale tmux sessions, Google API key, binary path shadowing, security scanners installed |
| INFO | Diagnostic only | config loaded, project detected, state directory exists, billing configured, Ollama (optional) |

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

The CLI accepts a natural-language requirement, builds the planning client and hands the requirement to the Planner, which emits `REQ_SUBMITTED` before it starts decomposing. The requirement text and repo directory are captured for the planning stage. Pre-flight validation runs automatically unless `--skip-preflight` is passed.

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

On resume, VXD first acquires a lock file (prevents concurrent runs), runs a consistency check for crash recovery, then dispatches the next wave. `vxd resume` dispatches the first wave; the monitor's poll loop dispatches every later wave itself (`Monitor.dispatchNextWave`) once no agents are active. With every story complete and no verdict (or a blocked one), resume skips dispatch and re-runs the completion gate — without regenerating the documentation, which the earlier pass already did — and exits 1 if the requirement stays blocked or the gate reaches no verdict.

The Dispatcher performs topological sort on the DAG and identifies the next **wave** — the set of stories whose dependencies are all satisfied.

### Complexity Routing

Each story is routed to an agent role based on its Fibonacci score:

| Complexity | Role | Model binding (default) |
|------------|------|-------------------------|
| 1-3 | Junior | `models.junior` (`anthropic` / `claude-haiku-4-5`) |
| 4-5 | Intermediate | `models.intermediate` (`anthropic` / `claude-haiku-4-5`) |
| 6-13 | Senior | `models.senior` (`anthropic` / `claude-opus-4-7`) |

Thresholds are configurable via `routing.junior_max_complexity` and `routing.intermediate_max_complexity`. Every role defaults to Anthropic. Gemma on Google AI Studio's free tier is opt-in: set a role's `provider` to `google`, and VXD falls back to Claude when the quota runs out (see the [Gemma 4 Guide](gemma-4-guide.md)).

### Per-Story Isolation

The Dispatcher emits `AGENT_SPAWNED` and `STORY_ASSIGNED` for each story in the wave. For each assignment, the Executor:
1. Creates a git worktree at a unique path
2. Creates a feature branch: `vxd/<story-id>`
3. Builds the role-appropriate prompt with story context and prior wave context
4. Calls the runtime's `Spawn`: the Adapter prepares the command (pure function) and the Runner starts it (tmux session, Docker container, or SSH). `Spawn` writes CLAUDE.md and AGENTS.md into the worktree on every spawn; the Executor does not write them.
5. Emits `STORY_STARTED`

**Events emitted:** `AGENT_SPAWNED`, `STORY_ASSIGNED` (Dispatcher), `STORY_STARTED` (Executor)

**Story status:** `draft` -> `assigned` -> `in_progress`

## Stage 4: Execution

**Actor:** Junior / Intermediate / Senior agent in tmux session

The agent works autonomously in its isolated worktree. The monitor polls every `poll_interval_ms` (default: 10s). On each pass it runs the **Watchdog** check for the session:
- Fingerprinting output and detecting stuck agents (unchanged fingerprint for `stuck_threshold_s`)
- Auto-approving permission prompts (sends "Y" when `permission_pattern` matches)
- Escaping plan mode (sends Escape when `plan_mode_pattern` matches)

A stuck agent produces an `AGENT_STUCK` event; the Watchdog does not stop the agent. On the same pass the monitor asks the runtime for the session status. When the runtime reports the session done or terminated, the monitor (not the Watchdog) emits `STORY_COMPLETED` and starts the post-execution pipeline: review, QA, security gate, merge.

When a failed story is re-dispatched, smart retry analyzes the error output into 8 categories (missing_symbol, syntax, type_error, import, test_failure, build_config, environment, timeout) and gives the retry agent targeted fix suggestions.

**Events emitted:** `STORY_PROGRESS`, `AGENT_STUCK` (Watchdog), `STORY_COMPLETED` (monitor)

**Story status:** `in_progress` -> `review` (on completion)

## Stage 5: Review

**Actor:** Reviewer (LLM code review)

The Reviewer captures the git diff from the story branch and sends it to a Senior LLM along with the story's acceptance criteria. The LLM returns a structured review:

- **Verdict:** `approve` or `request_changes`
- **Comments:** File, line, severity (critical/major/minor/info), message
- **Summary:** Overall assessment

If the review fails, the monitor resets the story to `draft` and the dispatcher re-dispatches it with the review feedback. After `max_retries_before_escalation` failures at the current tier, the story escalates. See Story Status Transitions for how every reset is recorded.

**Events emitted:** `STORY_REVIEW_PASSED` or `STORY_REVIEW_FAILED`

**Story status:** `review` -> `qa` (on pass) or back to `draft` (on fail)

## Stage 6: QA

**Actor:** QA pipeline (configurable commands)

QA runs three sequential checks against the story's worktree:

1. **Lint** — e.g., `golangci-lint run`
2. **Build** — e.g., `go build ./...`
3. **Test** — e.g., `go test ./...`

Each check records: name, pass/fail, output, elapsed time. If any check fails, the monitor resets the story to `draft` for rework. After `max_qa_failures_before_escalation` total failures, the story escalates.

**Events emitted:** `STORY_QA_STARTED`, `STORY_QA_PASSED` or `STORY_QA_FAILED`

**Story status:** `qa` -> `pr_submitted` (on pass) or back to `draft` (on fail)

## Stage 6.2: Security Gate

**Actor:** Security gate (scanners plus an LLM threat-model review of the diff)

After QA passes and before any rebase or PR, the monitor runs the security gate on the story's worktree. It is on by default; set `security.disable_gate: true` to turn it off. It does not run in dry-run mode.

| Outcome | What happens |
|---------|--------------|
| No finding at or above `security.gate_severity` (default `critical`) | `STORY_SECURITY_PASSED`; continue to merge |
| Finding at or above the gate severity | `STORY_SECURITY_FAILED`, then `REQ_PAUSED`. The story is not reset or escalated. The pause message suggests fixing it on the branch or running `vxd resume <req-id> --godmode` |
| Capacity or session limit during the review | `REQ_PAUSED`; resume after the limit resets |
| Any other gate error | Logged; the merge continues |

**Events emitted:** `STORY_SECURITY_PASSED` or `STORY_SECURITY_FAILED`

## Stage 6.5: PR Review (Human Review Gates)

In `manual` review mode, after the security gate the monitor rebases the branch, pushes it and opens a PR without merging, then waits for human approval:

```bash
# See pending stories awaiting approval
vxd review <story-id>

# Approve a single story's PR
vxd approve <story-id>

# Reject a story's PR (returns to draft)
vxd reject <story-id>

# Batch approve all pending stories for a requirement
vxd approve --all <req-id>
```

**Events emitted:** `STORY_AWAITING_APPROVAL`, `STORY_APPROVED` or `STORY_REJECTED`

**Story status:** `awaiting_approval` -> `approved` -> `merged` (on approve) or `draft` (on reject)

## Stage 7: Merge

**Actor:** Monitor, then Merger (uses `gh` CLI)

Merges run one at a time: the monitor holds a merge lock from the rebase through post-merge cleanup. In `auto` or `plan_only` mode it:
1. Fetches the base branch and rebases the story branch onto `origin/<base>` (with LLM conflict resolution when a resolver is configured)
2. Re-runs the QA command checks on the rebased branch and blocks the merge if the story turns a green base red (turn off with `qa.disable_pre_merge_verify`)
3. Hands off to the Merger, which pushes the branch and creates a PR using `merge.pr_template`
4. Squash-merges the PR when `merge.auto_merge` is true

In `manual` mode the flow stops once the PR exists (see Stage 6.5). A rebase, pre-merge QA, push, PR or merge error resets the story to `draft`, like a review failure.

**Events emitted:** `STORY_PR_CREATED`, `STORY_MERGED` (if auto-merged or approved)

**Story status:** `pr_submitted` -> `merged` (or `awaiting_approval` in manual mode)

## Stage 8: Cleanup

**Actor:** Monitor (inline, right after a successful merge)

1. Records what the story built into the wave context for later stories
2. Removes the worktree and deletes the local branch (`git worktree remove --force`, `git branch -D`)
3. Deletes the remote branch
4. Runs a post-merge integration build on the base branch when a tech-lead fixer is configured; a failure emits `STORY_INTEGRATION_FAILED` and dispatches a fix
5. Enforces the per-requirement budget cap (`billing.max_usd_per_req`)

The monitor's poll loop then dispatches the next wave, unless the requirement is paused. This inline cleanup emits no events and does not read `cleanup.worktree_prune`.

**`vxd gc`:** the Reaper runs only from `vxd gc`. It deletes branches of merged stories older than `cleanup.branch_retention_days` and emits `BRANCH_DELETED` and `GC_COMPLETED`. The same command removes log files older than `log_retention_days`.

## Story Status Transitions

```
draft ─► assigned ─► in_progress ─► review ─► qa ─► pr_submitted ─► merged
                                                          │
                                          (manual mode)   └─► awaiting_approval ─► approved ─► merged
```

Failures never send a story back to `in_progress`. The monitor resets it to `draft` so the dispatcher picks it up again, and records the reset as `STORY_REVIEW_FAILED` whatever stage failed. The event's `reason` payload says what actually happened.

| Failure | Events |
|---------|--------|
| No code changes, conflict markers, or git diff error | `STORY_REVIEW_FAILED` |
| Review rejected or review error | `STORY_REVIEW_FAILED` |
| QA failed or QA error | `STORY_QA_FAILED` (from QA, on a failed check) + `STORY_REVIEW_FAILED` |
| Rebase, pre-merge QA, PR or merge error | `STORY_REVIEW_FAILED` |
| Pipeline timeout | `STORY_REVIEW_FAILED` |
| PR rejected with `vxd reject` | `STORY_REJECTED` |

When the current tier's retry limit is reached, the monitor emits `STORY_ESCALATED` before the same `STORY_REVIEW_FAILED` reset. Manager (tier 2) and Tech Lead (tier 3) handling can also emit `STORY_REWRITTEN` or `STORY_SPLIT` (the parent moves to `split`).

`paused` is a requirement status, not a story status. A security finding, a capacity or session limit, a fatal API error, the budget cap, or an exhausted escalation chain emits `REQ_PAUSED` and leaves the story's status as it was.

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
