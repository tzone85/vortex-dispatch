# VXD Codebase Audit, Documentation & Architecture Diagrams

**Date:** 2026-05-11
**Status:** Approved (design phase)
**Owner:** Thando Mini

## 1. Purpose

Deep-audit the VXD codebase to surface bugs, dead code, wiring gaps, and integration regressions; refresh documentation; produce rendered architectural diagrams (no Mermaid); then perform three progressive sweeps to verify nothing was missed. Confirm self-improvement and MemPalace integrations are live and producing value, not just compiled.

## 2. Non-Goals

- New features
- Performance benchmarking
- E2E browser testing
- Refactors unrelated to audit findings
- Mermaid diagrams (explicit user constraint — use d2, plantuml, dot only)

## 3. Phases

### Phase 0 — Worktree

```bash
git worktree add -b feat/codebase-audit-2026-05 /tmp/vxd-audit
cd /tmp/vxd-audit
```

All work occurs in worktree. Single PR to main at end.

### Phase 1 — Baseline Health (read-only)

Capture current state before changing anything:

```bash
go build ./...                       # capture build output
go test ./... -count=1 -timeout 5m   # capture test output
go vet ./...                         # capture vet warnings
staticcheck ./...                    # capture static analysis (install if missing)
deadcode ./...                       # capture dead code (install if missing)

vxd preflight                        # 12 checks
launchctl list | grep vxd            # self-improve cron status
ls -la docs/self-improvement/runs/   # daily run files
vxd improve runs --since 7d          # last week of self-improve
vxd autoresearch status              # autoresearch budget + posterior
```

Output appended to `docs/audits/2026-05-11-findings.md` as a Baseline section.

### Phase 2 — Deep Audit

Findings doc structure:

```markdown
# Codebase Audit 2026-05-11

## Baseline (Phase 1 output)
## Build/Test Failures
## Wiring Gaps
  - Events emitted but not handled in sqlite.go Project() switch
  - CLI commands missing from CLAUDE.md
  - Config fields missing from README.md Configuration table
  - Doc-coverage wiring test additions needed
## Security Findings
  - Hardcoded secrets scan
  - Unquoted shell args (per CLAUDE.md ValidateShellArg pattern)
  - SQL injection (parameterized queries audit)
  - Environment variable leakage
## MemPalace Integration Health
  - Drawer count, last index time, search latency
  - Auto-mine wiring (from commit 0ecf617)
## Self-Improvement Health
  - Actionable rate (last 7 daily runs)
  - Abort rate + root causes
  - Email send status (RESEND_API_KEY present, last email sent)
  - PR generation rate
## Autoresearch Health
  - Coordinator running? Budget remaining? program.md updated?
## Dead Code (initial scan)
## Tech Debt
  - Files > 800 lines
  - Functions > 50 lines
  - Nesting > 4 levels
```

Each finding gets a severity tag: `CRITICAL`, `HIGH`, `MEDIUM`, `LOW`, `INFO`.

### Phase 3 — Fix Waves (TDD)

Order strict:

**Wave A — Build/Test Green**
- Resolve any build failures
- Fix failing tests (TDD: confirm test catches the bug, then fix)
- Gate: `go build ./... && go test ./... -count=1` exits 0

**Wave B — Wiring + Security**
- Add wiring tests for unhandled events
- Add doc-coverage wiring tests for missing CLI cmds and config fields
- Patch security findings (CRITICAL + HIGH only — MEDIUM/LOW logged for follow-up)
- Gate: new wiring tests pass

**Wave C — Integration Health**
- Fix MemPalace integration issues
- Fix self-improvement abort root causes
- Fix autoresearch coordinator if stalled
- Gate: live system checks all pass (see §6)

### Phase 4 — Documentation Refresh

- Regenerate CLI commands table in CLAUDE.md from `internal/cli/root.go`
- Regenerate config table in README.md from `internal/config/config.go`
- Refresh `docs/architecture.md` package overview from `go list ./...`
- Verify `docs/agents-and-roles.md` matches current `internal/agent/`
- Link new diagrams from getting-started.md, architecture.md
- Refresh "Pending Work" section in CLAUDE.md based on actual state

### Phase 5 — Architecture Diagrams

Tooling split (user-confirmed):

| Tool | Purpose | Files |
|------|---------|-------|
| **d2** | Static architecture | `arch-overview.d2`, `pipeline-flow.d2`, `escalation-tiers.d2` |
| **plantuml** | Sequence diagrams | `sequence-dispatch.puml`, `sequence-escalation.puml`, `sequence-merge.puml`, `sequence-improve.puml`, `sequence-autoresearch.puml` |
| **graphviz dot** | Package dependency graph | `package-deps.dot` |

Rendered outputs:
- d2 → `.svg`
- plantuml → `.png`
- dot → `.svg`

Build script `docs/diagrams/render.sh` regenerates all artifacts. Source files committed alongside rendered output.

### Phase 6 — Three Progressive Sweeps

Each sweep runs after Wave C is green. Output appended to `docs/audits/2026-05-11-sweeps.md`.

**Sweep 1 — Dead Code**
- `deadcode ./...` → list unused funcs
- `staticcheck -checks U1000 ./...` → unused identifiers
- Delete confirmed dead funcs/files
- Re-run baseline tests after each batch

**Sweep 2 — Stale Tests + Comments + TODOs**
- Find `*_boost_test.go` / `*_coverage_test.go` (per memory: auto-generated stale)
- Find commented-out code blocks (>3 consecutive commented lines)
- Find `TODO` / `FIXME` / `XXX` markers — resolve, convert to issue, or accept with justification comment
- Re-run tests

**Sweep 3 — Wiring Gap Pass (final)**
- For every event type in `internal/state/events.go`: grep `sqlite.go` for handler; assert wiring test exists
- For every `newXxxCmd()` in `internal/cli/root.go`: assert appears in CLAUDE.md CLI table
- For every top-level field in `Config` struct: assert appears in README.md Configuration table
- Assert `TestDocCoverage_*` tests cover all of the above
- Expected outcome: zero new findings (otherwise loop back to Wave B)

### Phase 7 — PR

Single PR `feat/codebase-audit-2026-05 → main`. Title: `chore: codebase audit, doc refresh, architecture diagrams`. Body lists findings counts, fixes per wave, sweep deltas.

## 4. Deliverables

**New files:**
- `docs/audits/2026-05-11-findings.md`
- `docs/audits/2026-05-11-sweeps.md`
- `docs/diagrams/render.sh`
- `docs/diagrams/arch-overview.d2` + `.svg`
- `docs/diagrams/pipeline-flow.d2` + `.svg`
- `docs/diagrams/escalation-tiers.d2` + `.svg`
- `docs/diagrams/sequence-dispatch.puml` + `.png`
- `docs/diagrams/sequence-escalation.puml` + `.png`
- `docs/diagrams/sequence-merge.puml` + `.png`
- `docs/diagrams/sequence-improve.puml` + `.png`
- `docs/diagrams/sequence-autoresearch.puml` + `.png`
- `docs/diagrams/package-deps.dot` + `.svg`
- New wiring tests in `internal/engine/wiring_test.go` (one per gap found)

**Updated files:**
- `CLAUDE.md` (CLI table, config table, pending work)
- `README.md` (config table, diagram links)
- `docs/architecture.md` (diagram links, package overview)
- `docs/getting-started.md` (diagram links)
- Source files per audit fixes

**Removed files (after sweeps):**
- `*_boost_test.go`, `*_coverage_test.go` (per memory: auto-generated stale)
- Dead funcs/files identified by deadcode

## 5. Testing Strategy

**TDD per fix:** failing test → minimal fix → green → commit.

**Coverage:** must not drop below current baseline. Capture baseline coverage in Phase 1.

**Wiring tests:** every gap found in Sweep 3 gets a wiring test. Pattern:
```go
func TestWiring_<EventName>_HandledInProjection(t *testing.T) {
    // assert event appears in sqlite.go Project() switch
}
```

**Gates between phases:** see §3. Each gate is a hard stop.

## 6. Live System Verification Checks

Before declaring Wave C complete, all must pass:

```bash
vxd preflight                              # 12 checks, all CRITICAL pass
launchctl list | grep vxd-improve          # cron loaded
test -f docs/self-improvement/runs/$(date +%Y-%m-%d).json  # today ran
vxd improve runs --since 7d                # >0 actionable findings last week
vxd autoresearch status                    # if any repo tracked, returns valid state
vxd memory                                 # opens dashboard, MemPalace accessible
vxd metrics                                # projection query works
```

## 7. Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| Audit reveals show-stopper bug | Wave A fixes before continuing; PR is partial if needed |
| Self-improvement cron not running | Document re-install steps; restore via launchctl load |
| MemPalace index corrupted | Document rebuild command; out-of-scope to rebuild data |
| Dead-code tool false positives | Manual review before deletion; test after each batch |
| Diagram rendering tool missing | Tools confirmed installed (`dot`, `plantuml`, `d2`) at design time |
| Worktree drift from main | Final rebase before PR; CI runs on merge |

## 8. Success Criteria

- `go build ./... && go test ./... -count=1` green on PR branch
- All 3 sweeps complete; Sweep 3 finds zero new wiring gaps
- All live system checks (§6) pass
- All diagrams render and are committed
- All docs (CLAUDE.md, README.md, architecture.md) reflect current code
- PR merged to main

## 9. Out of Scope

- New features
- NXD port of any fixes (separate session, per existing pattern)
- Mermaid diagrams
- Browser-based E2E
- Performance regressions / benchmarks
- Refactors not surfaced by audit
