# VXD (Vortex Dispatch)

**Hand off a software requirement. Let coding agents plan, build, review and test it in isolated worktrees.**

[![Go](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![CI](https://github.com/tzone85/vortex-dispatch/actions/workflows/ci.yml/badge.svg)](https://github.com/tzone85/vortex-dispatch/actions/workflows/ci.yml)

VXD is an open-source AI coding-agent orchestrator built by [Vortex Dispatch](https://vortexdispatch.co.za). It coordinates tools such as Claude Code, Codex and Gemini CLI across the lifecycle of a software change: planning, parallel implementation, review, QA and delivery.

VXD is useful on its own. It is also intentionally the **public core**, not a publication of every private production system, commercial policy or accumulated engineering technique used by Vortex Dispatch. Read the [open-core boundary](docs/OPEN_CORE.md) or the [public explanation](https://vortexdispatch.co.za/open-source).

## Why VXD

A strong coding model can write code. The harder problem is coordinating many pieces of engineering work without turning the repository into a small electrical fire.

VXD adds the orchestration layer:

- **Dependency-aware planning** turns a requirement into stories that can run in parallel where possible.
- **Isolated git worktrees** keep concurrent agents from trampling each other's changes.
- **Pluggable agent runtimes** let teams use the coding CLIs they already prefer.
- **Automated review and QA** validate work before delivery.
- **Escalation and human gates** give failed or uncertain work somewhere sane to go.
- **Event-sourced state** makes runs inspectable, resumable and replayable.
- **TUI and web dashboards** make active requirements observable without living in terminal scrollback.
- **Optional ephemeral databases** isolate database-touching stories from one another.

VXD does not promise that autonomous agents never fail. It is designed around the more useful assumption that they sometimes will, and that failures should be visible, recoverable and governable.

## Demo

![VXD Demo](https://vhs.charm.sh/vhs-23AYbABlUZ9ssvssNj9pWX.gif)

See the [tutorial](docs/tutorial.md) for a full walkthrough.

## Quick start

### Prerequisites

- Go 1.23+
- git and GitHub CLI (`gh`)
- tmux for the full macOS/Linux agent pipeline
- at least one supported coding-agent CLI configured for your environment

Then:

```bash
go install github.com/tzone85/vortex-dispatch/cmd/vxd@latest
vxd init
vxd preflight
vxd req "Add a health endpoint with tests and documentation"
```

For local development from source:

```bash
git clone https://github.com/tzone85/vortex-dispatch.git
cd vortex-dispatch
go build -o ~/.local/bin/vxd ./cmd/vxd
vxd preflight
```

`vxd req` can accept an inline requirement or a file:

```bash
vxd req "Build a REST API for user management"
vxd req --file requirements.md
cat requirements.md | vxd req -f -
```

## How it works

```text
Requirement
    |
    v
 Planning
    |
    v
Dependency DAG
    |
    v
Parallel agent worktrees
    |
    v
 Review -> QA -> delivery gate
    |
    v
 PR / merge / human decision
```

The exact path depends on your configuration and review mode. VXD keeps an append-only event history and uses SQLite projections for current state, so a run can be inspected and recovered without treating the terminal session as the source of truth.

## Agent providers

VXD can drive multiple coding-agent CLIs through configuration. Common setups include:

| Provider | Typical runtime | Authentication |
|---|---|---|
| Anthropic | Claude Code CLI | Claude Code login/subscription or API configuration |
| OpenAI | Codex CLI | Codex/ChatGPT login |
| Google | Gemini CLI | Google AI credentials |
| Custom | YAML-described CLI runtime | Runtime-specific |

Provider availability and model names change quickly. Run `vxd preflight` and use current provider documentation rather than assuming an old model identifier is still valid.

## Platform support

| Platform | CLI / read-only commands | Full tmux pipeline |
|---|---:|---:|
| macOS | Yes | Yes |
| Linux | Yes | Yes |
| Windows native | Yes | No |
| Windows + WSL2 | Yes | Yes |

The full local execution pipeline currently depends on tmux. Windows users should use WSL2 for the same Linux workflow.

## Core commands

```text
vxd init                 initialise a workspace
vxd req                  submit a requirement
vxd status               inspect requirements and stories
vxd watch                tail a run in the terminal
vxd dashboard            open the TUI status surface
vxd dashboard --web      open the browser dashboard
vxd pause / vxd resume   control a running requirement
vxd preflight            validate the environment
vxd doctor               diagnose common pipeline problems
vxd estimate             estimate work/cost
vxd metrics              inspect run metrics
vxd report               generate delivery reports
vxd events               inspect the event history
vxd replay               rebuild projections from the event log
vxd backup               archive project state
vxd config               inspect and validate configuration
```

Run `vxd --help` and `vxd <command> --help` for the current command surface. The repository's [public agent guide](CLAUDE.md) also documents the command names required by the project's documentation tests without publishing private Vortex Dispatch implementation strategy.

## Review modes

VXD supports different levels of human involvement:

| Mode | Plan approval | PR approval | Use case |
|---|---|---|---|
| `auto` | No | No | trusted local pipelines |
| `plan_only` | Yes | No | inspect decomposition before execution |
| `manual` | Yes | Yes | maximum human control |

Example:

```yaml
merge:
  auto_merge: false
  review_mode: manual
  base_branch: main
```

Use autonomous merge settings only in repositories where you are comfortable with the risk. "The agent sounded confident" remains a terrible change-management policy.

## Configuration

Run `vxd init` to generate `vxd.yaml`. The top-level sections are:

| Section | Purpose |
|---|---|
| `workspace` | state location, storage and logging |
| `models` | provider/model bindings for public agent roles |
| `routing` | complexity thresholds, concurrency and retries |
| `monitor` | polling, stuck detection and pipeline timeouts |
| `cleanup` | worktree, branch and log cleanup |
| `merge` | base branch, merge behaviour and review mode |
| `planning` | story decomposition and planning controls |
| `runtimes` | coding CLI definitions and execution targets |
| `billing` | estimates, rates and spend limits |
| `qa` | build/test checks and completion verification |
| `security` | public security gate and scanner controls |
| `sla` | optional story-duration limits |
| `secrets` | environment/Vault secret provider configuration |
| `notify` | optional notification settings |
| `autoresearch` | experimental research/experiment-loop settings |
| `devdb` | optional ephemeral database configuration |
| `dashboard` | web dashboard startup, port and token settings |
| `improve` | experimental opt-in improvement tooling |

See [docs/configuration.md](docs/configuration.md) for detailed options and the generated config for current defaults.

## Ephemeral databases

Database-touching stories can optionally receive isolated Postgres environments so migrations and destructive tests do not share one mutable database across agents. Configure the `devdb` section when that isolation is useful; leave it disabled for projects that do not need it.

## Recovery and observability

VXD is designed so a failed terminal or agent process is not the end of the story.

Useful commands:

```bash
vxd status
vxd watch
vxd doctor
vxd events
vxd backup
vxd replay --dry-run
```

The web and terminal dashboards are views over persisted state, not the only place that state exists.

## Open source and the Vortex Dispatch factory

VXD is Apache-2.0 software. You may use, modify and redistribute it under the terms of that license.

Vortex Dispatch also operates private commercial engineering systems. Those private systems can contain production policies, proprietary prompts, customer-specific governance, accumulated operational data and software-factory intelligence that are deliberately not mirrored into this repository.

That boundary is intentional:

- **VXD should remain genuinely useful open-source software.**
- **Private Vortex systems do not need feature parity with VXD.**
- **A useful public abstraction does not require publishing every internal implementation.**
- **Customer or cross-project learning never belongs in the public repository.**

Read [`docs/OPEN_CORE.md`](docs/OPEN_CORE.md) for contributor rules, or visit [vortexdispatch.co.za/open-source](https://vortexdispatch.co.za/open-source) for the commercial explanation.

## Documentation

- [Getting started](docs/getting-started.md)
- [Tutorial](docs/tutorial.md)
- [Workflows](docs/workflows.md)
- [Configuration](docs/configuration.md)
- [Agents and roles](docs/agents-and-roles.md)
- [Monitoring](docs/monitoring.md)
- [Architecture](docs/architecture.md)
- [Contributing](CONTRIBUTING.md)
- [Open-core boundary](docs/OPEN_CORE.md)

## Contributing

Contributions to the public VXD core are welcome. Start with [CONTRIBUTING.md](CONTRIBUTING.md).

Before proposing a feature, check the open-core boundary. A contribution can improve VXD significantly without attempting to reproduce private Vortex Dispatch factory systems.

## Security

Do not commit API keys, tokens, customer data, private repository URLs or machine-specific secrets. Follow [SECURITY.md](SECURITY.md) for vulnerability reporting if present, and use the repository's normal issue process for non-sensitive bugs.

## About Vortex Dispatch

[Vortex Dispatch](https://vortexdispatch.co.za) is a software engineering company in Cape Town building production software with agent-orchestrated engineering systems.

VXD is the open-source part of that story. The commercial offering is the engineering capability and software outcomes, not a requirement that clients adopt VXD themselves.

## License

[Apache License 2.0](LICENSE)
