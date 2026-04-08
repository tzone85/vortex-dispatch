# VXD Self-Improvement Engine Design

**Date:** 2026-04-08
**Status:** Approved
**Approach:** Monolithic Go Binary + Local Cron (launchd)

## Overview

An autonomous daily pipeline that researches improvements for VXD across the full ecosystem (LLM providers, Go ecosystem, competitor tools, security advisories, historical best practices), implements low-risk improvements with strict quality gates, opens PRs, and sends a rich HTML email report with graphs, reasoning, and audit trail.

**Cost model:** $0 beyond existing Claude Max subscription. Research analysis uses Google AI free tier (Gemma 4). Implementation uses `claude -p` CLI (Max subscription). Email via Resend free tier. Charts via QuickChart.io (free, URL-based).

**Legal compliance:** No copying of proprietary code. Only permissively-licensed dependencies (Apache 2.0, MIT, BSD, ISC, MPL-2.0). All external content sanitized. Ideas and patterns are not copyrightable — only specific implementations are.

## Architecture

```
launchd (6am daily, macOS)
  → vxd-improve binary
    → Phase 1: Research (Firecrawl API → curated sources)
    → Phase 2: Analysis (Gemma 4 triage → Claude -p deep analysis)
    → Phase 3: Implementation (branch → claude -p → quality gates → PR)
    → Phase 4: Email (HTML + QuickChart graphs → Resend API)
    → Phase 5: Audit (changelog.jsonl + run summary → git commit)
```

## Phase 1: Research Engine

Scrapes curated sources via Firecrawl API, organized into categories. Bidirectional: current (last 24h) + historical (rotating deep-dive).

### Source Categories

| Category | Sources | Direction |
|----------|---------|-----------|
| LLM Providers | Google AI blog, Anthropic changelog, OpenAI blog, Ollama releases | Current |
| Go Ecosystem | Go blog, Go release notes, golang/go issues, trending Go repos | Current |
| Competitor Tools | Cursor changelog, Devin blog, SWE-agent/OpenHands GitHub releases, Codex CLI | Current |
| Security | vuln.go.dev, GitHub Advisory DB, OWASP updates, prompt injection research | Current |
| Historical | Domain-specific deep searches via Firecrawl (rotating topic per day) | Historical |
| General SE | Hacker News top/best, r/golang, r/LocalLLaMA | Current |

### Historical Rotation

One deep-dive topic per day, cycling through:
- Event sourcing patterns
- tmux automation techniques
- CLI UX patterns
- Agent orchestration research papers
- Go performance optimization
- Distributed systems patterns
- LLM prompt engineering advances
- Code review automation
- Dependency management strategies
- Testing strategies for AI systems

Each topic searches going back months/years for established patterns VXD could adopt.

### Rate Budget

Firecrawl free tier: 500 credits/month (~16/day). Budget per run:
- 10 current source scrapes
- 2 historical deep searches
- 4 reserve
- Total: 16 credits/day — fits within free tier

### Implementation

`internal/improve/research.go`:
- `Source` struct: name, URL template, category, direction (current/historical)
- `Researcher` struct: Firecrawl API key, source registry
- `Research(ctx) ([]Finding, error)` — scrapes all sources, returns structured findings
- Each `Finding`: title, source URL, content (max 2000 chars), category, timestamp, direction

Firecrawl API call:
```
POST https://api.firecrawl.dev/v2/scrape
Authorization: Bearer {FIRECRAWL_API_KEY}
{"url": "...", "formats": ["markdown"]}
```

## Phase 2: Analysis + Scoring Pipeline

Two-stage analysis: lightweight triage via Gemma 4 (free), deep analysis via Claude (Max subscription).

### Stage 1: Gemma 4 Triage

Each finding is scored by the existing `GoogleAIClient` (from `internal/llm/google.go`):

| Metric | Range | Description |
|--------|-------|-------------|
| Relevance | 0-10 | How applicable to VXD specifically |
| Impact | 0-10 | How much would this improve VXD |
| Risk | 0-10 | How likely to break something (higher = riskier) |
| Effort | S/M/L | Estimated implementation complexity |
| Category | enum | security, performance, feature, dependency, docs, architecture |

**Filtering:** Findings with relevance < 5 are discarded. Remaining are ranked by `(impact * 2 + relevance) - risk`.

### Stage 2: Claude Deep Analysis

Top 5 findings from Stage 1 are sent to `claude -p` with VXD codebase context:

- Concrete implementation plan (files to change, how)
- Security implications assessment
- License compliance check
- Test strategy
- Go/no-go recommendation

### Guardrails

- External content sanitized before LLM input: strip HTML, limit 2000 chars per finding, reject content containing known prompt injection patterns ("ignore previous instructions", "system prompt override", etc.)
- Claude's go/no-go must explicitly confirm: no license violation, no security regression, tests exist
- Maximum 3 findings proceed to implementation per run

### Implementation

`internal/improve/analyzer.go`:
- `Analyzer` struct: GoogleAIClient (triage), claude CLI path (deep analysis)
- `Triage(ctx, findings) ([]ScoredFinding, error)` — Gemma 4 scoring
- `DeepAnalyze(ctx, scoredFindings) ([]AnalyzedFinding, error)` — Claude analysis
- `ScoredFinding`: Finding + scores + rank
- `AnalyzedFinding`: ScoredFinding + implementation plan + go/no-go + security review + license check

`internal/improve/sanitize.go`:
- `SanitizeContent(raw string) string` — HTML stripping, length limiting
- `DetectPromptInjection(content string) bool` — pattern-based detection
- `ScanForSecrets(diff string) bool` — API key / token / password patterns

## Phase 3: Implementation Pipeline

Approved findings are implemented one at a time in isolated branches.

### Per-Improvement Flow

```
1. git checkout -b vxd-improve/YYYY-MM-DD-<slug> main
2. Write prompt file with finding + analysis + relevant source files
3. exec.Command("claude", "-p", promptString, "--output-format", "json", "--max-turns", "1") — Go exec, not shell, avoids quoting issues
4. Parse response, write files
5. Quality gates (sequential, any failure aborts):
   a. go build ./...
   b. go vet ./...
   c. go test ./... -race
   d. License check: no new GPL/AGPL/SSPL dependencies
   e. Secret scan: no API keys/tokens in diff
   f. Diff size: max 500 lines changed
   g. File scope: max 10 files changed
6. git add + commit
7. git push -u origin <branch>
8. gh pr create --title "..." --body "..."
9. Record result in audit log
```

### Safety Guardrails

| Guard | Rule | On Failure |
|-------|------|------------|
| Test suite | All existing tests must pass (`go test ./... -race`) | Abort, log, skip to next |
| Diff size | Max 500 lines changed per PR | Downgrade to "Proposed" in email |
| File scope | Max 10 files changed per PR | Downgrade to "Proposed" |
| Secret scan | No API keys, tokens, passwords in diff | Abort immediately |
| License check | Only Apache 2.0, MIT, BSD, ISC, MPL-2.0 new deps | Abort, flag in email |
| Build check | `go build ./...` clean | Abort, log |
| Race detector | `go test -race` clean | Abort, log |
| Max PRs per run | 3 | Stop, remaining go to "Proposed" |
| Prompt validation | Claude output must be valid Go, must compile | Abort that finding |
| No force push | Never `git push --force` | Hard-coded, no override |
| No main commits | Implementation never commits directly to main | Only branches + PRs |

### Implementation

`internal/improve/implementer.go`:
- `Implementer` struct: repo path, claude CLI path, github token
- `Implement(ctx, finding AnalyzedFinding) (ImplementResult, error)`
- `ImplementResult`: branch name, PR URL, files changed, lines changed, tests passed, disposition
- Quality gate functions: `checkBuild()`, `checkVet()`, `checkTests()`, `checkLicense()`, `checkSecrets()`, `checkDiffSize()`

## Phase 4: Email Report

HTML email sent via Resend API with QuickChart.io graphs.

### Email Metadata

```
From: VXD Self-Improvement <onboarding@resend.dev>
To: vortex.dispatch01@gmail.com
Subject: VXD Daily Improvement Report — YYYY-MM-DD (N PRs, N Alerts)
```

### Section Layout

Navigation bar at top with anchor links. Each section has a colored header. Empty sections omitted.

1. **Executive Summary** (always present) — 3-4 sentences, total PRs, risk score, green/yellow/red status
2. **PRs Created Today** (if any) — table: title, link, category, tests status, lines changed
3. **Current Trends** — LLM provider updates, Go ecosystem changes, general SE trends
4. **Historical Discoveries** — today's deep-dive topic and findings
5. **Competitor Watch** — what Cursor/Devin/SWE-agent/OpenHands/Codex shipped
6. **Security Alerts** — CVEs, vulnerabilities, prompt injection patterns
7. **Metrics Dashboard** — QuickChart graphs (PRs over time, category breakdown, test pass rate, codebase health)
8. **Proposed (Not Implemented)** — ideas too risky/large for auto-implementation, awaiting decision
9. **Audit Trail** — link to changelog.jsonl on GitHub

### Graphs (QuickChart.io)

Embedded as `<img src="https://quickchart.io/chart?c={config}">`. No JavaScript required.

1. **PRs over time** (last 30 days) — bar chart, color-coded by category
2. **Category breakdown** (this run) — pie chart
3. **Test pass rate** (last 30 days) — line chart
4. **Codebase health** — gauge chart (coverage %, lint issues, dep freshness)

Graph data sourced from `changelog.jsonl` history — charts improve over time.

### Resend API

```
POST https://api.resend.com/emails
Authorization: Bearer {RESEND_API_KEY}
{"from": "onboarding@resend.dev", "to": ["vortex.dispatch01@gmail.com"], "subject": "...", "html": "..."}
```

Single HTTP POST, no SDK. Matches VXD's direct HTTP convention.

### Implementation

`internal/improve/email.go`:
- `EmailBuilder` struct: audit log reader, chart builder
- `Build(ctx, runResult RunResult) (string, error)` — returns HTML string
- `Send(ctx, html, subject string) error` — Resend API call
- Section builders: `buildSummary()`, `buildPRTable()`, `buildTrends()`, etc.
- `buildChartURL(chartType, data)` — constructs QuickChart.io URLs

## Phase 5: Audit Trail + Persistence

### Audit Log

`docs/self-improvement/changelog.jsonl` — one JSON line per finding per run:

```json
{
  "run_id": "2026-04-08T06:00:00Z",
  "finding_id": "f-2026-04-08-001",
  "source": "https://go.dev/blog/example",
  "category": "go_ecosystem",
  "title": "Description of finding",
  "relevance": 8,
  "impact": 7,
  "risk": 3,
  "disposition": "implemented",
  "pr_url": "https://github.com/tzone85/vortex-dispatch/pull/25",
  "tests_passed": true,
  "files_changed": 4,
  "lines_changed": 87,
  "reasoning": "Why this was implemented...",
  "security_review": "No new deps, no external input changes",
  "license_check": "pass"
}
```

Disposition values: `implemented`, `proposed`, `rejected`, `aborted`

### Run Summary

`docs/self-improvement/runs/YYYY-MM-DD.json` — per-run metadata:

```json
{
  "run_id": "2026-04-08T06:00:00Z",
  "started_at": "2026-04-08T06:00:00Z",
  "completed_at": "2026-04-08T06:12:34Z",
  "sources_scraped": 12,
  "findings_total": 27,
  "findings_relevant": 8,
  "findings_analyzed": 5,
  "prs_created": 3,
  "prs_proposed": 2,
  "errors": [],
  "email_sent": true
}
```

### Git Commit Strategy

After all PRs are created, one commit to `main` updates audit files:
```
chore(self-improve): daily run YYYY-MM-DD — N PRs, M findings analyzed
```

### Idempotency

The binary checks for `docs/self-improvement/runs/YYYY-MM-DD.json` on startup. If it exists and `email_sent: true`, the run is skipped. Prevents duplicate PRs/emails if launchd triggers twice.

## Scheduling (launchd)

`~/Library/LaunchAgents/com.vxd.self-improve.plist`

- Triggers daily at 6:00 AM local time
- If Mac was asleep at 6am, runs on next wake
- Stdout/stderr to `~/.vxd/self-improve/launchd.log`
- Sets `PATH` and environment so `claude`, `go`, `gh`, and API keys are available

Manual trigger: `vxd-improve` or `go run ./cmd/vxd-improve`
Dry-run mode: `vxd-improve --dry-run` — research + analysis + email, no branches/PRs

## Files Changed

| File | Action | Description |
|------|--------|-------------|
| `cmd/vxd-improve/main.go` | **New** | Entry point, orchestrates all 5 phases |
| `internal/improve/research.go` | **New** | Firecrawl API client, source registry, bidirectional scraping |
| `internal/improve/analyzer.go` | **New** | Gemma 4 triage + Claude deep analysis |
| `internal/improve/implementer.go` | **New** | Branch, claude -p, quality gates, PR creation |
| `internal/improve/email.go` | **New** | HTML builder, QuickChart graphs, Resend API |
| `internal/improve/audit.go` | **New** | JSONL log, run summary, idempotency check |
| `internal/improve/sanitize.go` | **New** | Input sanitization, prompt injection detection, secret scanning |
| `internal/improve/config.go` | **New** | Sources, thresholds, limits, email settings |
| `internal/improve/research_test.go` | **New** | Firecrawl mock, source parsing |
| `internal/improve/analyzer_test.go` | **New** | Scoring, filtering, sanitization |
| `internal/improve/implementer_test.go` | **New** | Quality gates, diff guards, license check |
| `internal/improve/email_test.go` | **New** | HTML generation, graph URLs, Resend mock |
| `internal/improve/audit_test.go` | **New** | JSONL append/read, idempotency |
| `internal/improve/sanitize_test.go` | **New** | Prompt injection patterns, secret detection |
| `.github/workflows/ci.yml` | **Edit** | Add vxd-improve to build check |
| `~/Library/LaunchAgents/com.vxd.self-improve.plist` | **New** | launchd schedule |
| `docs/self-improvement/changelog.jsonl` | **New** | Audit log (grows daily) |
| `docs/self-improvement/runs/` | **New** | Per-run summary directory |

## Environment Variables Required

| Variable | Purpose | Source |
|----------|---------|--------|
| `FIRECRAWL_API_KEY` | Web research scraping | ~/.zshrc |
| `RESEND_API_KEY` | Email sending | ~/.zshrc |
| `ANTHROPIC_API_KEY` | Not used (cost avoidance) — Claude Max via CLI | N/A |
| `GOOGLE_AI_API_KEY` | Gemma 4 triage analysis | ~/.zshrc |
| `GITHUB_TOKEN` or `gh auth` | PR creation | gh auth login |

## Constraints

- **Zero API cost** — Google AI free tier + Claude Max CLI + Resend free tier + QuickChart free
- **Legal compliance** — No proprietary code copying, permissive licenses only, ideas/patterns not implementations
- **Security-first** — Input sanitization, prompt injection detection, secret scanning, license auditing
- **Idempotent** — Safe to run multiple times per day
- **Max 3 PRs per run** — Prevents runaway automated changes
- **All changes must pass existing test suite** — No regressions ever
- **Never force push, never commit directly to main** — Branches + PRs only
