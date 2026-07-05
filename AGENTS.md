# VXD (Vortex Dispatch) — AI Agent Orchestration System

> **Doc precedence:** `CLAUDE.md` is the canonical, test-enforced project doc
> (`TestDocCoverage_*` verifies CLI commands + config fields against CLAUDE.md
> and README.md). AGENTS.md is the condensed mirror read by non-Claude agent
> CLIs (Codex, Gemini). When the two disagree, CLAUDE.md + code win — update
> this file to match, never the other way round.

## What This Is
VXD orchestrates AI coding agents (Claude Code, Codex, Gemini CLI) to autonomously implement software requirements. It decomposes requirements into stories, dispatches agents in parallel via tmux sessions, monitors progress, runs QA, and merges PRs — with a 5-tier escalation chain for failures.

## Build Commands
```bash
# CRITICAL: Always build to ~/.local/bin/ (NOT ~/go/bin/)
go build -o ~/.local/bin/vxd ./cmd/vxd

# Run tests (CI runs the full module with -race; improve/ is included)
go test ./... -count=1

# NXD (public Ollama version) — at ~/Sites/misc/nexus-dispatch
cd ~/Sites/misc/nexus-dispatch && go build -o ~/.local/bin/nxd ./cmd/nxd/
```

## Architecture

### Pipeline Flow
```
vxd req "requirement" → TechLead decomposes into stories (Fibonacci complexity)
  → Dispatcher assigns to agents by tier (junior/intermediate/senior)
  → Executor creates git worktrees + spawns tmux sessions
  → Monitor polls agent status every 10s
  → Agent finishes → post-execution pipeline:
    → Code Review (LLM-based) → QA (lint/build/test + declarative criteria)
    → Merge (rebase → push → PR → squash-merge)
  → Auto-resume: dispatch next wave of ready stories
  → All stories done → requirement complete
```

### Escalation Chain (5 tiers)
```
Tier 0: Same-role retry with smart error analysis (8 categories)
Tier 1: Senior developer (more capable model)
Tier 2: Manager diagnosis (LLM analyzes failure pattern)
Tier 3: Tech Lead re-planning (decompose into smaller stories)
Tier 4: Pause (human intervention required)
```

### Critical Events
- `STORY_ESCALATED` — story moved to next tier (from_tier, to_tier, reason)
- `STORY_REWRITTEN` — manager rewrote story description/acceptance criteria
- `STORY_SPLIT` — tech lead decomposed into child stories
- `STORY_SLA_BREACHED` — story exceeded per-complexity duration limit (configurable via `sla.max_minutes_per_complexity`)
- `REQ_BLOCKED`, `STORY_SECURITY_PASSED/FAILED`, `SECURITY_SCAN_COMPLETED`, `SECURITY_RULE_LEARNED` — see CLAUDE.md "Critical Events" for the full, current list

### Event Sourcing
- **Source of truth**: `events.jsonl` (append-only, fsync'd)
- **Materialized views**: SQLite with WAL mode
- **CRITICAL**: New event types MUST be handled in `sqlite.go Project()` switch — the `default` case silently ignores them. Always add a wiring test.

### Key Packages
| Package | Purpose |
|---------|---------|
| `internal/engine/` | Core pipeline: monitor, executor, dispatcher, escalation, QA, merger, planner, reviewer |
| `internal/agent/` | Prompt templates, role definitions, diagnostic playbooks |
| `internal/runtime/` | Runtime interface, CLI adapter, tmux runner, input sanitization |
| `internal/state/` | Event store (JSONL), SQLite projections, domain models |
| `internal/cli/` | All CLI commands (cobra) |
| `internal/config/` | YAML config loading, validation, defaults |
| `internal/git/` | Worktree management, PR creation, branch operations |
| `internal/tmux/` | Tmux session lifecycle |
| `internal/llm/` | LLM clients (Anthropic, Google AI, OpenAI, CLI, fallback) |
| `internal/improve/` | Self-improvement engine — EXPERIMENTAL: 0 actionable findings to date, retained as research scaffolding (see CLAUDE.md + README honesty note) |
| `internal/web/` | Web dashboard (WebSocket, embedded static files) |
| `internal/dashboard/` | TUI dashboard (Bubbletea) |
| `internal/preflight/` | Pre-flight validation checks |
| `internal/memory/` | Memory dashboard + MemPalace integration |
| `internal/repolearn/` | Repo Learning System: 3-pass analysis (static, git history, LLM) of tracked repos |

### Config (vxd.yaml)
```yaml
workspace:
  state_dir: ~/.vxd
  backend: sqlite
models:
  tech_lead: {provider: anthropic, model: claude-opus-4-8}
  senior: {provider: anthropic, model: claude-opus-4-7}
  junior: {provider: anthropic, model: claude-haiku-4-5}  # was google/gemma-4-27b-it — a 404 model that killed the junior tier
routing:
  junior_max_complexity: 3
  max_retries_before_escalation: 2
planning:
  max_story_complexity: 5
  design_approach: ddd-tdd  # ddd-tdd | tdd | standard
merge:
  auto_merge: true
  review_mode: auto  # auto | manual | plan_only
  base_branch: main
qa:
  success_criteria:  # declarative checks (AgentFlow-inspired)
    - kind: output_contains
      value: "PASS"
    - kind: file_exists
      path: coverage.html
billing:
  default_rate: 150.0
  currency: USD
```

## CLI Commands
| Command | Purpose |
|---------|---------|
| `vxd init` | Initialize workspace, create `~/.vxd/`, generate default `vxd.yaml` |
| `vxd req "requirement"` | Submit new requirement for autonomous implementation |
| `vxd resume <req-id>` | Resume paused pipeline (has lock file + crash recovery) |
| `vxd status` | Show requirement and story status |
| `vxd pause <req-id>` | Pause a running requirement |
| `vxd dashboard` | TUI dashboard (`--web` for browser version) |
| `vxd metrics` | Success rates, timing, escalations, SLA breaches per requirement |
| `vxd estimate "req"` | Cost estimation with `--quick`, `--json`, `--rate` |
| `vxd report <req-id>` | Client delivery report (`--html`, `--internal`) |
| `vxd preflight` | Run 18 pre-flight checks before dispatch |
| `vxd approve/reject` | Human review gates for PRs |
| `vxd approve-plan` | Approve story plan before dispatch |
| `vxd reject-plan` | Reject story plan |
| `vxd review` | View PR diff and approve/reject |
| `vxd reject` | Reject a story's PR |
| `vxd projects` | List all projects |
| `vxd agents` | List active agents |
| `vxd events` | View event log |
| `vxd escalations` | List escalation events |
| `vxd config show\|validate` | View or validate configuration |
| `vxd archive` | Archive completed requirements |
| `vxd memory` | Launch memory dashboard |
| `vxd opportunity` | Manage opportunity pipeline |
| `vxd learn [path]` | Run repo analysis (`--force`, `--pass 1\|2\|3`, `--json`) |
| `vxd backup` | Create tar.gz archive of project state (`--output DIR`) |
| `vxd gc` | Garbage-collect branches + expired logs |
| `vxd improve log` | Browse improvement changelog (`--disposition`, `--category`, `--since`, `--errors`) |
| `vxd improve runs` | Show daily run summaries (findings, PRs, email status) |
| `vxd improve detail <id>` | Full details of a specific finding (reasoning, errors, PR) |
| `vxd watch [req-id]` | Tail live events for one requirement (defaults to newest in this repo) |
| `vxd retry <story-id>` | Reset a story's escalation tier and re-queue to draft (`--reason`) |
| `vxd logs <req-id>` | Print daemon log captured when `vxd req --background` self-daemonized |
| `vxd security scan\|kb` | Run the security agent on a repo / show the knowledge base |
| `vxd figma auth\|status` | One-time Figma token session / show Figma access status |
| `vxd autoresearch start\|stop\|status\|hypotheses\|evolve` | Karpathy-style experiment loop per repo (evolve opens a human-gated program.md PR) |
| `vxd db list\|connect\|sql\|schema\|delete\|gc\|ping\|template` | Manage ephemeral story databases (devdb) |
| `vxd template create\|list` | Create/list devdb template databases |

See CLAUDE.md's CLI Commands table for the authoritative, flag-level detail.

## Documentation Requirements (MANDATORY)

**Every behavioral change MUST update documentation.** This is enforced by `TestDocCoverage_*` wiring tests in `engine/doc_coverage_test.go`.

### What Counts as Behavioral
- New CLI command → add to CLAUDE.md CLI Commands table + README.md (mirror here)
- New config field → add to CLAUDE.md Config section + README.md Configuration table
- New event type → add to CLAUDE.md Architecture section if user-facing
- Changed default values → update all docs referencing the old default
- New API endpoint → add to README.md + architecture overview

### What Does NOT Need Doc Updates
- Internal refactoring (no user-visible change)
- Test-only changes
- Performance improvements (unless they change behavior)
- Bug fixes that restore documented behavior

### Enforcement
1. `TestDocCoverage_CLICommands` — verifies every `newXxxCmd()` in `internal/cli/root.go` appears in CLAUDE.md (AGENTS.md is a mirror, not the enforcement target)
2. `TestDocCoverage_ConfigSections` — verifies every top-level Config struct field appears in README.md Configuration table
3. Pre-commit awareness: if you add a new command or config field, update docs BEFORE committing

## Spec-Driven Development (SDD)
Spec-kit is installed (`.specify/`). For new features:
1. Use `speckit.specify` skill or `.specify/templates/spec-template.md` to create specs
2. Write Given/When/Then acceptance scenarios BEFORE implementation
3. Constitution at `.specify/memory/constitution.md` governs all architectural decisions
4. Specs go to `docs/superpowers/specs/` (hook-allowed path)
5. Retroactive spec for agentflow features: `docs/superpowers/specs/2026-04-11-agentflow-integration-spec.md`

## Testing Conventions
- TDD mandatory: write tests first
- Wiring tests in `engine/wiring_test.go` — verify features are ACTIVATED, not just implemented
- 30+ wiring tests guard all behavioral features
- Test pattern: pure functions for logic, thin adapters for I/O
- `package engine` (internal tests) preferred over `engine_test` (external)

## Key Design Decisions
1. **Event sourcing over CRUD** — full audit trail, replay capability, temporal queries
2. **SQLite WAL mode** — concurrent readers without blocking writers
3. **Tmux sessions** — agents survive monitor crashes, can be inspected live
4. **Worktrees** — file isolation between parallel agents, shared git objects
5. **Prompt-via-file** — write to `.vxd-prompts/prompt.txt`, pass via `$(cat)` to avoid shell escaping
6. **CLAUDE.md + AGENTS.md in worktrees** — VXD dual-writes the directive on every spawn so Claude Code (CLAUDE.md), Codex CLI (AGENTS.md), and Gemini CLI all see the no-brainstorm instructions. Both files are stripped from the branch before merge so the project's own versions survive.
7. **Adapter/Runner separation** — `Adapter.Prepare()` is a pure function (testable), `Runner.Run()` handles execution (swappable: tmux, Docker, SSH)
8. **Declarative success criteria** — configurable QA checks in YAML, evaluated as pure functions
9. **Attempt tracking** — reconstruct per-attempt history from event log for post-mortem and retry context injection

## VXD vs NXD
| Aspect | VXD | NXD |
|--------|-----|-----|
| Repo | `tzone85/vortex-dispatch` (private) | `tzone85/nexus-dispatch` (public) |
| LLM | Cloud APIs (Anthropic + Google AI) | Ollama (offline-first) |
| Module | `github.com/tzone85/vortex-dispatch` | `github.com/tzone85/nexus-dispatch` |
| Binary | `~/.local/bin/vxd` | `~/.local/bin/nxd` |
| Rule | NEVER reference VXD in NXD code | Keep in sync on core fixes |

## Pending Work (as of 2026-07-05)
Tracked authoritatively in CLAUDE.md "Pending Work" — highlights:
1. Fix GitHub Actions billing — account payment issue, CI slimmed to ubuntu-only — OPEN
2. Coverage roadmap: raise `internal/cli` (72.9%) over 80% — OPEN (needs fakes for docker/gh/claude CLI)
3. Self-improve source-quality gap — research scrapers fetch news, not code-actionable signals — FEATURE REQUEST
(Everything from the 2026-04-12 list — repolearn, adapter/runner, Docker/SSH runners + NXD ports, SDD specs — is DONE.)
