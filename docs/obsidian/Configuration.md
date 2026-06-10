---
tags: [operations, reference, configuration]
---

# Configuration

VXD is configured via `vxd.yaml` (`internal/config/`). Defaults come from
`DefaultConfig()`; `Validate()` enforces invariants.

## Key sections
```yaml
workspace:
  state_dir: ~/.vxd        # validated: no shell metachars / ".." traversal
  backend: sqlite
models:
  tech_lead: {provider: anthropic, model: claude-opus-4-20250514}
  senior:    {provider: anthropic, model: claude-sonnet-4-20250514}
  junior:    {provider: google, model: gemma-4-27b-it}
routing:
  junior_max_complexity: 3
  max_retries_before_escalation: 2
monitor:
  stuck_threshold_s: 600   # AGENT_STUCK threshold
planning:
  max_story_complexity: 5
merge:
  auto_merge: true
  review_mode: auto        # auto | manual | plan_only
qa:
  success_criteria:        # declarative checks evaluated as pure functions
    - {kind: output_contains, value: "PASS"}
```

## Autoresearch (spend control)
```yaml
autoresearch:
  enabled: false
  budget: 5m               # per-experiment timeout
  parallel: 2
  max_experiments: 0       # hard cap on total experiments (0 = unlimited)
  gate: winning            # auto | winning | pr
```
A consecutive-failure circuit breaker (default 10) also bounds runaway runs.

## Ephemeral databases (devdb)
```yaml
devdb:
  provider: null           # ghost | docker | null
  template: ""             # source DB to fork from (required when provider != null)
  docker:
    image: postgres:16
    host_port_range: "5500-5599"
    host: "localhost"      # set to a VM IP for Colima/Lima
```
> The Docker provider binds Postgres to **127.0.0.1** by default (loopback only).
> A non-loopback `host` (a VM IP) binds `0.0.0.0` so cross-host access works.
> See [[Security Model]].

## Secrets
Sourced from env (default) or HashiCorp Vault (`secrets.provider: vault`). Never
commit a live `vault_token` to `vxd.yaml`.

## Related
- [[CLI Commands]] · [[Operations Runbook]] · [[Documentation Enforcement]]
