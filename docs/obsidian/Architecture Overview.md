---
tags: [architecture]
---

# Architecture Overview

VXD is a single Go module (`github.com/tzone85/vortex-dispatch`) organized into
focused internal packages.

## Package map
| Package | Responsibility |
|---------|----------------|
| `internal/engine/` | Core pipeline: monitor, executor, dispatcher, escalation, QA, merger, planner, reviewer, conflict_resolver |
| `internal/state/` | [[Event Sourcing]]: JSONL event store, SQLite projection, domain models |
| `internal/runtime/` | [[Runtime and Adapters]]: adapter (pure command-build) + runner (tmux/Docker/SSH) |
| `internal/agent/` | Prompt templates, role definitions, diagnostic playbooks |
| `internal/llm/` | LLM clients (Anthropic, Google AI, OpenAI, Claude CLI, fallback) |
| `internal/config/` | YAML config loading, validation, defaults — see [[Configuration]] |
| `internal/git/` | Worktree management, PR creation, branch operations |
| `internal/tmux/` | Tmux session lifecycle |
| `internal/devdb/` | Ephemeral per-story Postgres (Docker + Ghost providers) |
| `internal/web/` | Web dashboard (WebSocket + embedded static) — see [[Dashboard Authentication]] |
| `internal/dashboard/` | TUI dashboard (Bubbletea) |
| `internal/improve/` | Self-improvement engine (research, analysis, implementation) |
| `internal/autoresearch/` | Per-repo experiment loop with Bayesian sampling |
| `internal/repolearn/` | 3-pass repo analysis (static, git history, LLM) |
| `internal/preflight/` | Pre-flight validation checks |
| `internal/sanitize/` | Prompt-injection + secret pattern detection |
| `internal/shellexec/` | Host shell resolution for user-supplied commands |

## Cross-cutting design decisions
1. **Event sourcing over CRUD** — full audit trail, replay (see [[Event Sourcing]]).
2. **SQLite WAL mode** — concurrent readers without blocking writers.
3. **Tmux sessions** — agents survive monitor crashes, inspectable live.
4. **Worktrees** — file isolation between parallel agents, shared git objects.
5. **Adapter/Runner separation** — `Adapter.Prepare()` is pure; `Runner.Run()`
   handles I/O. See [[Runtime and Adapters]].
6. **Declarative QA criteria** — configurable checks in YAML, evaluated as pure
   functions.

## Platform notes
Platform-specific code lives in `_unix.go` / `_windows.go` build-tagged pairs.
Read-only commands work natively on Windows; the full agent pipeline needs tmux
(run under WSL2). Shell exec goes through `internal/shellexec`.
