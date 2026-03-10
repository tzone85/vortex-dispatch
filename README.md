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

## Quick Start

```bash
go install github.com/tzone85/vortex-dispatch/cmd/vxd@latest
vxd init
vxd req "Build a REST API for user management with CRUD endpoints"
vxd status
vxd dashboard
```

### Demo

Generate an animated demo with [VHS](https://github.com/charmbracelet/vhs):

```bash
brew install vhs
vhs docs/demo.tape
```

See the [full tutorial](docs/tutorial.md) for a step-by-step walkthrough.

## Features

- **Agent hierarchy with complexity-based routing** -- Fibonacci scoring routes stories to the right tier
- **Event-sourced architecture** -- append-only event log with materialized SQLite projections
- **Pluggable runtimes via YAML config** -- swap between Claude Code, Codex, and Gemini CLI
- **Watchdog monitoring** -- stuck detection, permission bypass, context freshness checks
- **Supervisor oversight** -- periodic drift detection and reprioritization
- **Senior code review** -- automated review via LLM with approve/request-changes verdicts
- **Automated QA pipeline** -- lint, build, and test execution per story
- **Auto-merge with PR creation** -- stories flow from code to merged PR hands-free
- **Tiered cleanup** -- worktree pruning, branch garbage collection with configurable retention
- **TUI dashboard** -- 4-panel Bubbletea interface (pipeline, agents, activity, escalations)
- **Reputation scoring** -- per-agent performance tracking across assignments

## CLI Commands

| Command | Description |
|---------|-------------|
| `vxd init` | Initialize workspace, create `~/.vxd/` dirs, copy default config, set up stores |
| `vxd req <requirement>` | Submit a requirement for Tech Lead decomposition into stories |
| `vxd status [--req ID]` | Show requirement and story status, optionally filtered by requirement |
| `vxd resume <req-id>` | Resume a paused pipeline, dispatch the next wave of ready stories |
| `vxd agents [--status S]` | List all agents with current story, session, and status |
| `vxd escalations` | List all escalation events with story, agent, reason, and status |
| `vxd gc [--dry-run]` | Garbage-collect merged branches and worktrees past retention |
| `vxd config show` | Pretty-print the current configuration as YAML |
| `vxd config validate` | Load and validate the configuration file |
| `vxd events [--type T] [--story S] [--limit N]` | List events from the event store, newest first |
| `vxd dashboard` | Launch the live TUI dashboard |

## Configuration

Copy `vxd.config.example.yaml` to `vxd.yaml` (or run `vxd init`) and customize:

| Section | Purpose |
|---------|---------|
| `workspace` | State directory, backend (dolt/sqlite), log level and retention |
| `models` | LLM provider and model per agent role (tech_lead, senior, intermediate, junior, qa, supervisor) |
| `routing` | Complexity thresholds, retry and escalation limits |
| `monitor` | Poll interval, stuck threshold, context freshness token limit |
| `cleanup` | Worktree pruning strategy, branch retention days |
| `merge` | Auto-merge toggle, base branch, PR template |
| `runtimes` | CLI runtime definitions (command, args, model list, detection patterns) |

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
[Merge] --> PR creation + auto-merge
    |
    v
[Cleanup] --> Worktree prune + branch GC
```

Events are appended at every stage. SQLite projections materialize the current state for queries.

## Agent Roles

| Role | Model Tier | Responsibility |
|------|------------|----------------|
| Tech Lead | Opus | Requirement decomposition, story planning, dependency graphs |
| Senior | Sonnet | Complex stories (5+ points), code review of junior/intermediate work |
| Intermediate | Haiku / Sonnet | Medium stories (3-5 points) |
| Junior | Haiku / GPT-4o-mini | Simple stories (1-3 points) |
| QA | Sonnet | Lint, build, test execution per story |
| Supervisor | Sonnet | Drift detection, reprioritization, escalation handling |

## Project Structure

```
cmd/vxd/              CLI entry point
internal/
  agent/              Role definitions, complexity scoring, prompts
  cli/                Cobra command implementations
  config/             YAML config loader and validation
  dashboard/          Bubbletea TUI (pipeline, agents, activity, escalations)
  engine/             Core orchestration
    planner.go        Tech Lead decomposition
    dispatcher.go     Wave-based parallel dispatch
    watchdog.go       Stuck detection, permission bypass
    supervisor.go     Drift detection, reprioritization
    reviewer.go       Senior code review
    qa.go             Lint/build/test pipeline
    merger.go         PR creation and auto-merge
    reaper.go         Tiered cleanup and GC
  git/                Branch, worktree, and GitHub PR operations
  graph/              Dependency DAG with topological sort
  llm/                Anthropic and OpenAI API clients
  runtime/            Pluggable runtime registry
  state/              Event store (file-based) + SQLite projections
  tmux/               Session management (create, capture, send-keys)
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

## License

[Apache License 2.0](LICENSE)

---

Built with the philosophy: **orchestrate agents like a real agile team.**
