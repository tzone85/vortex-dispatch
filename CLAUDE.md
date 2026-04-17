# VXD (Vortex Dispatch) — AI Agent Orchestration System

## What This Is
VXD orchestrates AI coding agents (Claude Code, Codex, Gemini CLI) to autonomously implement software requirements. It decomposes requirements into stories, dispatches agents in parallel via tmux sessions, monitors progress, runs QA, and merges PRs — with a 5-tier escalation chain for failures.

## Build Commands
```bash
# CRITICAL: Always build to ~/.local/bin/ (NOT ~/go/bin/)
go build -o ~/.local/bin/vxd ./cmd/vxd

# Run tests (exclude improve/ which has flaky prompt injection test)
go test $(go list ./... | grep -v improve) -count=1

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
| `internal/improve/` | Self-improvement engine (research, analysis, implementation) |
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
  tech_lead: {provider: anthropic, model: claude-opus-4-20250514}
  senior: {provider: anthropic, model: claude-sonnet-4-20250514}
  junior: {provider: google, model: gemma-4-27b-it}
routing:
  junior_max_complexity: 3
  max_retries_before_escalation: 2
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
| `vxd preflight` | Run 12 pre-flight checks before dispatch |
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

## Documentation Requirements (MANDATORY)

**Every behavioral change MUST update documentation.** This is enforced by `TestDocCoverage_*` wiring tests in `engine/doc_coverage_test.go`.

### What Counts as Behavioral
- New CLI command → add to CLAUDE.md CLI Commands table + README.md
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
1. `TestDocCoverage_CLICommands` — verifies every `newXxxCmd()` in `internal/cli/root.go` appears in CLAUDE.md
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
6. **CLAUDE.md in worktrees** — overrides Claude Code plugins that would hijack agent behavior
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

## Critical Operational Knowledge

### ANTHROPIC_API_KEY Conflict
- When `ANTHROPIC_API_KEY` is set AND Claude CLI is installed, CLI uses API credits instead of Max subscription
- VXD actively removes this key from tmux global env (`tmux set-environment -g -u ANTHROPIC_API_KEY`)
- The CLI adapter also unsets it in the command string (`unset CLAUDECODE ANTHROPIC_API_KEY;`)
- Preflight check `CheckAnthropicKeyConflict` warns about this condition
- **User fix:** `unset ANTHROPIC_API_KEY` before running VXD

### Claude CLI Max-Turns
- `ClaudeCLIClient` uses `--max-turns 25` for planning/review calls
- Sonnet 4.6+ uses tool calls to read files before responding — needs 10-20 turns for complex projects
- If planning fails with "error_max_turns", increase this in `internal/llm/claude_cli.go`

### Plugin Interference
- Claude CLI loads superpowers/brainstorming plugins that hijack structured JSON responses
- The `buildCLIPrompt` appends "CRITICAL: You are being called programmatically by VXD..." to override plugins
- `extractJSON` in `internal/engine/jsonutil.go` handles preamble text before JSON by finding first `[` or `{`
- CLAUDE.md in worktrees controls agent behavior at project level

### Code Review Graph (codegraph)
- `internal/codegraph/` integrates code-review-graph (Python, installed via `uv tool install`)
- Provides blast-radius analysis before code review
- Gracefully degrades — if binary not installed, all functions return empty results
- Graph stored in `.code-review-graph/graph.db` (SQLite)

## Pending Work (as of 2026-04-14)
1. ~~All items from 2026-04-12~~ — DONE
2. ~~Code-review-graph integration~~ — DONE (VXD + NXD)
3. ~~Test coverage boost~~ — VXD 84.4%, NXD 70.1%
4. ~~Pipeline bug fixes~~ — 9 bugs found and fixed via real pipeline testing
5. ~~Web dashboard JSON fix~~ — model structs now have JSON tags
6. Port Docker/SSH runners to NXD (remaining sync)
7. Fix GitHub Actions billing to unblock CI
8. Re-planner guardrails — prevent hallucinated sub-stories during tier-3 splits
9. Post-merge rebase check — auto-detect and resolve conflicts on open PRs
10. NXD native runtime unstaged changes — auto-commit before rebase
