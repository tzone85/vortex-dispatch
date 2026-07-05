# Autonomous Software Development with VXD — Full Training Guide

VXD makes building real software a breeze: submit natural language, get production-ready merged changes with full audit trail, zero babysitting.

This training expands the tutorial into mastery of the sophisticated subsystems.

## Core Loop (repeatable & observable)

1. `vxd init` (once per machine/workspace)
2. `vxd preflight` — 18 checks, now includes disk space + tmux server health.
3. `vxd estimate "..."` — quick cost sanity.
4. `vxd req "..."` (or --file) — plans, dispatches, monitors, reviews, QAs, merges.
5. Dashboard opens automatically (or `vxd watch`, `vxd dashboard`).
6. If pause or fail: `vxd resume <id>`, `vxd retry <story>`, escalations visible.
7. `vxd report <id>` — beautiful delivery artifact.

All state is event-sourced: replay, audit, resume after any crash.

## The 5-Tier Escalation (never silently fails)

See diagrams/escalation-tiers.svg and agents-and-roles.md.

- Tier 0: same agent + smart diagnosis (8 error categories)
- Tier 1: senior model
- Tier 2: manager re-diagnoses + rewrites story
- Tier 3: tech lead splits story
- Tier 4: hard pause for you

`vxd escalations` and events show history.

## Advanced Subsystems (all wired, no stubs)

- **Autoresearch**: experiment harness for self-tuning agents on your repo. `vxd autoresearch start`, `evolve` (now fully invokes ProgramMDEvolver + PRs).
- **DevDB**: per-story ephemeral Postgres (docker or ghost). Destructive migrations are safe. `vxd db ...`
- **Security Gate**: scanners + LLM threat model. Auto-learns. `vxd security scan`
- **Repo Learn**: `vxd learn` builds persistent profile for better plans.
- **Self-Improve**: `vxd improve` daily research + proposals.
- **Reporters + Metrics**: full observability.
- **Event projections + SQLite**: fast queries, consistent even on replay.

All exercised by unit + wiring tests.

## Ease of Use Polish

- `vxd init` now prints product/marketing recipes and next steps.
- Consistent observable outputs on repeated runs (event log + projections deterministic).
- `--skip-preflight`, per-run overrides.
- Headless friendly: no browser if no TTY.

## Hands-on Exercises

1. Small feature: health endpoint (as in tutorial).
2. Medium: add a new API resource with tests + migration (uses devdb).
3. Cross-cutting: refactor + security scan + docs update.
4. Failure injection: give impossible acceptance criteria, watch escalation + resume.
5. Marketing crossover: build a docs site generator as part of your app.

## Configuration Mastery

See configuration.md. Key for breeze:
- models per role
- sla.auto_escalate: true for hands-off
- notify.* for Slack on complete/stalled
- autoresearch for your custom agent "DNA"

## Observability & Intervention

- Live: dashboard (web + TUI), watch
- Historical: events, metrics, report, logs
- Safe pause/resume/retry

## Extending

Add new runtime in runtime/registry.go, new prompt variants in agent/, new checks in preflight/.

Update CLAUDE.md (enforced by doc coverage).

## Mastery Checklist

- [ ] Can submit, pause, resume, report a full req end-to-end
- [ ] Understand every event type and projection
- [ ] Tune models + SLA for your budget/quality
- [ ] Use autoresearch + improve to make VXD better at *your* domain over time
- [ ] Treat marketing/product launches identically to backend features

With VXD, building software (and marketing products) is dramatically more mature, observable, and low-friction than raw agents or other orchestrators.

Continue with workflows.md for stage details and product-marketing-made-easy.md .
