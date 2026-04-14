<!--
Sync Impact Report
═══════════════════
Version change: N/A → 1.0.0 (initial ratification)
Modified principles: N/A (all new)
Added sections:
  - Core Principles (7 principles)
  - Technology Constraints
  - Development Workflow
  - Governance
Removed sections: N/A
Templates requiring updates:
  - .specify/templates/plan-template.md ✅ compatible (Constitution Check
    section already references this file generically)
  - .specify/templates/spec-template.md ✅ compatible (no direct
    constitution references)
  - .specify/templates/tasks-template.md ✅ compatible (no direct
    constitution references)
  - .specify/templates/commands/*.md ✅ no command files exist
Follow-up TODOs: none
-->

# VXD (Vortex Dispatch) Constitution

## Core Principles

### I. Event-Sourced Truth (NON-NEGOTIABLE)

The append-only event log (`events.jsonl`) is the sole source of truth
for all state in VXD. SQLite projections are derived materialized views
and MUST be rebuildable from the event log at any time.

- Events MUST be immutable once appended. No event is ever modified or
  deleted.
- Every state transition in the pipeline MUST emit a typed event before
  the transition is considered complete.
- New projection fields require both a schema migration (`ALTER TABLE
  ADD COLUMN`) and a replay of historical events to backfill existing
  rows. `CREATE TABLE IF NOT EXISTS` alone is insufficient.
- Silent state changes are forbidden. If a story changes status, an
  event MUST be emitted — including failure and rollback transitions
  (e.g., `STORY_REVIEW_FAILED` when a diff is empty).
- Zero-value struct fields MUST NOT silently disable downstream logic.
  Guard against uninitialized fields that act as false negatives.

### II. CLI-First Interface

Every VXD feature MUST be accessible through the `vxd` CLI. The CLI is
the primary user interface; dashboards (TUI and web) are secondary
views.

- Commands follow the pattern `vxd <noun> [--flags]` with positional
  arguments for primary inputs.
- Output MUST support both human-readable (default) and structured
  formats where applicable.
- Complex inputs (requirements, specs) MUST be accepted from files
  (`--file`) and stdin in addition to inline arguments.
- Exit codes MUST distinguish success (0) from user error (1) from
  internal failure (2).

### III. Pluggable Runtimes

AI agent runtimes (Claude Code, Codex, Gemini CLI) are declared in
`vxd.yaml`, not in source code. The runtime registry loads
configurations at startup and dispatches work through a uniform
interface.

- Adding a new runtime MUST NOT require changes to any package outside
  `internal/runtime/`.
- Runtime configuration is YAML-only: command, args, model list, and
  detection patterns.
- The core engine (`internal/engine/`) MUST interact with runtimes
  exclusively through the `runtime.Runtime` interface.
- Runtime-specific workarounds (e.g., stdin pipe behavior in tmux) MUST
  be encapsulated within the runtime adapter, never leaked into engine
  logic.

### IV. Autonomous End-to-End Pipeline

VXD MUST support fully autonomous operation from requirement submission
to merged PR without human intervention when godmode is enabled.

- Each pipeline stage (intake → planning → dispatch → execution →
  review → QA → merge → cleanup) MUST be self-contained and
  recoverable.
- Failure at any stage MUST emit an explicit event and either retry,
  escalate, or pause — never silently drop work.
- The `--godmode` flag and `planning.godmode` config MUST be the only
  controls for human-in-the-loop toggling. No other implicit approval
  gates may be introduced.
- Pipeline resumption (`vxd resume`) MUST pick up from the last
  successful stage, not restart from the beginning.

### V. Complexity-Routed Agent Hierarchy

Stories are routed to agent tiers based on Fibonacci complexity scoring.
Escalation follows a strict 5-tier chain with no tier-skipping.

- Routing thresholds: Junior (1-2), Intermediate (3-5), Senior (5-8),
  Tech Lead (13+). Thresholds are configurable in `vxd.yaml` under
  `routing`.
- Escalation chain: same-role retry → senior → manager diagnosis →
  tech_lead re-plan → pause. Each tier MUST be attempted before
  advancing.
- Escalation events (`STORY_ESCALATED`, `STORY_REWRITTEN`,
  `STORY_SPLIT`) MUST be emitted at each tier transition.
- Agent reputation scoring MUST influence future routing but MUST NOT
  override complexity-based tier assignment.
- Manager-tier uses a mid-range model (Sonnet) for diagnosis;
  Tech Lead uses the highest-capability model (Opus) for re-planning.

### VI. Observability

Every state transition MUST be observable through at least one of:
event queries, CLI output, or dashboard views.

- The watchdog MUST detect stuck agents (exceeded time threshold) and
  emit monitoring events without manual triggering.
- Dashboards (TUI via Bubbletea, web via WebSocket) MUST reflect
  real-time state from SQLite projections.
- The event store MUST support filtering by type, story, and
  recency (`vxd events --type T --story S --limit N`).
- Error messages in CLI output MUST NOT leak internal implementation
  details (stack traces, file paths) to the user. Detailed context
  MUST be logged to the event store or log files.

### VII. Immutability and Small Modules

Functions MUST create new values rather than mutating inputs. Packages
MUST follow the single-responsibility principle.

- Data structures passed between packages MUST be treated as immutable.
  Create updated copies instead of modifying in place.
- Source files MUST target 200-400 lines. Files exceeding 800 lines
  MUST be split.
- Functions MUST NOT exceed 50 lines. Extract sub-functions when
  complexity grows.
- Nesting depth MUST NOT exceed 4 levels. Refactor with early returns
  or extracted helpers.
- No hardcoded values in logic — use constants, config, or `vxd.yaml`.

## Technology Constraints

- **Language**: Go 1.23+ (standard library preferred over third-party
  dependencies)
- **State storage**: Append-only JSONL for the event log; SQLite for
  materialized projections
- **Session management**: tmux for agent process isolation; sessions
  MUST be cleaned up (killed) before re-creation to prevent duplicates
- **TUI framework**: Bubbletea (single-pane, no tabs — all sections
  visible simultaneously)
- **Web dashboard**: Vanilla HTML, CSS, and JavaScript with WebSocket
  for real-time updates; no frontend build toolchain
- **Build target**: `go build -o ~/.local/bin/vxd ./cmd/vxd` — the
  binary MUST be installed to `~/.local/bin/`, never `~/go/bin/`
- **Configuration**: YAML (`vxd.yaml`) for all user-facing settings;
  no environment variable overrides except `ANTHROPIC_API_KEY` as a
  fallback for users without Claude Code CLI
- **VCS integration**: GitHub via `gh` CLI for PR creation and
  auto-merge

## Development Workflow

- **Test-Driven Development** is mandatory: write tests first (RED),
  verify they fail, implement minimally (GREEN), then refactor. Target
  80%+ coverage.
- **Conventional commits**: `<type>: <description>` where type is one
  of feat, fix, refactor, docs, test, chore, perf, ci.
- **Code review**: Automated via code-reviewer and security-reviewer
  agents. CRITICAL and HIGH issues MUST be resolved before merge.
- **Feature branches**: All work on feature branches; merge to main
  via PR. No direct commits to main.
- **Environment discipline**: `unset CLAUDECODE` before spawning
  agents. API keys MUST be explicitly exported in `Spawn()` — tmux
  does not inherit parent environment.
- **Binary verification**: After every `go build`, verify with
  `which vxd` that the correct binary is on PATH. A running process
  does not pick up rebuilt binaries — restart after rebuilding.

## Governance

This constitution is the highest-authority document for VXD development
decisions. When a practice conflicts with a principle defined here, the
constitution prevails.

- **Amendment procedure**: Any change to this constitution MUST be
  documented with a version bump, a Sync Impact Report (HTML comment
  at file top), and propagation checks across dependent templates.
- **Versioning policy**: Semantic versioning applies. MAJOR for
  principle removal or incompatible redefinition; MINOR for new
  principles or material expansion; PATCH for wording clarifications.
- **Compliance review**: All implementation plans MUST pass a
  Constitution Check gate (see `plan-template.md`) before Phase 0
  research begins and again after Phase 1 design.
- **Runtime guidance**: Agent-specific behavior (prompts, worktree
  setup, CLAUDE.md injection) is managed outside this constitution in
  per-project configuration files. This constitution governs
  architectural principles, not operational scripts.

**Version**: 1.0.0 | **Ratified**: 2026-04-01 | **Last Amended**: 2026-04-01
