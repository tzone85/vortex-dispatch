# VXD — Public Architecture Overview

VXD is the Apache-2.0 orchestration core for developers running coding agents
on repositories they control. This document covers the public implementation.
The [open-core boundary](OPEN_CORE.md) explains publication policy; the
[package map](architecture.md) provides implementation references.

## Requirement lifecycle

1. The CLI records a requirement and resolves project configuration.
2. The planner decomposes the requirement into stories and dependencies.
3. The dispatcher schedules eligible stories in dependency order.
4. The executor prepares an isolated worktree and invokes a configured agent.
5. Review, QA and security checks assess the resulting change.
6. Retry and escalation handle failures. Human gates depend on the review mode.
7. Delivery opens or merges changes according to configuration, and completion
   verification checks the assembled result.

See [workflows](workflows.md) for commands and approval modes. Individual story
success is not a substitute for checking the assembled application.

## State and recovery

The append-only event log is the source of truth. SQLite stores projections
used by the CLI and dashboards. State-changing events require a projector and
replay tests; adding an event without projecting it leaves views inconsistent.

`vxd events` inspects history, `vxd replay` rebuilds projections, and
`vxd doctor` diagnoses inconsistent or stalled pipelines. Back up state before
repairing it. See [monitoring](monitoring.md) for operator workflows.

Event changes must preserve replay compatibility: prefer additive fields with
safe defaults. Schema evolution, log retention and filesystem capacity need
operator attention. A local state directory is not a distributed lock or a
multi-tenant data boundary.

## Agent execution

Runtime adapters construct commands for coding-agent CLIs; runners execute
them in the selected environment. Agent execution must preserve cancellation,
timeouts, worktree boundaries and explicit failures. Supported providers and
bindings are described in [model selection](model-selection.md).

Worktrees isolate concurrent edits. They do not themselves sandbox arbitrary
commands or isolate host credentials. Choose the runner and credentials to
match the trust level of the workload.

## Review and delivery gates

The public pipeline includes automated review, configured QA commands,
security scanning, retries and escalation. Human review modes control plan
and delivery approval. See [configuration](configuration.md) for the actual
settings; the CLI help and code are authoritative when examples lag.

QA must exercise the changed behavior and its integration with the rest of
the project. Missing tools, unexecuted checks and provider outages must be
distinguished from passing results. A successful agent exit is not proof of
correctness or client acceptance.

## Observability

Event history, terminal status, the TUI and web dashboard expose run state.
The web server includes a `/health` endpoint. Notifications provide configured
outbound status updates. Cost and status reporting describe observed runs;
they are not contractual delivery or availability guarantees.

## Optional public modules

VXD also ships disposable developer databases, repository learning,
self-improvement and experimental autoresearch capabilities. These modules
remain part of the public source distribution. Their presence does not imply
that private production data, policies or commercial systems are published.

See [adaptive routing](adaptive-routing.md),
[autoresearch design](autoresearch-harness-design.md), and the public package
map for implementation details. Public experimental features should be
evaluated against their tests and documented limitations before adoption.

## Data and security

Keep API keys, customer data and runtime output outside source control.
Treat repository content, tool results and model output as untrusted input.
Review provider terms and configure credentials for the deployment in use;
this document makes no claims about third-party retention policies.

The [security policy](../SECURITY.md) explains vulnerability reporting.
Publication checks complement code review; they do not replace secret scans
or a review of old branches, tags and Git history.

## Licensing and publication

VXD remains Apache-2.0. The boundary controls future publication; editing the
current tree does not remove earlier commits or retract previously published
code. Company strategy, internal deployment plans and commercial forecasts
belong outside this repository.
