# VXD (Vortex Dispatch)

**Hand off a requirement, walk away, come back to merged PRs.**

[![Go](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![CI](https://github.com/tzone85/vortex-dispatch/actions/workflows/ci.yml/badge.svg)](https://github.com/tzone85/vortex-dispatch/actions/workflows/ci.yml)

## Overview

VXD is a Go CLI that orchestrates autonomous AI agents through the full software development lifecycle. Submit a natural-language requirement and VXD decomposes it into stories, assigns them to agents based on complexity, executes work in parallel waves, runs code review and QA, creates pull requests, and merges them -- all without human intervention.

- **Event-sourced state management** with an append-only event log and SQLite materialized projections
- **Full agile team hierarchy** -- Tech Lead, Senior, Intermediate, Junior, QA, Supervisor
- **Pluggable AI runtimes** -- Claude Code, Codex, Gemini CLI (configured via YAML)
- **Wave-based parallel execution** with topological dependency resolution

## Cost Model

VXD uses your existing Claude Code subscription for ALL operations:
- **Agent development work** — via Claude Code CLI in tmux sessions
- **Code review, conflict resolution, and planning** — via Claude Code CLI

No separate API credits needed. If you have Claude Code installed and authenticated, VXD is free to use.

For users without Claude Code CLI, VXD falls back to direct API calls using `ANTHROPIC_API_KEY`.

## Quick Start

```bash
go install github.com/tzone85/vortex-dispatch/cmd/vxd@latest
vxd init
vxd preflight                     # Validate environment
vxd estimate "Build a REST API"   # Estimate cost before committing
vxd req "Build a REST API for user management with CRUD endpoints"
vxd resume <req-id>               # Dispatch agents
vxd status
vxd dashboard
```

### Demo

![VXD Demo](https://vhs.charm.sh/vhs-5yT705ybH66DOTmCJKviR8.gif)

See the [full tutorial](docs/tutorial.md) for a step-by-step walkthrough.

<details>
<summary>Re-record the demo locally</summary>

```bash
brew install vhs ffmpeg ttyd
vhs docs/demo.tape
```
</details>

## Features

- **Agent hierarchy with complexity-based routing** -- Fibonacci scoring routes stories to the right tier
- **Event-sourced architecture** -- append-only event log with materialized SQLite projections
- **Pluggable runtimes via YAML config** -- swap between Claude Code, Codex, and Gemini CLI
- **Adapter/Runner execution model** -- pure command prep (Adapter) separated from execution (TmuxRunner, DockerRunner, SSHRunner)
- **5-tier escalation chain** -- same-role retry with smart error analysis, senior, manager diagnosis, tech lead re-planning, pause
- **Smart retry with error analysis** -- 8 error categories with targeted fix suggestions passed to retry agents
- **Human review gates** -- three modes (auto, plan_only, manual) for plan approval and PR review
- **Crash recovery** -- lock files, checkpoints, and consistency checks for resuming after process death
- **Pre-flight validation** -- 12 environment checks across 3 severity tiers before pipeline execution
- **Cost estimation** -- quick heuristic and LLM-based estimation with Fibonacci-to-hours mapping
- **Watchdog monitoring** -- stuck detection, permission bypass, context freshness checks
- **Supervisor oversight** -- periodic drift detection and reprioritization
- **Senior code review** -- automated review via LLM with approve/request-changes verdicts
- **Automated QA pipeline** -- lint, build, and test with declarative success criteria (6 kinds)
- **Auto-merge with PR creation** -- stories flow from code to merged PR hands-free
- **LLM-powered conflict resolution** -- rebase conflicts auto-resolved via Senior model instead of blocking
- **Client delivery reports** -- markdown and HTML reports with effort summary, timeline, and agent performance
- **Pipeline metrics** -- success rates, timing, escalations, and trace-based agent activity stats
- **Repo learning** -- 3-pass analysis (static scan, git history, LLM deep analysis) builds persistent profiles for agents
- **Agent context sharing** -- WAVE_CONTEXT.md captures prior wave changes, injected into subsequent waves
- **TUI dashboard** -- single-pane Bubbletea interface (all 5 sections visible at once: agents, pipeline, stories, activity, escalations)
- **Web dashboard** -- browser-based dashboard via `vxd dashboard --web` with real-time WebSocket updates and full control panel
- **Multi-project isolation** -- per-project state under `~/.vxd/projects/<name>/`
- **Tiered cleanup** -- worktree pruning, branch garbage collection with configurable retention
- **Self-improvement engine** -- daily autonomous pipeline: research, analysis, implementation, PR, email report; weekly competitor repo clone+diff for pattern extraction
- **Reputation scoring** -- per-agent performance tracking across assignments

## CLI Commands

| Command | Description |
|---------|-------------|
| `vxd init` | Initialize workspace, create `~/.vxd/` dirs, generate default config, set up stores |
| `vxd req <requirement>` | Submit a requirement (supports `--file`/`-f`, `--godmode`, `--skip-preflight`) |
| `vxd status [--req ID]` | Show requirement and story status, optionally filtered by requirement |
| `vxd resume <req-id>` | Resume a paused pipeline (`--godmode`, `--review`, `--auto`, `--force`) |
| `vxd agents [--status S]` | List all agents with current story, session, and status |
| `vxd escalations` | List all escalation events with story, agent, reason, and status |
| `vxd gc [--dry-run]` | Garbage-collect merged branches and worktrees past retention |
| `vxd config show` | Pretty-print the current configuration as YAML |
| `vxd config validate` | Load and validate the configuration file |
| `vxd events [--type T] [--story S] [--limit N]` | List events from the event store, newest first |
| `vxd dashboard` | Launch the live TUI dashboard |
| `vxd dashboard --web [--port 8787]` | Launch the web dashboard (browser-based, default port 8787) |
| `vxd preflight` | Run pre-flight environment checks (12 checks, 3 severity tiers) |
| `vxd estimate <requirement>` | Estimate cost (`--quick`, `--json`, `--rate`, `--save`) |
| `vxd report <req-id>` | Generate client delivery report (`--html`, `--internal`, `--output`) |
| `vxd metrics [--req ID]` | Show pipeline performance metrics with agent activity stats |
| `vxd learn [repo-path]` | Analyse a repository and build a persistent profile (`--pass`, `--force`) |
| `vxd projects` | List all tracked projects |
| `vxd approve-plan <req-id>` | Approve a plan for dispatch (review gates) |
| `vxd reject-plan <req-id>` | Reject a plan (review gates) |
| `vxd review <story-id>` | Show story details for review |
| `vxd approve <story-id>` | Approve a story's PR for merge (`--all <req-id>` for batch) |
| `vxd reject <story-id>` | Reject a story's PR (returns to in_progress) |
| `vxd pause <req-id>` | Pause a running requirement |
| `vxd memory` | Launch the memory dashboard |
| `vxd backup [--output DIR]` | Create tar.gz archive of project state (events.jsonl, store.db, config) |

### Submitting Requirements

The `vxd req` command accepts requirements in three ways:

```bash
# Inline as a positional argument
vxd req "Build a REST API for user management with CRUD endpoints"

# From a file (--file or -f)
vxd req --file requirements.md
vxd req -f ~/specs/my-feature.md

# From stdin
cat spec.md | vxd req -f -
```

Using `--file` is recommended for complex requirements — write your full spec in a markdown file with acceptance criteria, constraints, and architecture notes, then hand it off to VXD.

### Godmode (Autonomous Operation)

By default, VXD's LLM calls (planning, review, conflict resolution) may prompt for permission. Pass `--godmode` to run fully autonomously without approval prompts:

```bash
# Submit a requirement in godmode
vxd req --file requirements.md --godmode

# Resume a pipeline in godmode
vxd resume 01KM035Y --godmode
```

Godmode can also be set permanently in `vxd.yaml`:
```yaml
planning:
  godmode: true
```

The `--godmode` flag takes precedence over the config value. When not passed, the config value is used (default: `false`).

### Repo Learning

Before dispatching agents, run `vxd learn` to build a persistent profile of the target repository. This eliminates the codebase archaeology phase where agents waste early iterations figuring out the tech stack, build commands, and test conventions.

```bash
# Analyse the current directory
vxd learn

# Analyse a specific repo
vxd learn /path/to/project

# Re-run all passes (even if previously completed)
vxd learn --force

# Run only a specific pass
vxd learn --pass 1   # Static scan only
vxd learn --pass 2   # Git history only

# Output the full profile as JSON
vxd learn --json
```

The analysis runs in three passes:

- **Pass 1 — Static scan**: Detects language, framework, build/lint/test commands (from Makefile, package.json, Cargo.toml, etc.), CI system, directory structure, entry points, dependencies, and signals (monorepo, no tests, Docker, vendored deps)
- **Pass 2 — Git history**: Analyses commit message format (conventional/ticket-prefix/freeform), contributor count, churn hotspots (most-changed files), and branch naming patterns
- **Pass 3 — Deep analysis**: LLM-assisted summary of project purpose, architecture, key patterns, and gotchas (runs automatically during `vxd req` when an LLM client is available)

The profile is saved to `~/.vxd/projects/<name>/repo-profile.json` and automatically loaded by the executor and planner to enrich agent prompts with pre-learned knowledge. Use `vxd projects` to see the learning status of all tracked projects.

### SLA Tracking

VXD tracks per-story duration against configurable SLA thresholds and emits `STORY_SLA_BREACHED` events when stories exceed their limit:

```yaml
sla:
  max_minutes_per_complexity:
    1: 60      # 1pt = 1 hour
    2: 120     # 2pt = 2 hours
    3: 240     # 3pt = 4 hours
    5: 480     # 5pt = 8 hours
    8: 960     # 8pt = 16 hours
    13: 1920   # 13pt = 32 hours
  auto_escalate: false   # opt-in: trigger tier escalation on breach
```

Breaches surface in `vxd metrics` (count + rate), `vxd report` (⚠ badge per story), and the event log. Set `auto_escalate: true` to automatically promote breached stories to the next tier.

### Secrets Management

LLM API keys are loaded via a swappable secrets provider. Default is environment variables; HashiCorp Vault is supported for production:

```yaml
# Default — read from env (no config needed)
secrets:
  provider: env

# Phase 2 — read from Vault
secrets:
  provider: vault
  vault_addr: http://127.0.0.1:8200
  vault_token: "..."        # or set VAULT_TOKEN env var
  vault_mount: secret        # optional, defaults to "secret"
  vault_path: vxd            # optional, defaults to "vxd"
```

Vault uses KV v2 (modern default). Store secrets as a single map at the configured path:
```bash
vault kv put secret/vxd \
  ANTHROPIC_API_KEY="sk-ant-..." \
  GOOGLE_API_KEY="AIza..." \
  GITHUB_TOKEN="ghp_..."
```

Switching providers requires no code changes — only the config file.

### Health Endpoint

When running `vxd dashboard --web`, a `/health` endpoint returns JSON `{status: "ok", version: "0.1.0"}` for systemd, Docker, or Kubernetes liveness probes.

### Backups

Create a tar.gz archive of the project state directory:
```bash
vxd backup                    # to current directory
vxd backup --output /backups  # to specific directory
```

Archives include `events.jsonl`, `store.db`, and other state files. Combined with the append-only event log design, this provides a baseline disaster recovery story (RPO = backup interval, RTO = restore + replay time).

## Configuration

Run `vxd init` to generate `vxd.yaml` with sensible defaults, then customize:

| Section | Purpose |
|---------|---------|
| `workspace` | State directory, backend (dolt/sqlite), log level and retention |
| `models` | LLM provider and model per agent role (tech_lead, senior, intermediate, junior, qa, supervisor, manager) |
| `routing` | Complexity thresholds, retry and escalation limits |
| `monitor` | Poll interval, stuck threshold, context freshness token limit |
| `cleanup` | Worktree pruning strategy, branch retention days |
| `merge` | Auto-merge toggle, base branch, PR template, review_mode |
| `runtimes` | CLI runtime definitions (command, args, model list, detection patterns) |
| `billing` | Hourly rate, Fibonacci-to-hours mapping for cost estimation |
| `qa` | Lint/build/test commands, declarative success criteria |
| `sla` | Per-complexity duration limits, optional auto-escalation on breach |
| `secrets` | Secrets provider (`env` or `vault`) and Vault connection settings |

## Architecture

```
Requirement
    |
    v
[Intake] --> vxd req decomposes via Tech Lead LLM
    |
    v
[Planning] --> Stories with Fibonacci complexity + dependency DAG
    |
    v
[Dispatch] --> Wave-based parallel assignment (topo sort on DAG)
    |
    v
[Execution] --> Agents work in tmux sessions via pluggable runtimes
    |
    v
[Review] --> Senior agent reviews diff via LLM
    |
    v
[QA] --> Lint + build + test pipeline
    |
    v
[Merge] --> Rebase with LLM conflict resolution + PR creation + auto-merge
    |
    v
[Cleanup] --> Worktree prune + branch GC
```

Events are appended at every stage. SQLite projections materialize the current state for queries.

## Agent Roles

| Role | Model Tier | Responsibility |
|------|------------|----------------|
| Tech Lead | Claude Opus | Requirement decomposition, story planning, dependency graphs |
| Senior | Claude Sonnet | Complex stories (5+ points), code review, conflict resolution |
| Intermediate | Gemma 4 / Claude Sonnet | Medium stories (3-5 points) |
| Junior | Gemma 4 / Claude Haiku | Simple stories (1-3 points) |
| QA | Claude Sonnet | Lint, build, test execution per story |
| Supervisor | Claude Sonnet | Drift detection, reprioritization |
| Manager | Claude Sonnet | Failure diagnosis, story rewriting at escalation tier 2 |

## Project Structure

```
cmd/vxd/              CLI entry point
internal/
  agent/              Role definitions, complexity scoring, prompts
  artifact/           Artifact store (launch configs, diffs, traces)
  cli/                Cobra command implementations (25+ commands)
  config/             YAML config loader and validation
  dashboard/          Bubbletea TUI (single-pane, all sections visible)
  engine/             Core orchestration (35+ files)
    planner.go        Tech Lead decomposition
    dispatcher.go     Wave-based parallel dispatch
    executor.go       Agent lifecycle management
    monitor.go        Polling loop with review gates and checkpoints
    escalation.go     5-tier escalation machine
    smart_retry.go    Error analysis with fix suggestions
    manager.go        Manager diagnosis and story rewriting
    reviewer.go       Senior code review
    review_gate.go    Human review mode resolution
    qa.go             Lint/build/test with declarative criteria
    merger.go         PR creation and auto-merge
    reaper.go         Tiered cleanup and GC
    checkpoint.go     Crash recovery checkpoints
    recovery.go       Consistency check and recovery
    lockfile.go       Advisory lock with PID-based stale detection
    cost.go           Cost estimation
    report.go         Client delivery reports
    trace.go          Agent output trace normalization
    metrics.go        Pipeline performance metrics
    wave_context.go   Cross-story context sharing
  git/                Branch, worktree, and GitHub PR operations
  graph/              Dependency DAG with topological sort
  improve/            Self-improvement engine (research, analysis, repo learning, revenue)
  llm/                LLM clients (Anthropic, OpenAI, Google AI, Claude CLI, Fallback)
  memory/             Memory dashboard + MemPalace integration
  preflight/          Pre-flight validation (12 checks, 3 severity tiers)
  repolearn/          3-pass repo learning (static, git history, LLM deep)
  runtime/            Adapter/Runner pattern (tmux, Docker, SSH)
  scratchboard/       Shared memory across parallel agents
  state/              Event store (file-based) + SQLite projections
  tmux/               Session management (create, capture, send-keys)
  web/                Web dashboard (WebSocket, static files, command handlers)
migrations/           SQLite schema migrations
test/                 E2E tests
```

## Documentation

Full training guides are available in the [`docs/`](docs/) directory:

- **[Getting Started](docs/getting-started.md)** -- Prerequisites, installation, first run
- **[Tutorial](docs/tutorial.md)** -- Hands-on walkthrough of the full pipeline
- **[Workflows](docs/workflows.md)** -- Each pipeline stage explained in depth
- **[Configuration](docs/configuration.md)** -- Every config knob with tuning advice
- **[Agents and Roles](docs/agents-and-roles.md)** -- Role hierarchy, routing, reputation
- **[Monitoring](docs/monitoring.md)** -- Watchdog, supervisor, dashboard, escalations
- **[Architecture](docs/architecture.md)** -- Event sourcing, internals, data flow
- **[Contributing](docs/contributing.md)** -- Adding runtimes, components, commands

## Testing

```bash
go test ./...                    # Unit + integration (128 tests)
go test -tags e2e ./test/        # E2E tests
go test ./... -race -coverprofile=coverage.out  # With race detection + coverage
```

## Development

```bash
make build    # Build the vxd binary
make test     # Run tests with race detection and coverage
make lint     # Run golangci-lint
make clean    # Remove binary and coverage artifacts
make install  # Build and install to $GOPATH/bin
```

### Required: PATH Setup

Before using VXD, ensure `~/go/bin` is on your PATH:

```bash
mkdir -p "$(go env GOPATH)/bin"
echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.zshrc
source ~/.zshrc
```

Then build and install:

```bash
make build && make install
vxd --help   # Should show the command list
```

### Using VXD in a New Project

VXD works in **any** git repository — you don't need to be in the source directory:

```bash
mkdir ~/my-project && cd ~/my-project && git init
vxd init
vxd req "Your requirement here"
```

See the [full Getting Started guide](docs/getting-started.md) for detailed setup instructions.

## Acknowledgements

VXD builds on ideas and patterns from several open-source projects. We're grateful for their pioneering work in AI agent orchestration:

| Project | Author | What We Learned |
|---------|--------|-----------------|
| [Gastown](https://github.com/steveyegge/gastown) | Steve Yegge | Git-backed persistence, runtime abstraction, convoy/formula system |
| [Beads](https://github.com/steveyegge/beads) | Steve Yegge | Hash-based task IDs, dependency-aware graph, memory decay patterns |
| [Dolt](https://github.com/dolthub/dolt) | DoltHub | Version-controlled SQL state, branch-per-agent isolation, row-level diffing |
| [Hungry Ghost Hive](https://github.com/nikrich/hungry-ghost-hive) | nikrich | Agile team hierarchy, complexity-based routing, micromanager daemon |
| [Wasteland](https://github.com/gastownhall/wasteland) | Gastown Hall | Reputation scoring, embedded web UI, tiered cleanup strategies |

If you're interested in AI agent orchestration, these projects are well worth studying.

## Recent Changes

### Unreleased — Hardening Session (2026-04-15/16)

**Security**
- Google AI API key moved from URL query string to `x-goog-api-key` header (HIGH)
- Planner rejects requirements with prompt injection patterns or embedded secrets (MEDIUM)
- File permissions tightened from 0644 to 0600 on event store, proposals, opportunities, feedback (LOW)
- Log retention enforced via `engine.CleanupLogs()` (wired into `vxd gc`)
- Shared `internal/sanitize/` package extracted for reuse

**Capacity & Performance**
- `routing.max_concurrent_agents` config (default 5, range 1-50)
- 5 SQLite indexes on foreign key columns
- Memory leak fixed in Monitor SLA tracking maps

**SLA Tracking**
- New `STORY_SLA_BREACHED` event type with full projection
- Per-Fibonacci-complexity duration limits (configurable)
- Optional auto-escalation on breach (`sla.auto_escalate`)
- Breach badges in `vxd report` (markdown + HTML), counts in `vxd metrics`

**Observability**
- `/health` endpoint on web dashboard for liveness probes

**Disaster Recovery**
- `vxd backup` command — tar.gz of project state directory

**Secrets Management**
- New `internal/secrets/` package with `Provider` interface
- `EnvProvider` (default) and `VaultProvider` (HashiCorp Vault KV v2)
- Config-driven provider switching via `secrets.provider: vault`
- Phase 2 ready — flip from env to Vault with zero code changes

**Bug Fixes**
- `extractJSON()` now handles conversational preambles and embedded code fences
- Google AI integration test updated for header-based auth

**Documentation**
- New 1,650-line architecture overview at `docs/superpowers/specs/2026-04-15-architecture-overview.md`
- README sections for SLA, secrets, /health, backup workflow

## License

[Apache License 2.0](LICENSE)

---

Built with the philosophy: **orchestrate agents like a real agile team.**
