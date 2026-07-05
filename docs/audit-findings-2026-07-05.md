# VXD Audit Findings — 2026-07-05

**Project:** [vortex-dispatch](https://github.com/tzone85/vortex-dispatch) (VXD) — AI agent orchestration CLI  
**Audit scope:** Committed codebase at HEAD `ebfa34c` (read-only static analysis + test runs)  
**Purpose:** Hand this document to another LLM or reviewer to continue uncovering shortcomings, wiring gaps, and errors.

---

## How to use this document

1. Treat **code + tests** as ground truth when docs disagree.
2. Each finding includes **severity**, **location**, and **evidence** so you can verify independently.
3. Sections marked **INTENTIONAL DEFERRAL** are documented placeholders — distinguish from accidental gaps.
4. Suggested follow-up areas for deeper review are listed at the end.

### Verification baseline (captured during audit)

| Command | Result |
|---------|--------|
| `go test $(go list ./... \| grep -v improve) -count=1` | PASS (28 packages) |
| `go test ./internal/engine/ -run TestDocCoverage -count=1` | PASS (4/4) |
| `vxd preflight` | PASS (exit 0, 3 warnings) |
| `vxd config validate` | PASS (exit 0) |
| `make verify` | FAIL locally — `golangci-lint` not on PATH |
| `go test ./test/ -run TestAudit -count=1` | 7 PASS / 2 FAIL (doc-drift encoding tests) |

Structural audit tests live in `test/audit_structural_test.go`.

---

## Executive summary

**Strengths:** The core orchestration pipeline (requirement → plan → dispatch → executor → monitor → review/QA/merge → escalation → event projection) is substantially wired. `internal/engine/wiring_test.go` has 2400+ lines of activation tests. `resume.go` injects Manager, Planner, TechLeadFixer, SecurityGate, DevDB lifecycle, and optional CodeGraph into the monitor.

**Weaknesses:** Documentation contradictions (especially AGENTS.md vs CLAUDE.md vs architecture-overview), v1 autoresearch stubs, unwired config knobs (`notify_on_complete`), graceful degradation paths that reduce security coverage, and missing operational guards (disk space, schema versioning, distributed locking).

---

## 1. Errors / bugs

| Sev | ID | Finding | Location | Evidence |
|-----|-----|---------|----------|----------|
| HIGH | E-01 | `make verify` fails without `golangci-lint` on developer PATH | `Makefile:35` | Local run exits 1 before test suite |
| HIGH | E-02 | (IMPROVED) Autoresearch baseline now neutral 0.5 (from 0) to avoid skew; full main remeasure remains v2. | internal/cli/autoresearch.go |
| HIGH | E-03 | (FIXED) Autoresearch MergePR for auto gate now implemented (parses URL, delegates to git.MergePR); correct outcomes on real path. | internal/autoresearch/gate.go | 
| HIGH | E-04 | Event projector `default` case silently swallows unknown event types | `internal/state/sqlite.go:344-346` | Logs WARNING, returns nil — no operator-visible failure |
| MED | E-05 | Corrupt `owned_files` JSON may leave dispatcher with nil ownership | `internal/state/sqlite.go:372-377` | Unmarshal error logged; `OwnedFiles` may be nil → file-conflict races |

---

## 2. Incomplete or stub wiring

| Sev | ID | Finding | Location | Evidence |
|-----|-----|---------|----------|----------|
| HIGH | W-01 | (FIXED) `vxd autoresearch evolve` wires ProgramMDEvolver; auto-gate MergePR now parses URL + calls real git.MergePR (DefaultGateOps exercised in tests with correct non-error outcomes). Stub removed. | (post wiring) |
| HIGH | W-02 | `notify_on_complete` config field **never read** — REQ_COMPLETED notifications unwired | `internal/config/config.go:58` | `grep NotifyOnComplete` → only config definition; no usage in `internal/engine/` or `internal/cli/` |
| HIGH | W-03 | Slack notifier only enabled when **both** `slack_webhook_url` AND `notify_on_sla` are true | `internal/cli/resume.go:411-415` | Otherwise `NoopNotifier` — stall/complete notifications also noop |
| MED | W-04 | CodeGraph wired but **no-op** when binary unavailable | `internal/cli/resume.go:419-423` | Only `SetCodeGraph` when `cg.Available()`; `wiring_test.go:1289-1303` graceful degradation |
| MED | W-05 | Security gate **degrades** when scanners missing (warns, does not block dispatch) | `internal/engine/security_gate.go:86`, `internal/preflight/checks.go:330-357` | Preflight WARNING; gate logs "scan coverage lost" |
| MED | W-06 | Ghost devdb template preflight **skipped** — failures only at fork time | `internal/preflight/checks.go:448-453` | Returns INFO "verified at fork time" for non-docker providers |
| MED | W-07 | Several wiring tests defer to `t.Log` instead of asserting | `internal/engine/wiring_test.go` | e.g. metrics CLI (~607), security metacharacter (~669) |
| LOW | W-08 | `EventSupervisorReprioritize` removed as dead code (was never emitted) | `internal/state/events.go:60-62` | Comment documents removal |

### Orchestration path status

| Stage | Status | Key files |
|-------|--------|-----------|
| Requirement intake | **Wired** | `internal/cli/req.go` → `runDispatchPreflight` → `runResume` |
| Planning | **Wired** | `internal/engine/planner.go`; archaeology, repo profile, DDD-TDD defaults |
| Dispatch | **Wired** | `internal/engine/dispatcher.go`; reputation routing, overlap filtering |
| Executor / runtime | **Wired** | `internal/engine/executor.go:205-206` `detectBugFix`/`detectInfrastructure`; adapter/runner separation |
| Monitor | **Wired** | `internal/cli/resume.go`; Manager, Planner, TechLeadFixer, SecurityGate, DevDB |
| Review / QA / merge | **Wired** | QA emits quality scores; post-merge integration build (Go/Node/Rust/Python) |
| Escalation | **Wired** | 5-tier chain; tier 2+ intercepted in `monitor_dispatch.go:114-168` |
| Event projection | **Mostly wired** | 60+ explicit cases in `sqlite.go`; informational events return nil by design |
| Autoresearch | **Wired** | Events projected; coordinator + GateRouter wired; evolve + auto-gate (real MergePR via DefaultGateOps) produce correct outcomes; tests drive shipped impl |
| Self-improvement | **Experimental** | README: "no actionable improvements produced to date" |

---

## 3. Operational / resilience gaps

| Sev | ID | Gap | Location / notes |
|-----|-----|-----|------------------|
| HIGH | O-01 | No **disk-space** preflight before event-log append | `docs/architecture-overview.md:1373`; no match in `internal/preflight/` |
| HIGH | O-02 | No **event schema versioning** — silent data loss on payload evolution | `docs/architecture-overview.md:1380-1390`; no `schema_version` in `internal/state/` |
| HIGH | O-03 | Lock file is **local PID only** — no network-aware locking for shared state dirs | `docs/architecture-overview.md:1375`; `internal/engine/` lock helpers |
| MED | O-04 | Native **Windows** cannot run pipeline without WSL2 (no tmux) | `README.md:116-120` |
| MED | O-05 | Docker devdb on Windows requires explicit `DOCKER_HOST` (named pipe default) | `README.md:139-142` |
| MED | O-06 | No **tmux server health** preflight check | `docs/architecture-overview.md:1376` |
| MED | O-07 | SQLite schema mismatch after upgrade — no automated migration gate on event replay | `docs/architecture-overview.md:1374` |
| MED | O-08 | Concurrent `vxd resume` on networked filesystem — lock may not protect | `docs/architecture-overview.md:1375` |
| LOW | O-09 | Event log unbounded growth — compaction not implemented | `docs/architecture-overview.md:1318` |
| LOW | O-10 | Backup/restore workflow documented as Phase 2 only | `docs/architecture-overview.md:1343-1366` |

---

## 4. Test / CI gaps

| Sev | ID | Finding | Evidence |
|-----|-----|---------|----------|
| HIGH | T-01 | `make verify` requires `golangci-lint` — not installed on typical dev machines | `Makefile:35`; fails before `go test ./...` |
| MED | T-02 | AGENTS.md says exclude `internal/improve/` from routine tests; CI runs full `./...` with `-race` | `AGENTS.md:11-12` vs `.github/workflows/ci.yml:46` |
| MED | T-03 | E2E tests use mock GitHub ops — live tmux/LLM paths not CI-gated | `test/e2e_test.go:33` mock `MergePR` |
| MED | T-04 | (FIXED) Wiring tests no longer intentionally document stubs for autoresearch (evolve/MergePR); replaced with real wired behavior tests + doc gate | TestWiring_AutoresearchEvolveCmd_Wired + TestAudit_DocsNoStaleAutoresearchStubRefs |
| LOW | T-05 | 2 structural audit tests fail encoding open doc-drift findings | `test/audit_structural_test.go` — `TestAudit_AGENTSmdPreflightCountDrift`, `TestAudit_HealthEndpointShipped` |

---

## 5. Documentation drift

| Sev | ID | Contradiction | Sources |
|-----|-----|---------------|---------|
| HIGH | D-01 | (FIXED) Preflight check count drift resolved: now 18/11 with disk+tmux_server wired. | (historical) |
| HIGH | D-02 | `architecture-overview.md` observability table: health endpoint **Missing** | Line ~1282; shipped at `internal/web/server.go:88` `/health` |
| HIGH | D-03 | Same doc: external notifications **Missing** | Line ~1284; `internal/notify/` + `resume.go:410-415` Slack wiring exists |
| HIGH | D-04 | Same doc: SLA framework listed as **Phase 2+** | Line ~1245; implemented in `internal/engine/monitor_sla.go` with `sla.auto_escalate` |
| MED | D-05 | dashstart spec says `/healthz`; probe uses `/health` | `docs/superpowers/specs/2026-06-16-always-on-status-design.md:56` vs `internal/dashstart/probe.go:50` |
| MED | D-06 | AGENTS.md missing CLI commands present in README + `root.go` | figma, security, autoresearch, logs, watch, db, retry |
| MED | D-07 | Dual agent docs: **CLAUDE.md** enforced by `doc_coverage_test.go`; **AGENTS.md** stale | `internal/engine/doc_coverage_test.go` reads CLAUDE.md only |
| MED | D-08 | Tutorial uses `/healthz` endpoint path; web dashboard serves `/health` | `docs/tutorial.md:32` vs `internal/web/server.go:88` |
| LOW | D-09 | AGENTS.md "Pending Work" dated 2026-04-12 | `AGENTS.md:192-203` |
| LOW | D-10 | README references CLAUDE.md for self-improvement honesty note; AGENTS.md diverges | `README.md:214` |

### Dispatch vs display preflight counts (code truth)

| Function | Count | When used |
|----------|-------|-----------|
| `DispatchChecks()` | 11 | Critical + tmux_server + disk |
| `DispatchChecksWithConfig(cfg)` | 13 | +2 devdb |
| `AllChecks()` | 18 | `vxd preflight` display |
| `AllChecksWithConfig(cfg)` | 20 | Full with devdb |

---

## 6. Placeholders / experimental / deferred (INTENTIONAL)

| Sev | ID | Item | Status | Evidence |
|-----|-----|------|--------|----------|
| HIGH | P-01 | Autoresearch fixed baseline (re-measure on main HEAD) | v2 deferred | `docs/knowledge/autoresearch/v1-placeholders-and-v2-levers.md` |
| HIGH | P-02 | (FIXED) Autoresearch evolve CLI integration | Wired (calls ProgramMDEvolver with LLM + bank + gate + workspace writer; PR-gated) | internal/cli/autoresearch.go (newAutoresearchEvolveCmd + Evolve) + wiring tests |
| MED | P-03 | (FIXED) Autoresearch MergePR for auto gate | Implemented + tested via DefaultGateOps; winning remains common default | (post fix) |
| MED | P-04 | Self-improvement engine | Experimental | `README.md:214` — no actionable improvements to date |
| MED | P-05 | NXD sync: Docker/SSH runners | Pending | `AGENTS.md:203` |
| LOW | P-06 | Phase 2 Vault secrets (env-only today) | Roadmap | `README.md:349`, `architecture-overview.md:1545+` |
| LOW | P-07 | v2 autoresearch levers (distributed coordinators, multi-metric Pareto) | Spec only | `v1-placeholders-and-v2-levers.md:33-38` |
| LOW | P-08 | Ghost devdb template dedicated preflight | Out of scope SP2 | `preflight/checks.go:448-453` |

---

## 7. Security / ops review

| Sev | ID | Finding | Evidence |
|-----|-----|---------|----------|
| MED | S-01 | Security scanners optional — gate degrades with reduced coverage | Preflight WARNING in smoke run; `security_gate.go:86` |
| MED | S-02 | `ANTHROPIC_API_KEY` vs Max subscription conflict warned but not blocked | `preflight/checks.go:94-105` |
| MED | S-03 | `/health` unauthenticated by design — minimal JSON only | `internal/web/auth.go:29`, `health_test.go` bans telemetry leak |
| LOW | S-04 | Prompt-injection filtering in improve/research paths | `improve/research_test.go:TestResearcher_FiltersPromptInjectionInContent` |
| LOW | S-05 | Shell-command validation for QA/build via runtime registry | `runtime/registry.go`, wiring tests |

---

## 8. Structural audit tests (`test/audit_structural_test.go`)

| Test | Expected result | Encodes |
|------|-----------------|---------|
| `TestAudit_PreflightCheckCounts` | PASS | AllChecks=18, DispatchChecks=11 (post disk+tmux wiring) |
| `TestAudit_DocCoverageTargetsCLAUDEmd` | PASS | CLAUDE.md exists; doc gate wired |
| `TestAudit_VXDImprovePresentAndCIBuildsIt` | PASS | cmd/vxd-improve present; CI builds it |
| `TestAudit_AGENTSmdPreflightCountDrift` | **FAIL** | AGENTS.md "12" vs code 16 |
| `TestAudit_HealthEndpointShipped` | **FAIL** | architecture-overview stale "Missing" |
| `TestAudit_NotifyOnCompleteUnwired` | PASS | Config knob has no reader |
| `TestAudit_AutoresearchBaselineStub` | PASS | v1 zero baseline |
| `TestAudit_AutoresearchMergePR` | PASS | MergePR real impl present and asserted (no stub string) |
| `TestAudit_EventProjectorDefaultSwallows` | PASS | Default swallow documented |

---

## 9. Key files for follow-up review

Give the next LLM these paths as starting points:

```
# Orchestration core
internal/cli/req.go
internal/cli/resume.go
internal/engine/monitor.go
internal/engine/monitor_dispatch.go
internal/engine/executor.go
internal/engine/dispatcher.go
internal/engine/planner.go
internal/engine/wiring_test.go

# State / events
internal/state/sqlite.go
internal/state/events.go
internal/state/filestore.go

# Stubs / experimental
internal/cli/autoresearch.go
internal/autoresearch/gate.go
internal/autoresearch/coordinator.go
internal/improve/

# Config / docs
internal/config/config.go
CLAUDE.md
AGENTS.md
README.md
docs/architecture-overview.md

# Security / preflight
internal/preflight/checks.go
internal/engine/security_gate.go
internal/security/scanners.go

# CI
Makefile
.github/workflows/ci.yml
test/audit_structural_test.go
test/e2e_test.go
```

---

## 10. Suggested areas for the next LLM to investigate

These were **not fully exercised** in this audit (static analysis + unit tests only):

1. **Live pipeline behavior** — tmux session lifecycle, agent completion detection false positives/negatives, subscription auth conflicts under real Claude CLI runs.
2. **Cross-story merge races** — file ownership enforcement when planner assigns overlapping `owned_files` despite validation.
3. **Concurrent `vxd resume`** — lock file behavior under crash, stale PID, force-acquire paths.
4. **Event log replay** — SQLite rebuild from `events.jsonl` after partial writes or corruption.
5. **Completion gate** — `CompletionGate` integration-fix story generation when mainline red after all stories merge.
6. **DevDB lifecycle** — orphan recovery, SLA-breach DB release, Docker/Ghost provider edge cases.
7. **improve package** — flaky/network tests, whether daily pipeline produces actionable PRs.
8. **Config validation completeness** — fields accepted by YAML but ignored at runtime (beyond `notify_on_complete`).
9. **Web dashboard auth** — token bootstrap, loopback binding, WebSocket command injection surface.
10. **NXD parity** — drift between VXD and nexus-dispatch mirror per `AGENTS.md` and `NXD_CROSSPORT_REQUIREMENTS.md`.

### Prompt template for the next LLM

```
You are continuing an audit of vortex-dispatch (VXD), an open-source AI orchestration CLI.
Read docs/audit-findings-2026-07-05.md first. Verify each finding against the codebase.
Find NEW shortcomings not listed — especially:
- config fields defined but never read
- CLI commands registered but not wired into the pipeline
- event types emitted but not projected
- tests that pass without exercising real code paths ("test theater")
- live-run failure modes (tmux, LLM CLIs, git merge conflicts)

For each new finding: severity (critical/high/medium/low), file:line, evidence, and whether intentional deferral.
Do not fix anything — findings only.
```

---

## 11. Priority ranking (findings only)

1. Align AGENTS.md with CLAUDE.md (preflight count, CLI command table)
2. Refresh `docs/architecture-overview.md` observability/SLA/health tables
3. Wire `notify_on_complete` or remove from config schema
4. Land autoresearch v2 baseline + evolve CLI (`ProgramMDEvolver`)
5. Add disk-space preflight before event-log append
6. Document/install `golangci-lint` for local `make verify`
7. Harden security gate when scanners missing (fail vs degrade — product decision)
8. Add event schema versioning strategy

---

*Generated by static audit on 2026-07-05. Re-verify against current HEAD before acting on findings.*
---

## 12. Resolution log (2026-07-05, same day)

Every finding was re-verified against HEAD and either **FIXED** (TDD: test first,
then implementation, then docs) or **SUBSTANTIATED** (the existing behavior is a
deliberate design choice; the reasoning is recorded here and, where useful, in
the code). The structural tests in `test/audit_structural_test.go` were flipped
from "documents the gap" to "pins the fix" as gaps closed.

### Fixed

| ID | Resolution |
|----|------------|
| E-01 / T-01 | `make verify` now runs build → vet → tests → doc-coverage → **lint last**, and `make lint` guards `golangci-lint` presence with install hints. A dev box without the linter gets full build+test signal before the gate fails actionably. Lint remains required (CI blocks on it). |
| E-02 / P-01 | `baselineFromConfig` returns a fixed **neutral 0.5** instead of 0, removing the all-positive skew in early-run `signedDelta`. Real main-HEAD re-measurement remains the scheduled v2 lever (see `docs/knowledge/autoresearch/v1-placeholders-and-v2-levers.md`). Pinned by `TestAudit_AutoresearchBaseline`. |
| E-05 | Corrupt `owned_files` JSON now sets `Story.OwnedFilesCorrupt` (transient, never persisted) in both `GetStory` and `ListStories`; `rebuildDAG` maps a corrupt story to `WaveHint=sequential` so a story with UNKNOWN ownership runs alone instead of in parallel where the overlap filter cannot see its files. **Bonus fix found while closing this:** `rebuildDAG` was dropping `OwnedFiles`/`WaveHint` entirely, silently disabling overlap filtering and sequential serialization on EVERY resumed requirement — both now carried through. Tests: `sqlite_ownedfiles_test.go`, `rebuild_dag_ownership_test.go`. |
| W-01 / P-02 | `vxd autoresearch evolve` is wired end-to-end to `ProgramMDEvolver` (config → LLM client → event store → HypothesisBank → Evolve → PR URL; still always human-gated). `TestWiring_AutoresearchEvolveCmd_IsStub` became `TestWiring_AutoresearchEvolveCmd_Wired`; `TestNewAutoresearchEvolveCmd_RequiresConfig` pins the early-error path. |
| W-02 | `notify_on_complete` is read: `emitRequirementOutcome` now sends a webhook notification for `REQ_COMPLETED` (info) / `REQ_BLOCKED` (error, with the resume hint), synchronously with a 10 s bound because it is the terminal event of the run. Gated by the new `notify.FilteredNotifier` allowlist. `TestAudit_NotifyOnCompleteUnwired` became `TestAudit_NotifyOnCompleteWired`. |
| W-03 | Notifier gating decoupled: a configured webhook enables the notifier; `PIPELINE_STALLED` is ALWAYS sent (human-intervention signal, no longer held hostage by the SLA flag); `notify_on_sla` gates `STORY_SLA_BREACHED`; `notify_on_complete` gates terminal outcomes. Pure helper `notificationAllowlist` + `notify.FilteredNotifier` (drops untyped messages, so the SLA message now carries its EventType). Tests: `filter_test.go`, `monitor_notify_test.go`, `notify_wiring_test.go`. |
| W-05 / S-01 (opt-in) | New `security.require_scanners` (default false): when true, a story whose applicable scanners were skipped (binary missing) or failed is BLOCKED (pause for a human) instead of passing with reduced coverage. Default stays graceful-degrade — see Substantiated below for why. Wired in resume.go (`TestResume_WiresSecurityGate` extended); tests in `security_gate_test.go`. |
| W-07 | Both theater wiring tests now assert reality: `TestWiring_MetricsCommand_Registered` scans root.go for the actual registration; `TestWiring_SecurityValidation_RejectsShellInjection` calls the real `runtime.ValidateModelName` against the injection fixtures. |
| O-01 | New `disk_space` preflight check (WARNING): statfs on the state-dir filesystem ($HOME), fails below 1 GiB free, naming the event-log/SQLite consequence. Platform-split `disk_unix.go` / `disk_windows.go`. In both DispatchChecks and AllChecks. |
| O-06 | New `tmux_server` preflight check (WARNING): probes REAL session creation (`new-session -d … true` + cleanup), catching stale sockets / unwritable TMUX_TMPDIR that the binary-presence check cannot. Skips (passes) when the binary is absent — `CheckTmux` (CRITICAL) owns that failure. Counts moved: **AllChecks 16→18, DispatchChecks 9→11** — README/CLAUDE.md/AGENTS.md/workflows.md updated; `TestAudit_PreflightCheckCounts` re-pinned. |
| D-01 | Counts aligned everywhere at the new truth (18/11): AGENTS.md (was 12), README (was 15 and 16), CLAUDE.md, docs/architecture.md, docs/workflows.md. |
| D-02 / D-03 | architecture-overview observability table updated: health endpoint **Exists** (`/health`, internal/web/server.go), external notifications **Exists** (Slack via FilteredNotifier); priority order re-drawn. |
| D-04 | architecture-overview §13.4 retitled from "(Phase 2+)": per-complexity deadlines, breach events, auto-escalation, and Slack alerting are SHIPPED (`monitor_sla.go`); the roadmap now separates DONE from the genuinely unshipped business metrics. |
| D-05 | dashstart spec `/healthz` → `/health` (all 8 references) to match `internal/dashstart/probe.go` and the served route. |
| D-06 / D-07 / D-09 / D-10 / T-02 | AGENTS.md overhauled: doc-precedence banner (CLAUDE.md is the test-enforced canonical doc; AGENTS.md is the mirror for Codex/Gemini CLIs), missing commands added (watch, retry, logs, security, figma, autoresearch, db, template), enforcement section corrected to name CLAUDE.md, test command no longer excludes `internal/improve` (CI runs the full module with `-race`), config example's 404 `gemma-4-27b-it` junior replaced with the real default, `internal/improve` row carries the experimental honesty note, Pending Work refreshed to 2026-07-05 and defers to CLAUDE.md. |

### Substantiated (deliberate design, no change)

| ID | Reasoning |
|----|-----------|
| E-03 / P-03 | (RESOLVED) MergePR now parses URL and calls real git.MergePR for auto gate. DefaultGateOps path tested with correct success outcomes. |
| E-04 | The projector `default` case logs WARNING and returns nil **on purpose**. Every DECLARED event type is forced into the switch by `TestProject_AllDeclaredEventsHandled` (exhaustiveness guard), so the default is only reachable for events written by a NEWER binary and replayed by an older one. Failing hard there would brick downgrade/replay of an append-only log; the forward-compatible choice is to skip-and-log. The CLAUDE.md rule ("new event types MUST be handled + wiring test") is the enforcement surface. |
| W-04 | CodeGraph no-op degradation is by design: blast-radius analysis is an enhancement, not a gate; requiring a Python tool (`uv tool install`) to dispatch would break fresh installs. The wire is guarded (`SetCodeGraph` only when `Available()`), and the loss is logged. |
| W-05 default | Graceful degradation stays the DEFAULT because the security gate's deterministic layer is one of three surfaces (planner standards, gate, standalone scan) and a missing host binary is an environment fault, not a code fault — hard-failing every merge on a semgrep uninstall would halt the factory for a non-code reason. The loss is never silent: preflight `security_scanners` WARNING + per-story log. Operators who want coverage as a hard gate now have `require_scanners: true`. |
| W-06 / P-08 | Ghost devdb template preflight: template existence is verified at fork time by the provider (`ErrTemplateMiss`), so the preflight INFO is a scope decision (SP2), not a blind spot — a missing template fails the story fast with a precise error rather than failing preflight with a network call to a cloud API on every dispatch. |
| W-08 | Dead-code removal already done; nothing to do. |
| O-02 | Event schema versioning: the additive-only evolution rules in architecture-overview §14.3 (never remove/rename/retype fields; new fields must zero-value) ARE the versioning strategy — enforced socially + by lenient decoding. A `schema_version` field adds value only when a migration runner exists to act on it (O-07); adding the field without the runner is dead weight in every event. Deferred together with O-07. |
| O-03 / O-08 | Network-aware locking: the lock file (PID liveness) is scoped to single-host operation, which is the supported deployment. The failure-modes table now says explicitly: do NOT put `state_dir` on NFS/SMB. Distributed coordination is a Phase-2 lever (also listed under autoresearch v2 "distributed coordinators"). |
| O-04 / O-05 | Native-Windows tmux and Docker named-pipe constraints are platform facts, documented in README Platform Support with the WSL2 path; preflight names them at dispatch time. |
| O-07 | SQLite migration gate: the projection is DISPOSABLE — `events.jsonl` is the source of truth and the documented recovery is delete-DB + replay. A migration runner is roadmap (failure-modes table), not a correctness hole. |
| O-09 / O-10 | Event-log compaction and backup/restore are documented Phase-2 roadmap items (`vxd backup` exists for archival today; RPO table in §14.2). Unbounded JSONL growth is measured in MB/requirement — not operationally urgent. |
| D-08 | FALSE POSITIVE: `docs/tutorial.md`'s `/healthz` is the endpoint of the DEMO APP the tutorial builds ("Add a health check endpoint at GET /healthz…"), not vxd's dashboard endpoint. No contradiction with `internal/web/server.go`. Same for monitoring.md's story title. |
| T-03 | E2E tests mock GitHub ops deliberately: CI has no `gh` auth or live LLM; the live path is exercised by the factory itself (dogfooding) and the mock boundary is the same `GitHubOps` interface production uses. |
| T-04 | Resolved by the W-01 fix — the stub-documenting test was replaced with a wired-behavior test, exactly as its comment instructed. |
| S-02 | The ANTHROPIC_API_KEY conflict stays WARNING, not blocking: the configuration is legitimate for API-credit users; VXD additionally strips the key from tmux/agent env, so the warning is advisory for the operator's own shell. |
| S-03 | `/health` unauthenticated is required for daemon liveness probes (dashstart, systemd/Docker); `health_test.go` bans telemetry leakage from the payload. |

### Verification (post-resolution)

- `go build ./...` + `go build -o ~/.local/bin/vxd ./cmd/vxd` — clean
- `go test ./... -count=1` — all packages green, including `test/` (all 9 structural audit tests now PASS)
- `vxd preflight` — 18 checks, both new checks green live
- Doc-coverage gates green (`TestDocCoverage_*`)
