# VXD (Vortex Dispatch) — AI Agent Orchestration System

## What This Is
VXD orchestrates AI coding agents (Claude Code, Codex, Gemini CLI) to autonomously implement software requirements. It decomposes requirements into stories, dispatches agents in parallel via tmux sessions, monitors progress, runs QA, and merges PRs — with a 5-tier escalation chain for failures.

## Build Commands
```bash
# CRITICAL: Always build to ~/.local/bin/ (NOT ~/go/bin/)
go build -o ~/.local/bin/vxd ./cmd/vxd

# Run tests
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
| `vxd preflight` | Run 12 pre-flight checks before dispatch |
| `vxd approve <story-id>` | Approve a story PR for merge (`--all <req-id>` for batch) |
| `vxd approve-plan` | Approve story plan before dispatch |
| `vxd reject-plan` | Reject a plan with feedback |
| `vxd review <story-id>` | View PR diff before approving (`--open` to open in browser) |
| `vxd reject <story-id>` | Reject a story's PR with feedback |
| `vxd projects` | List all projects |
| `vxd agents` | List active agents |
| `vxd events` | View event log |
| `vxd escalations` | List escalation events |
| `vxd config show` | Pretty-print current configuration as YAML |
| `vxd config validate` | Validate the current configuration file |
| `vxd archive` | Archive completed requirements |
| `vxd memory` | Launch memory dashboard |
| `vxd opportunity` | Manage opportunity pipeline |
| `vxd opportunity list` | Show opportunity pipeline sorted by rank (`--status`, `--limit`) |
| `vxd opportunity propose <id>` | Draft a proposal for a specific opportunity |
| `vxd opportunity status <id> <new-status>` | Update opportunity status |
| `vxd opportunity won <id> <amount>` | Log revenue for a won opportunity |
| `vxd opportunity sources` | Show discovered sources pending approval |
| `vxd opportunity approve-source <url>` | Approve a discovered source for active scraping |
| `vxd learn [path]` | Run repo analysis (`--force`, `--pass 1\|2\|3`, `--json`) |
| `vxd backup` | Create tar.gz archive of project state (`--output DIR`) |
| `vxd gc` | Garbage-collect branches + expired logs |
| `vxd improve log` | Browse improvement changelog (`--disposition`, `--category`, `--since`, `--errors`) |
| `vxd improve runs` | Show daily run summaries (findings, PRs, email status) |
| `vxd improve detail <id>` | Full details of a specific finding (reasoning, errors, PR) |
| `vxd autoresearch start <repo>` | Start autoresearch coordinator for a repo (`--budget`, `--continuous`) |
| `vxd autoresearch stop <repo>` | Drain and stop coordinator |
| `vxd autoresearch status [<repo>]` | Show wins, losses, Bayes posterior, budget |
| `vxd autoresearch hypotheses <repo>` | List top wins and recent losses with diff hashes |
| `vxd autoresearch evolve <repo>` | Manually trigger `program.md` evolution PR (always human-gated) |

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

## Working Principles

### 1. Look for existing tools first
Before building anything new, check `tools/`, `internal/`, and existing packages based on what the workflow requires. Only create new scripts or modules when nothing exists for that task.

### 2. Learn and adapt when things fail
When you hit an error:
- Read the full error message and trace
- Fix the script and retest (if it uses paid API calls or credits, check with the user before running again)
- Document what you learned in the workflow (rate limits, timing quirks, unexpected behavior)
- Example: You get rate-limited on an API, so you dig into the docs, discover a batch endpoint, refactor the tool to use it, verify it works, then update the workflow so this never happens again

### 3. Keep workflows current
Workflows should evolve as you learn. When you find better methods, discover constraints, or encounter recurring issues, update the workflow. Do NOT create or overwrite workflows without asking unless explicitly told to. These are instructions that need to be preserved and refined, not tossed after one use.

### The Self-Improvement Loop
Every failure is a chance to make the system stronger:
1. Identify what broke
2. Fix the tool
3. Verify the fix works
4. Update the workflow with the new approach
5. Move on with a more robust system

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
- `ClaudeCLIClient` uses `--max-turns 50` for planning/review calls (`internal/llm/claude_cli.go`)
- `Implementer` uses `--max-turns 25` for self-improvement implementations (`internal/improve/implementer.go`)
- Sonnet 4.6+ uses tool calls to read files before responding — complex projects need 15-30 turns
- If planning fails with "error_max_turns", increase in the relevant file above

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

### Self-Improvement Pipeline (vxd-improve)
- `implementer.go` uses `--max-turns 25` for Claude CLI (was 1 — root cause of ALL aborted findings)
- `filterEnv()` strips `ANTHROPIC_API_KEY` and `CLAUDECODE` from agent env
- Triage includes `actionable` flag — non-actionable findings (competitor news, blog posts) skip implementation and log as "proposed"
- `AuditEntry` has `Error` field for diagnostic persistence; errors also tracked in `RunSummary.Errors`
- `SaveRunSummary` is called TWICE: once at Phase 5 (pre-email) and once at end (with `email_sent` flag)
- Stale `*_boost_test.go` / `*_coverage_test.go` files are auto-generated and may reference deleted functions — delete them if they break the build

### gitDiff Branch Support
- `gitDiff()` in `monitor.go` tries merge-base candidates: `origin/main`, `origin/master`, `main`, `master`
- Repos using `master` (e.g., mukuru-api) previously fell back to root commit, producing massive diffs that obscured real changes
- **If review keeps rejecting valid work**: check which branch the target repo uses and verify merge-base resolution

### SLA Timer on Resume
- `checkSLA()` uses the LATEST `STORY_STARTED` event, not the first
- Without this, resumed stories get immediately terminated (elapsed time includes paused period)
- SLA start times are cached in `slaStartTimes` map — cleared when story finishes

### Multi-Project State Directories
- Default state: `~/.vxd/projects/<name>/`
- Projects can override via `vxd.yaml` `workspace.state_dir` (e.g., `~/.vxd-mukuru-api/`)
- To find which state dir a running dashboard uses: `lsof -p <PID> | grep events.jsonl`
- The `vxd projects` command shows what VXD knows, but custom state dirs may not appear there

### Post-Merge Artifact Protection
- `stripVXDArtifactsFromBranch()` runs after autoCommit, before review/merge
- For files that exist on the base branch (e.g. project CLAUDE.md): restores the base version via `git checkout origin/main -- CLAUDE.md` so the merge is a no-op
- For VXD-only files (WAVE_CONTEXT.md, .vxd-prompts/, .serena/): fully removes via `git rm -rf`
- `pullMainAfterMerge()` runs after all stories complete: pulls latest main/master, cleans up WAVE_CONTEXT.md/REQUIREMENT.md from repo root, applies gitignore patterns
- 16 tests in `artifact_protection_test.go` verify: auto-pull, master branch support, artifact cleanup, gitignore, CLAUDE.md preservation after merge, full pipeline integration
- **Root cause (2026-04-30):** agents commit VXD directive CLAUDE.md into worktree branches → PR merges it → overwrites project's real CLAUDE.md. Tests caught that naive `git rm` would DELETE CLAUDE.md from main on merge — restore-to-base is the correct approach.

### Model ID Compatibility
- Claude CLI subscription uses `claude-sonnet-4-20250514` / `claude-opus-4-20250514`
- Dated 4.6 IDs (`claude-sonnet-4-6-20250620`) do NOT work on CLI subscription tier
- Always test model IDs with `claude --model <id> -p "test" --max-turns 1` before updating defaults

### Debugging Checklist (Pipeline Issues)
1. **Stories stuck in draft after escalation** → Check if SLA breach is killing them on resume. Reset `escalation_tier` in SQLite if needed.
2. **Review keeps rejecting valid work** → Check `gitDiff()` merge-base. Does the repo use `master` or `main`? Check diff output manually: `cd <worktree> && git diff origin/<branch>...HEAD --stat`
3. **Self-improvement findings all aborted** → Check `--max-turns`, env vars, and whether findings are actionable
4. **Email never sends (email_sent: false)** → Verify `RESEND_API_KEY` is set. Check if summary is re-saved after email phase.
5. **Agent produces work but diff shows empty** → Agent may not have committed. `autoCommit()` runs in post-execution, but check worktree: `cd <worktree> && git status`
6. **CLAUDE.md overwritten after merge** → `stripVXDArtifactsFromBranch` should prevent this. If it still happens, check that the function runs before `rebaseAndMerge`. Verify with: `git log --oneline --diff-filter=M -- CLAUDE.md`
7. **Code exists on GitHub but not locally** → `pullMainAfterMerge` should auto-pull. If it failed, check for dirty working tree or network issues. Manual fix: `git pull --ff-only origin main`

## Pending Work (as of 2026-04-30)
1. ~~Port Docker/SSH runners to NXD~~ — DONE
2. Fix GitHub Actions billing — account payment issue, CI slimmed to ubuntu-only
3. ~~Mukuru-api pipeline~~ — DONE (7/7 merged, PRs #6-#12)
4. ~~Mukuru-site pipeline~~ — DONE (7/7 merged, PRs #18-#24)
5. ~~CashTask backend~~ — DONE (10/10 merged, PRs #1-#10)
6. ~~Artifact protection~~ — DONE (stripVXDArtifactsFromBranch + pullMainAfterMerge + 16 tests)
7. ~~Codex review fixes~~ — DONE (scoping, agent projection, handler errors, ergonomics)
8. Post-merge rebase check — auto-detect and resolve conflicts on open PRs
9. Re-planner guardrails — prevent hallucinated sub-stories during tier-3 splits
