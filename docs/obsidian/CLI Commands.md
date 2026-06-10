---
tags: [operations, reference]
---

# CLI Commands

The VXD command surface (cobra, `internal/cli/`). The authoritative table lives
in `CLAUDE.md` and `README.md`; this note groups commands by task.

## Lifecycle
- `vxd init` — initialize workspace, generate default `vxd.yaml`
- `vxd req "…"` — submit a requirement (auto-dispatches when `review_mode=auto`)
- `vxd resume <req-id>` — resume a paused pipeline
- `vxd pause <req-id>` — pause a running requirement
- `vxd status` — requirement and story status

## Review gates
- `vxd approve <story-id>` / `--all <req-id>` — approve a PR for merge
  (validates the story exists, is `awaiting_approval`, and has a PR **before**
  requiring the `gh` CLI)
- `vxd approve-plan` / `vxd reject-plan` — plan-level gates
- `vxd review <story-id>` / `vxd reject <story-id>` — PR diff review

## Observability
- `vxd dashboard [--web]` — TUI or browser dashboard (see [[Dashboard Authentication]])
- `vxd metrics`, `vxd events`, `vxd escalations`, `vxd logs <req-id>`
- `vxd estimate "…"`, `vxd report <req-id>`

## Maintenance & recovery
- `vxd preflight` — 15 pre-flight checks
- `vxd rebuild` — rebuild the SQLite projection from `events.jsonl` (recovery for
  log↔projection divergence; see [[Event Sourcing]])
- `vxd backup`, `vxd gc [--dry-run]`, `vxd archive`

## Subsystems
- `vxd db …` — ephemeral story databases (see [[Configuration]])
- `vxd autoresearch start <repo> [--max-experiments N]` — experiment loop with
  spend cap (see [[Operations Runbook]])
- `vxd improve …` — self-improvement changelog
- `vxd learn [path]` — repo analysis
- `vxd config show|validate`

> Adding a command requires a doc update — see [[Documentation Enforcement]].
