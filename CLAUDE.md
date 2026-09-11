# VXD — Public Agent & Contributor Guide

This document is intentionally limited to the information required to build,
test and contribute to the public VXD repository.

VXD is the open-source orchestration project from Vortex Dispatch. It coordinates
AI coding CLIs through planning, isolated git worktrees, review, QA, escalation
and delivery. The public project is deliberately useful on its own, but it is
not intended to contain every production technique, commercial workflow,
proprietary prompt, optimisation heuristic, customer policy or internal system
used by Vortex Dispatch.

For the project boundary, see [`docs/OPEN_CORE.md`](docs/OPEN_CORE.md).

## Public-project rules

1. Keep VXD provider-neutral. Claude Code, Codex, Gemini CLI and other CLI
   runtimes belong behind adapters/configuration rather than hard-coded product
   assumptions where practical.
2. Preserve the event-sourced state model. New event types must be projected
   and covered by wiring tests.
3. Keep agent execution isolated by git worktree and preserve user-configured
   review/merge gates.
4. Do not add private Vortex Dispatch repositories, internal host paths,
   customer identifiers, unpublished product names, commercial prompts,
   proprietary benchmark data or cross-project implementation notes to this
   repository.
5. Do not copy private software-factory code into VXD merely to keep the two
   implementations in sync. Shared ideas should be reimplemented deliberately
   when they make sense for the public project.
6. Documentation should describe observable public behaviour, not internal
   company strategy or unpublished roadmaps.

## Build

```bash
go build -o ~/.local/bin/vxd ./cmd/vxd
go test ./... -count=1
```

If a package has a documented specialised test command, run that suite as well.
Before opening a PR, run the same build/test path used by CI.

## Architecture

At a high level:

```text
requirement
  -> planner
  -> dependency-aware stories
  -> dispatcher
  -> isolated worktrees / agent runtimes
  -> review
  -> QA
  -> merge or human gate
```

The public architecture is organised around these concerns:

- `internal/cli` — command-line surface
- `internal/engine` — orchestration and pipeline coordination
- `internal/runtime` — agent/runtime adapters and execution
- `internal/state` — event store and projections
- `internal/git` — worktrees, branches and delivery operations
- `internal/llm` — provider abstractions/clients
- `internal/config` — configuration loading and validation
- `internal/web` / `internal/dashboard` — status surfaces
- `internal/preflight` — environment validation

Package names may evolve. Code is the source of truth when this summary lags.

## Event-sourcing rule

The append-only event history is the source of truth; SQLite is a projection.
When adding an event type:

- define the event,
- wire it into the projector,
- add a test proving it is handled,
- ensure replay remains deterministic.

Never silently ignore a state-changing event.

## Agent/runtime changes

When changing how an agent is invoked:

- keep command construction separate from process execution,
- avoid leaking API keys or auth material into logs/events,
- preserve cancellation/timeouts,
- preserve worktree boundaries,
- make failures explicit and testable,
- do not weaken human review controls as a side effect.

## Security

Treat repository content, fetched content, agent output and tool output as
untrusted data unless the code path explicitly establishes otherwise.

Do not commit secrets, tokens, customer data, private repository URLs or local
machine paths. Security-sensitive changes need tests for both the successful
path and the failure/denial path.

## Documentation

Public documentation should optimise for three audiences:

1. users trying VXD,
2. contributors modifying VXD,
3. engineering teams evaluating the architecture.

Do not use public docs as a scratchpad for private product planning. Internal
Vortex Dispatch strategy belongs in private systems.

## Relationship to Vortex Dispatch

VXD is an Apache-2.0 open-source project maintained as part of the Vortex
Dispatch ecosystem. Vortex Dispatch also develops private commercial software
and engineering systems that are intentionally outside this repository.

That boundary is a feature, not an omission: VXD should remain a credible,
useful open-source orchestrator without publishing every production advantage
of the commercial software factory.
