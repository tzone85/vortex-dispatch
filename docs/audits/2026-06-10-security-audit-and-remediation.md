# Security & Quality Audit + Remediation — 2026-06-10

**Branch:** `claude/amazing-ride-cjzwcj`
**Scope:** full repository (~25 packages, 467 Go files)
**Method:** six parallel domain deep-dives (engine, state/config, llm/improve/autoresearch,
devdb/web/dashboard, git/tmux/cli/preflight) plus hands-on verification of the
security primitives, CI, dependencies, and test suite. Every high-severity finding
was reproduced against source before remediation.

This document records what was found, what was fixed (with the commit that fixed it),
and what remains as tracked follow-up.

---

## Through-line

The most consequential weakness was a single pattern: **untrusted LLM/scraped output
crossing trust boundaries without validation**, which the project's own `CLAUDE.md`
explicitly forbids. It manifested as path traversal (story IDs), prompt injection
(agent logs, scraped postings), and unbounded spend (autoresearch loop). The remediation
centralizes input validation and screening at each boundary.

---

## Findings & remediation

### Critical

| ID | Finding | Status | Fix |
|----|---------|--------|-----|
| C1 | Dashboard `/ws` dispatched mutating commands (kill_agent, edit_story, pause/resume, escalate, retry, reassign) with **no authentication**; the Origin check only constrains browsers that send an Origin header, so any non-browser local process could drive the orchestrator. | **Fixed** | Per-session token gate on `/ws` (constant-time), token handshake on the index page that never leaks the secret to an unauthenticated caller, retained Origin check for CSWSH. `feat(web): authenticate the dashboard WebSocket command channel` |
| C2 | Scraped web content flowed into prompts that drive agents/drafts; only a shallow substring blocklist screened it. | **Mitigated** | Input screening (`sanitize.DetectPromptInjection`) applied at the proposal-drafting and re-planner boundaries; auto-implementer pipeline remains dormant. `fix(security): sanitize agent log…`, `ci: …screen proposal input` |
| C3 | Autoresearch coordinator looped experiment waves forever; `--budget` is only a per-experiment timeout, so `--continuous` was an unbounded API-credit burn. | **Fixed** | Consecutive-failure circuit breaker (default 10) + `max_experiments` hard cap (`--max-experiments`). `feat(autoresearch): bound experiment spend…` |

### High

| ID | Finding | Status | Fix |
|----|---------|--------|-----|
| H1 | LLM-generated `story.ID` was never charset-validated before becoming git refs and filesystem paths → path traversal + argument injection. | **Fixed** | `state.ValidateStoryID` wired into `planner.Plan` and `escalation.ValidateSplitWithEdges`. `fix(security): validate LLM-generated story IDs…` |
| H2 | Docker dev-DB Postgres published on `0.0.0.0` (all interfaces). | **Fixed** | Binds `127.0.0.1` by default; `0.0.0.0` only for explicit non-loopback host (Colima/Lima). `fix: bind dev DB to loopback…` |
| H3 | `vxd db sql` runs as the Postgres superuser; per-DB DSNs embed shared admin credentials. | **Partially fixed** | Name validation at all provider boundaries + correct identifier quoting added. Per-DB least-privilege role is tracked follow-up (architectural). |
| H4 | Append/Project non-atomic with **no rebuild path** and swallowed Project errors → permanent log↔projection divergence on crash. | **Fixed** | `SQLiteStore.Rebuild` (transactional replay) + `vxd rebuild` command; idempotent `STORY_CREATED`; `_busy_timeout`. `fix(state): add projection rebuild path…` |
| H5 | `gitPullWithStash` ran `git status` against the daemon CWD, not the target repo → could stash/pop the wrong tree. | **Fixed** | Set `.Dir = repoDir`. `fix: …fix stash dir` |
| H6 | Post-merge integration-fixer derived its context from the soon-cancelled `pipelineCtx`, so the LLM call was killed before it ran. | **Fixed** | Detached `context.Background()` with its own timeout + regression test. `fix: …detach integration-fixer ctx` |
| H7 | (see C3) | **Fixed** | — |

### Medium (selected)

| ID | Finding | Status |
|----|---------|--------|
| M2 | Agent log fed into re-planner prompt unsanitized | **Fixed** (`DetectPromptInjection` + redaction) |
| M4 | `Merge` reported success when auto-merge configured but PR number 0 | **Fixed** (hard error + test) |
| M5 | `projectStoryCreated` used bare INSERT (non-idempotent) | **Fixed** (`INSERT OR IGNORE`) |
| M6 | SQLite opened with no busy timeout | **Fixed** (`_busy_timeout=5000`) |
| M7 | TOCTOU in `guardedStartStory` (SELECT then UPDATE) | **Fixed** (single conditional UPDATE) |
| M9 | Scraped posting text into proposal prompt unscreened | **Fixed** (input screening) |
| M10 | Ghost client interpolated path segments raw | **Fixed** (`url.PathEscape` helper) |
| M11 | `db delete`/`db schema` skipped name validation | **Fixed** (`devdb.IsValid` at boundary) |
| M13 | `state_dir` unvalidated (joined into paths/shell) | **Fixed** (metachar + traversal validation) |

### Low / hygiene

- LLM clients (Anthropic/Google/OpenAI) and the email sender had **no HTTP timeout** and
  used unbounded `io.ReadAll` → **Fixed** (timeout + `LimitReader`).
- `conflict_resolver` dead branch and a pointless `Sprintf("--pretty=%%s")` → **Fixed**.

### CI / test integrity

- CI **excluded `internal/cli` and `internal/improve`** from the test run, so the
  "all packages pass" claim was unenforced; both actually failed (non-hermetic git/gh
  dependencies). → **Fixed**: root causes addressed (hermetic test helpers,
  `commit.gpgsign=false`, `approve` validates before requiring `gh`), packages
  un-excluded, full `go test ./...` runs on every push.
- Workflow-wide `contents: write` → narrowed to `contents: read`; only `release` elevates.
- Added an advisory `govulncheck` job.

### Pre-existing bugs fixed along the way

- `detectExistingCodebase` returned `false` on any git error, short-circuiting the
  source-file heuristic (a repo full of source with unreadable history was misclassified
  as greenfield). → **Fixed**; also repaired the archaeology wiring test.
- Two privilege-sensitive tests asserted errors that don't occur as root → made hermetic.

---

## Remaining / tracked follow-up

These are intentionally deferred (architectural scope or debatable behavior change):

1. **H3 full least-privilege**: create a scoped per-DB Postgres role instead of sharing
   the cluster admin DSN. Requires changing the connection flow and template import.
2. **M8**: model-suggested source URLs in `improve/discovery` (SSRF surface) — currently
   routed through Firecrawl + human approval; lower risk.
3. **gc/archive `--confirm`**: deferred — `archive` operates on already-completed/merged
   work and adding a mandatory prompt would break non-interactive automation.
4. **`nhooyr.io/websocket` → `coder/websocket`** migration (deprecation; large, separate).
5. **`monitor.go` refactor** (1800+ lines) — tracked tech debt.
6. **`dolt` backend** is accepted by config validation but unimplemented — consider
   removing from `validBackends` to fail loud.

---

## Verification

After remediation: `go build ./...` clean, `go vet ./...` clean, `go test ./...` green
across all packages (0 failures), with new tests covering every fix above.
