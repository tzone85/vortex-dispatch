# VXD (Vortex Dispatch)

**Hand off a requirement, walk away, come back to merged PRs.**

[![Go](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![CI](https://github.com/tzone85/vortex-dispatch/actions/workflows/ci.yml/badge.svg)](https://github.com/tzone85/vortex-dispatch/actions/workflows/ci.yml)

## Overview

VXD is a Go CLI that drives the AI coding tools you already use — Claude Code, Codex, Gemini CLI, or any CLI describable in YAML — through the full lifecycle of a software change. You submit a requirement in natural language; an LLM tech-lead breaks it into a dependency DAG of stories; the dispatcher assigns each story to an agent in its own git worktree; the pipeline runs LLM code review and declarative QA against the agent's output; passing stories get a squash-merge. Stories that touch a database can be given their own ephemeral Postgres — local Docker by default, ghost.build cloud as an option — so destructive SQL has a blast radius of exactly one story. When things go wrong, a five-tier escalation chain takes over before giving up: same-role retry with categorized error analysis, then a senior agent, then a manager diagnosing the failure pattern, then a tech-lead re-planning the story, finally a hard pause that asks for human intervention. State is event-sourced into an append-only log with SQLite projections, so the pipeline can crash, resume, replay, and emit a client delivery report from the same event history.

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

### Platform Support

| Platform | Build | Read-only commands | Full agent pipeline |
|----------|-------|--------------------|---------------------|
| macOS (Apple Silicon, Intel) | native | yes | yes |
| Linux (x86_64, arm64) | native | yes | yes |
| Windows 10/11 (native, no WSL) | `GOOS=windows go build` | yes | **no — requires tmux** |
| Windows + WSL2 (Ubuntu/Debian) | inside WSL | yes | yes |

VXD's agent execution pipeline depends on tmux for session isolation, recovery,
and live inspection, and tmux has no native Windows port. On native Windows the
binary still builds and runs — `vxd estimate`, `vxd status`, `vxd metrics`,
`vxd report`, `vxd projects`, `vxd config`, `vxd events`, and the dashboards all
work — but `vxd req` / `vxd resume` will fail preflight with a clear pointer to
WSL2. The recommended Windows install path is WSL2 with Ubuntu, where the same
Linux instructions below apply unchanged.

#### Windows install (read-only CLI on native Windows)

```powershell
go install github.com/tzone85/vortex-dispatch/cmd/vxd@latest
# Resulting binary lives at %USERPROFILE%\go\bin\vxd.exe — ensure that
# directory is on PATH (it is by default if you installed Go via the MSI).
vxd init
vxd preflight    # Will fail the tmux check; everything else should pass.
```

State on Windows lives under `%USERPROFILE%\.vxd\` by default.

**Docker on Windows requires an explicit `DOCKER_HOST`.** The devdb provider's
built-in Windows default points at the named pipe
`npipe:////./pipe/docker_engine`, but Go's stdlib HTTP transport cannot dial
named pipes without an extra dependency, so this default is intentionally a
fail-closed sentinel rather than a working endpoint. Set `DOCKER_HOST` to one
of:

- `tcp://localhost:2376` with `DOCKER_TLS_VERIFY=1` and `DOCKER_CERT_PATH=...`
  (the secure path — Docker Desktop's TLS-enabled TCP socket).
- `tcp://localhost:2375` **only if you have already accepted the plaintext
  risk** (no auth, no TLS — any local process can take over the daemon).
- Anything reachable from your environment (remote Docker host, WSL2 bridge,
  etc.).

To pick a different host shell for user metric/migration commands, set
`VXD_SHELL=pwsh` (or any shell on PATH); the default is `cmd.exe`.

#### Windows install (full pipeline via WSL2)

```powershell
# In an elevated PowerShell, one time:
wsl --install -d Ubuntu
```

```bash
# Inside the Ubuntu WSL shell:
sudo apt update && sudo apt install -y tmux git build-essential
# Install Go 1.23+ (apt's package may lag — see https://go.dev/dl).
go install github.com/tzone85/vortex-dispatch/cmd/vxd@latest
vxd init && vxd preflight
```

### Demo

![VXD Demo](https://vhs.charm.sh/vhs-23AYbABlUZ9ssvssNj9pWX.gif)

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
- **LLM-powered conflict resolution** -- rebase conflicts auto-resolved; binary files handled without LLM (deterministic policy); complex/multi-file conflicts escalate to Tech Lead with full requirement DAG context
- **Client delivery reports** -- markdown and HTML reports with effort summary, timeline, and agent performance
- **Pipeline metrics** -- success rates, timing, escalations, and trace-based agent activity stats
- **Repo learning** -- 3-pass analysis (static scan, git history, LLM deep analysis) builds persistent profiles for agents
- **Agent context sharing** -- WAVE_CONTEXT.md captures prior wave changes, injected into subsequent waves
- **TUI dashboard** -- single-pane Bubbletea interface (all 5 sections visible at once: agents, pipeline, stories, activity, escalations)
- **Web dashboard** -- browser-based dashboard via `vxd dashboard --web` with real-time WebSocket updates and full control panel
- **Multi-project isolation** -- per-project state under `~/.vxd/projects/<name>/`
- **Tiered cleanup** -- worktree pruning, branch garbage collection with configurable retention
- **Self-improvement engine** *(experimental — no actionable improvements produced to date; see CLAUDE.md for the honest assessment)* -- daily autonomous pipeline: research, analysis, implementation, PR, email report; weekly competitor repo clone+diff for pattern extraction
- **Memory dashboard** -- browser-based timeline, findings explorer, and opportunities view with direct source/PR links
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
| `vxd preflight` | Run pre-flight environment checks (15 checks, 3 severity tiers) |
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
| `vxd memory` | Launch the memory dashboard (timeline, findings explorer, opportunities) |
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

### Godmode (Skip Per-Tool Permission Prompts)

`--godmode` skips per-tool permission prompts during agent execution. It does **NOT** bypass the `review_mode` plan gate or the `auto_merge` PR gate — for fully unattended operation, set `merge.review_mode: auto` (default) **and** `merge.auto_merge: true` in `vxd.yaml`. With those two plus `--godmode`, `vxd req` runs end-to-end from one command:

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

| Section | Purpose | Key Defaults |
|---------|---------|--------------|
| `workspace` | State directory, storage backend (`sqlite`/`dolt`), log level (`debug`/`info`/`warn`/`error`), and log retention in days | `state_dir: ~/.vxd`, `backend: sqlite`, `log_level: info`, `log_retention_days: 30` |
| `models` | LLM provider and model binding per agent role — tech_lead, senior, intermediate, junior, qa, supervisor, manager | `tech_lead: claude-opus-4-20250514` (anthropic), `senior/qa/manager: claude-sonnet-4-20250514`, `junior/intermediate/supervisor: gemma-4-27b-it` (google) |
| `routing` | Story complexity thresholds per tier, max retries before escalation, and max concurrent agents | `junior_max_complexity: 3`, `intermediate_max_complexity: 5`, `max_retries_before_escalation: 2`, `max_concurrent_agents: 5` |
| `planning` | Max story complexity (Fibonacci cap), sequential-file patterns, design approach, and godmode flag | `max_story_complexity: 5`, `design_approach: ddd-tdd`, `godmode: false` |
| `monitor` | Supervisor polling interval, stuck-agent threshold, and context-freshness token budget | `poll_interval_ms: 10000`, `stuck_threshold_s: 600`, `context_freshness_tokens: 150000` |
| `cleanup` | Worktree pruning strategy (`immediate`/`deferred`), branch retention window, and log archive mode | `worktree_prune: immediate`, `branch_retention_days: 7`, `log_archive: dolt` |
| `merge` | Auto-merge toggle, base branch, PR body template, and human review mode (`auto`/`plan_only`/`manual`) | `auto_merge: true`, `base_branch: main`, `review_mode: auto` |
| `runtimes` | Map of named CLI runtime definitions — command, args, supported models, and idle/permission detection patterns | Includes built-in entries for `claude-code`, `codex`, `gemini`, `swe-agent`; each supports optional `runner: docker\|ssh` |
| `billing` | Hourly consulting rate, currency, Fibonacci-to-hours range mapping, and LLM cost accounting mode | `default_rate: 150.0`, `currency: USD`, `llm_costs.mode: subscription` |
| `qa` | Declarative success criteria evaluated after each story (output_contains, file_exists, file_contains, exit_code_zero, etc.) | No criteria by default; standard lint/build/test always run |
| `sla` | Per-Fibonacci-point maximum story duration in minutes; `auto_escalate` promotes breached stories to the next tier | `1pt→60m`, `2pt→120m`, `3pt→240m`, `5pt→480m`, `8pt→960m`, `13pt→1920m`; `auto_escalate: false` |
| `secrets` | Secrets provider: `env` (default, reads from environment) or `vault` (HashiCorp Vault KV v2) | `provider: env`; Vault settings: `vault_mount: secret`, `vault_path: vxd` |
| `notify` | Outbound Slack webhook URL and per-event triggers (`notify_on_sla`, `notify_on_complete`) | Disabled by default (empty `slack_webhook_url`) |
| `autoresearch` | Per-repo Karpathy-style experiment loop: metric command, editable_paths allowlist, gate (`auto`/`winning`/`pr`), experiment budget, and Bayesian sampler | Disabled by default (`enabled: false`); requires `metric.command` and `editable_paths` when enabled |
| `devdb` | Per-story ephemeral Postgres: backend (`ghost`/`docker`/`null`), template DB to fork from, on-failure retention policy, and provider-specific settings | Disabled by default (`provider: null`); requires `template` when enabled. See "Ephemeral Databases" section below. |

## Ephemeral Databases

> **Status:** All sub-projects shipped on `main` (2026-05-22): SP1+SP3+SP4+SP5+SP6 (Docker provider, executor wiring, QA migration gate, `vxd db` CLI, dashboard DB column, metrics DB section) + SP2 (Ghost cloud provider). NXD mirror (Docker-only) also complete.

Every story can get its own throwaway Postgres database, forked from a template, deleted when the story finishes. Inspired by [ghost.build](https://ghost.build) — "Postgres built for agents."

**Two backends, one interface:**

- **Ghost** — cloud Postgres at ghost.build (VXD only). Sub-second forks, MCP-native, 100h/month free.
- **Docker** — local Postgres + template DBs (VXD + NXD). Fully offline.

**Shines for:**

- Per-story migration testing — fork prod snapshot, run migration, discard.
- Schema-aware code generation — agent calls `devdb_schema` for exact table structure.
- Destructive SQL testing — `DROP TABLE` freely; blast radius is one story.
- Multi-agent experimentation — competing approaches each get their own fork.

**Skip when:** pure-frontend stories, prod-touching ops, stories that finish in seconds.

**Minimum config:**

```yaml
devdb:
  provider: ghost           # or docker, or null
  template: my-prod-snapshot
  ghost: { api_key_env: GHOST_API_KEY }
```

Agents read `DATABASE_URL` from `.vxd-db/connect.env` (auto-injected into the worktree). Humans use `vxd db list/connect/logs/delete` plus the dashboard's per-story DB column.

For runtime usage run `vxd db --help`.

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

See [docs/diagrams/](docs/diagrams/) for rendered architecture and sequence diagrams.

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
  preflight/          Pre-flight validation (15 checks, 3 severity tiers)
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

## Troubleshooting

### Agents terminate immediately with no code changes

**Cause:** `ANTHROPIC_API_KEY` is set in your shell. Claude CLI uses API credits instead of your Max subscription, and the credits are exhausted.

**Fix:**
```bash
unset ANTHROPIC_API_KEY
vxd resume <req-id> --auto
```

VXD's preflight check will warn you about this conflict:
```
⚠ ANTHROPIC_API_KEY is set alongside Claude CLI — agents will use API credits
  instead of Max subscription. Run 'unset ANTHROPIC_API_KEY' if you have a
  Max subscription
```

**Permanent fix:** Remove `ANTHROPIC_API_KEY` from your shell profile (`~/.zshrc` or `~/.bashrc`) if you use a Max subscription.

### Web dashboard shows blank stories/escalations

**Cause:** Stale VXD binary without JSON tags on model structs (fixed in April 2026).

**Fix:** Rebuild VXD:
```bash
go build -o ~/.local/bin/vxd ./cmd/vxd
```

### Pipeline pauses with "agent produced no code changes"

**Causes:**
1. **API key conflict** (see above)
2. **Read-only stories** — the planner created a "Codebase Assessment" story that produces no code. VXD requires every story to produce a diff.

**Fix:** The planner prompt now instructs the tech lead not to create read-only assessment stories. Rebuild VXD to get the latest prompt.

### PR conflicts after merging

**Cause:** When multiple stories modify the same file, the second PR's branch diverges after the first merges.

**Fix:** Use `auto_merge: true` in `vxd.yaml`:
```yaml
merge:
  auto_merge: true
  base_branch: main
```

In auto-merge mode, VXD rebases each story onto main before merging, resolving conflicts via the LLM-powered ConflictResolver. The resolver applies a 3-tier strategy: (1) binary files (Mach-O, ELF, `*.exe`, `server`, `main`) are handled deterministically without an LLM call — oversized or compiled artifacts are removed via `git rm`, others are resolved with `git checkout --ours`; (2) text conflicts are attempted by the Senior model; (3) if the Senior fails, the resolved content still contains conflict markers, or the conflict spans more than 3 files, the Tech Lead is invoked with the full requirement text, story acceptance criteria, and dependency DAG context. Without auto-merge, PRs are created but must be rebased and merged manually or via `vxd approve`.

### Merging open PRs in order

```bash
# Auto-merge all pending PRs for a requirement (rebases in sequence)
vxd approve --all <req-id>

# Or approve/merge individual stories
vxd approve <story-id>
```

### Review modes

Control how much human oversight VXD requires:

```yaml
# vxd.yaml
merge:
  auto_merge: true     # Merge PRs automatically after QA passes
  review_mode: auto    # auto | plan_only | manual
```

| Mode | Plan Approval | PR Approval | Best For |
|------|--------------|-------------|----------|
| `auto` | Not required | Not required | Trusted pipelines, CI/CD |
| `plan_only` | Required (`vxd approve-plan`) | Not required | Review decomposition, auto-merge |
| `manual` | Required | Required (`vxd approve`) | Full human oversight |

Override per-run:
```bash
vxd resume <req-id> --auto     # Force auto mode for this run
vxd resume <req-id> --review   # Force manual review for this run
```

### Monitoring a running pipeline

```bash
# TUI dashboard (real-time, keyboard navigation)
vxd dashboard

# Web dashboard (browser-based, WebSocket updates)
vxd dashboard --web
vxd dashboard --web --port 8788  # Custom port if 8787 is in use

# Check status from CLI
vxd status --req <req-id>

# Peek at agent tmux sessions
tmux list-sessions
tmux attach -t <session-name>   # Watch agent work live
```

### Column headers in the dashboard

| Column | Meaning |
|--------|---------|
| **C** | Complexity — Fibonacci story points (1, 2, 3, 5, 8, 13) |
| **T** | Tier — escalation tier (0 = no escalation, 1 = senior retry, 2 = manager, 3 = tech lead re-plan, 4 = paused) |

## Testing

```bash
go test ./...                    # Unit + integration
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

### 2026-06-02 — Production-Readiness Pass

- **Verified green**: 28 packages pass `go test ./... -count=1`, `go vet` clean, `vxd preflight` all-green on configured hosts.
- **Conflict resolution**: 3-tier rebase strategy — binary detection (numstat + null-byte sniff), Senior fast-path, Tech-Lead escalation with DAG/sibling/log context. Triggers on Senior failure, residual `<<<<<<<` markers, or >3-file integration conflicts (`internal/engine/conflict_resolver.go`).
- **Post-merge integration build**: Tech-Lead-led auto-fix loop after merge to catch cross-story regressions before the next wave dispatches.
- **`vxd req --background`**: self-daemonizes the planning + dispatch loop; `vxd logs <req-id>` tails the captured daemon log; planning emits a heartbeat so the dashboard doesn't show a dead pipeline.
- **Reviewer structural check**: spec file list validated against actual diff — agents that ship a partial spec are rejected, not silently merged.
- **Config robustness**: `sla.max_minutes_per_complexity` accepts both bare-int and quoted-string keys; LLM provider error message lists accepted values (`anthropic_cli` alias added).
- **Cleanup hygiene**: `pre-ff-pull` removes `WAVE_CONTEXT.md` / `REQUIREMENT.md` before `git pull --ff-only` so fast-forward doesn't choke on VXD artifacts.

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
- New 1,650-line architecture overview at `docs/architecture-overview.md`
- README sections for SLA, secrets, /health, backup workflow

## License

[Apache License 2.0](LICENSE)

---

Built with the philosophy: **orchestrate agents like a real agile team.**

---

_Made with ❤️ by [Vortex Dispatch](https://github.com/tzone85/vortex-dispatch). Remove this line freely._
