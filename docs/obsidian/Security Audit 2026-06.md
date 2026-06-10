---
tags: [security, audit]
---

# Security Audit 2026-06

A full-repository security and quality audit and its remediation. The canonical
record (with commit references) is
`docs/audits/2026-06-10-security-audit-and-remediation.md`; this note is the
vault-linked summary.

## Headline fixes
- **C1 — Unauthenticated dashboard control channel** → token gate. See
  [[Dashboard Authentication]].
- **C3 / H7 — Unbounded autoresearch spend** → circuit breaker + `max_experiments`
  cap. See [[Operations Runbook]].
- **H1 — Path traversal via LLM story IDs** → `state.ValidateStoryID`. See
  [[Security Model]].
- **H2 — Dev DB exposed on `0.0.0.0`** → loopback by default. See [[Configuration]].
- **H4 — No projection rebuild path** → `vxd rebuild` + idempotent projection +
  busy timeout. See [[Event Sourcing]].
- **H5/H6 — Two silently-broken features** → `gitPullWithStash` dir fix and
  integration-fixer context detachment.

## Hardening
Input screening at every untrusted boundary (agent log, scraped postings);
HTTP timeouts + bounded reads on all LLM/email clients; devdb name validation
with correct identifier quoting and URL escaping; atomic `STORY_STARTED` guard.

## CI integrity
`internal/cli` and `internal/improve` were excluded from CI and actually failing
(non-hermetic git/gh deps). Root causes fixed; both un-excluded so the full
`go test ./...` runs on every push. Workflow permissions narrowed to
`contents: read`; advisory `govulncheck` job added. See [[Testing Conventions]].

## Tracked follow-up
- Per-DB least-privilege Postgres role (H3 full fix).
- `nhooyr.io/websocket` → `coder/websocket` migration.
- `monitor.go` refactor (1800+ lines).
- Remove unimplemented `dolt` backend from config validation.

## Related
- [[Security Model]] · [[VXD MOC]]
