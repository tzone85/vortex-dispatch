# VXD Open-Core Boundary

VXD is a real open-source project, not a crippled demo. The public repository
should remain useful for developers who want to orchestrate coding agents on
their own repositories.

At the same time, Vortex Dispatch develops private commercial software-factory
systems and production intelligence. The two worlds are related, but they are
not mirrors of each other.

This document defines the boundary.

## What VXD is for

VXD is the public orchestration core for developer-controlled environments. It
should make it practical to hand off a software requirement to coding-agent
CLIs and coordinate the resulting work through planning, isolated execution,
review, QA, recovery and delivery.

The public project is also a place to demonstrate engineering ideas openly,
invite contribution and provide a credible technical reference for agent
orchestration.

## What belongs in the public core

Good public-core capabilities include:

- provider-neutral agent/runtime adapters,
- Claude Code, Codex, Gemini CLI and comparable integrations,
- dependency-aware requirement decomposition,
- git worktree isolation,
- generic retry/escalation mechanics,
- basic automated review and QA,
- event history, replay and crash recovery,
- developer dashboards and diagnostics,
- generic cost/status reporting,
- local developer databases and disposable test infrastructure,
- extensibility points,
- public examples, tutorials and reference documentation.

A capability can be sophisticated and still belong in VXD. The test is not
"is this valuable?" The test is whether it primarily makes the open-source
orchestrator better for developers running VXD.

## What stays private

The private Vortex Dispatch software factory may contain capabilities that do
not belong in VXD, including:

- proprietary commercial prompts and role instructions,
- customer-specific delivery or governance policies,
- private deployment architecture,
- production optimisation/routing intelligence,
- cross-customer or cross-project learning systems,
- proprietary benchmark datasets and evaluation results,
- private skill libraries and accumulated production knowledge,
- commercial security/governance controls,
- internal pricing or operational logic,
- unpublished product strategy and roadmaps,
- code whose primary value is operating Vortex Dispatch's commercial factory
  rather than improving the standalone public orchestrator.

Private repository names, filesystem paths and implementation details should
not be used in VXD documentation.

## No parity requirement

VXD and private Vortex Dispatch systems do not need feature parity.

A feature may exist only in VXD because it is useful to the community. A
feature may exist only in the private factory because it serves production or
commercial requirements. A shared concept may be implemented differently in
each codebase.

Do not copy private code into VXD simply to keep two systems synchronised.
Likewise, do not intentionally weaken VXD to manufacture an artificial gap.
The public project should stand on its own merits.

## How to decide where a new idea belongs

Ask these questions in order:

1. **Does this primarily help an individual developer or engineering team run
   VXD on software they control?**
   If yes, it is probably a public-core feature.

2. **Does this primarily improve how Vortex Dispatch operates a multi-customer
   production software factory, accumulates proprietary engineering
   intelligence or enforces commercial governance?**
   If yes, keep it private.

3. **Would publishing the implementation reveal customer information,
   proprietary prompts, private evaluation data or internal strategy?**
   If yes, keep those details private even if a generic version of the idea is
   appropriate for VXD.

4. **Can the idea be expressed as a clean generic interface or independently
   implemented public version?**
   If yes, expose the generic seam without copying the private implementation.

When uncertain, start with the generic interface in VXD and keep production
policy behind the private boundary until there is a clear reason to publish
more.

## Documentation hygiene

Public documentation should contain:

- current public behaviour,
- public configuration,
- contributor guidance,
- examples and troubleshooting,
- architecture needed to understand VXD.

Public documentation should not contain:

- internal Vortex Dispatch strategy,
- private product names or repository paths,
- private implementation notes,
- customer-specific information,
- detailed parity/porting instructions between public and private systems.

## Licensing

VXD remains licensed under Apache License 2.0. Code already released under
Apache 2.0 remains subject to the rights granted by that licence.

The open-core boundary is therefore primarily a rule for **what Vortex
Dispatch publishes from this point forward**, not an attempt to retract code
that has already been released.

## Commercial relationship

VXD demonstrates and advances the public orchestration layer. Vortex Dispatch
may offer commercial software engineering, private factory capabilities,
support, integration, deployment, governance and other services outside this
repository.

The public project should create trust and technical credibility. The private
factory should accumulate the production knowledge and operational advantages
that make Vortex Dispatch commercially differentiated.
