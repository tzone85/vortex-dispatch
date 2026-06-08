# VXD (Vortex Dispatch) — Full Architecture Overview

> **Version:** 1.0 · **Date:** 2026-04-15 · **Codebase:** ~64k lines Go across 18 packages  
> **Purpose:** Autonomous AI agent orchestration for software development

---

## Table of Contents

1. [System Overview](#1-system-overview)
2. [System Diagrams](#2-system-diagrams)
3. [Package Dependency Graph](#3-package-dependency-graph)
4. [Executor Design Rationale](#4-executor-design-rationale)
5. [Revenue Pipeline Design](#5-revenue-pipeline-design)
6. [Bayesian Feedback Mathematics](#6-bayesian-feedback-mathematics)
7. [3-Phase Deployment Strategy](#7-3-phase-deployment-strategy)
8. [Technical Decision Table](#8-technical-decision-table)
9. [Risk Assessment](#9-risk-assessment)
10. [Revenue Projections](#10-revenue-projections)
11. [Security & Data Privacy](#11-security--data-privacy)
12. [Competitive Positioning](#12-competitive-positioning)
13. [Agent Quality Metrics & SLA](#13-agent-quality-metrics--sla)
14. [Capacity, Disaster Recovery & Evolution](#14-capacity-disaster-recovery--evolution)

---

## 1. System Overview

VXD orchestrates AI coding agents (Claude Code, Codex, Gemini CLI) to autonomously implement software requirements. It decomposes requirements into stories, dispatches agents in parallel via tmux sessions, monitors progress, runs QA, and merges PRs — with a 5-tier escalation chain for failures.

### Core Capabilities

| Capability | Description |
|------------|-------------|
| **Requirement Decomposition** | Tech Lead LLM breaks requirements into Fibonacci-scored stories with dependency DAG |
| **Parallel Dispatch** | Wave-based agent deployment in isolated git worktrees |
| **5-Tier Escalation** | Junior → Senior → Manager diagnosis → Tech Lead re-plan → Human pause |
| **Event Sourcing** | Append-only JSONL event log with SQLite materialized views |
| **Self-Improvement** | Daily autonomous research, implementation, and PR creation |
| **Revenue Engine** | Opportunity scraping, Bayesian-scored proposals, revenue tracking |
| **Cost Estimation** | Fibonacci-to-hours mapping with client-facing quotes |
| **Crash Recovery** | PID-based lock files, checkpoint writes, 5 recovery scenarios |
| **Human Review Gates** | 3 modes: auto, plan_only, manual — configurable per run |

---

## 2. System Diagrams

### 2.1 High-Level Pipeline

```
┌──────────────────────────────────────────────────────────────────────────┐
│                          VXD PIPELINE                                    │
│                                                                          │
│  ┌──────────┐   ┌──────────┐   ┌──────────┐   ┌──────────┐             │
│  │  vxd req │──▶│ Planner  │──▶│Dispatcher│──▶│ Executor │             │
│  │          │   │(Tech Lead│   │  (Waves) │   │(Worktrees│             │
│  │Requirement│  │   LLM)   │   │          │   │ + Tmux)  │             │
│  └──────────┘   └────┬─────┘   └──────────┘   └────┬─────┘             │
│                      │                              │                    │
│                      │ Stories + DAG                 │ Agents running     │
│                      │                              ▼                    │
│                      │              ┌───────────────────────────┐        │
│                      │              │        Monitor            │        │
│                      │              │   (10s polling loop)      │        │
│                      │              │                           │        │
│                      │              │  ┌─────┐ ┌──┐ ┌─────┐   │        │
│                      │              │  │Review│▶│QA│▶│Merge│   │        │
│                      │              │  └─────┘ └──┘ └──┬──┘   │        │
│                      │              └──────────────────┬┘       │        │
│                      │                                │         │        │
│                 ┌────▼─────┐    ┌──────────┐   ┌─────▼──┐      │        │
│                 │Escalation│◀───│  Manager  │   │ Reaper │      │        │
│                 │ Machine  │    │(Diagnosis)│   │(Cleanup│      │        │
│                 └──────────┘    └──────────┘   └────────┘      │        │
│                                                                 │        │
│                 ┌───────────────────────────────────────┐        │        │
│                 │         Auto-Resume                   │────────┘        │
│                 │   (dispatch next wave if ready)       │                 │
│                 └───────────────────────────────────────┘                 │
└──────────────────────────────────────────────────────────────────────────┘
```

### 2.2 Event Sourcing Architecture

```
                Write Path                         Read Path
                ─────────                          ─────────

Engine Component                              CLI / Dashboard
      │                                             │
      ▼                                             ▼
  NewEvent()                                SQLiteStore.Query()
      │                                             ▲
      ▼                                             │
FileStore.Append()───fsync──▶ events.jsonl    SQLite (WAL)
      │                       (source of        7 tables
      │                        truth)              ▲
      ▼                                            │
SQLiteStore.Project() ─────────────────────────────┘
      │
      └── switch evt.Type {
            case REQ_SUBMITTED:    → INSERT requirements
            case STORY_CREATED:    → INSERT stories
            case STORY_STARTED:    → UPDATE stories SET status
            case STORY_MERGED:     → UPDATE stories SET status
            case STORY_ESCALATED:  → INSERT escalations
            case STORY_RESET:      → UPDATE stories SET status = "draft"
            ...45+ event types
          }
```

### 2.3 Adapter/Runner Execution Model

```
┌─────────────────────────────────────────────────────────────┐
│                    EXECUTOR                                   │
│                                                               │
│  SessionConfig                                                │
│  ├── WorkDir: ~/.vxd/worktrees/{storyID}/                    │
│  ├── Model: claude-sonnet-4-20250514                          │
│  ├── SystemPrompt: role-specific instructions                │
│  ├── Goal: story description + acceptance criteria           │
│  └── LogFile: ~/.vxd/logs/{storyID}.log                     │
│         │                                                     │
│         ▼                                                     │
│  ┌──────────────────────┐     ┌──────────────────────┐       │
│  │   Adapter (PURE)     │     │   Runner (EFFECTS)   │       │
│  │                      │     │                      │       │
│  │  CLIAdapter.Prepare()│────▶│  TmuxRunner.Run()    │       │
│  │                      │     │  DockerRunner.Run()   │       │
│  │  Input: SessionConfig│     │  SSHRunner.Run()      │       │
│  │  Output:             │     │                      │       │
│  │   PreparedExecution  │     │  1. Write setup files │       │
│  │   ├── Command string │     │  2. Create tmux sess  │       │
│  │   ├── Env vars       │     │  3. Propagate env     │       │
│  │   ├── SetupFiles{}   │     │  4. Execute command   │       │
│  │   └── WorkDir        │     │                      │       │
│  │                      │     │  No return value —    │       │
│  │  NO I/O              │     │  fire and forget      │       │
│  │  NO side effects     │     │                      │       │
│  │  100% testable       │     │                      │       │
│  └──────────────────────┘     └──────────────────────┘       │
└─────────────────────────────────────────────────────────────┘
```

### 2.4 Escalation Chain

```
Story Fails (Review/QA rejection)
         │
         ▼
┌────────────────────────────────────────────────────────────────┐
│ Tier 0: SAME-ROLE RETRY                                        │
│ ├── SmartRetry: categorize error (8 types)                     │
│ ├── Inject actual build/test/lint errors                       │
│ ├── Add fix suggestions per category                           │
│ └── Max retries: max_retries_before_escalation (default 2)     │
│                                                                 │
│ If retries exhausted ──▶                                       │
│                                                                 │
│ Tier 1: SENIOR DEVELOPER                                        │
│ ├── Route to RoleSenior (more capable model)                   │
│ ├── Same error context + attempt history                       │
│ └── Max retries: max_senior_retries (default 2)                │
│                                                                 │
│ If retries exhausted ──▶                                       │
│                                                                 │
│ Tier 2: MANAGER DIAGNOSIS                                       │
│ ├── Manager LLM analyzes full failure pattern                  │
│ ├── Reads: agent log, event history, worktree, git log         │
│ ├── Returns JSON: { diagnosis, category, action }              │
│ │   Actions: retry | rewrite | split | escalate_to_techlead    │
│ └── Max attempts: max_manager_attempts (default 1)             │
│                                                                 │
│ If action = escalate_to_techlead ──▶                           │
│                                                                 │
│ Tier 3: TECH LEAD RE-PLANNING                                   │
│ ├── Planner.RePlan() decomposes into smaller sub-stories       │
│ ├── Validates: max split depth ≤ 2, no file overlaps           │
│ ├── Emits: STORY_SPLIT + STORY_CREATED for each child          │
│ └── DAG mutated: parent → non-executable, children inherit deps│
│                                                                 │
│ If still failing ──▶                                           │
│                                                                 │
│ Tier 4: PAUSE (human intervention required)                     │
│ └── Story status = "paused", vxd resume --force to retry       │
└────────────────────────────────────────────────────────────────┘
```

### 2.5 Revenue Engine Pipeline

```
┌──────────────────────────────────────────────────────────────────────┐
│               VXD-IMPROVE DAEMON (Daily 6am via launchd)             │
│                                                                       │
│  Phase 1-3: SELF-IMPROVEMENT                                         │
│  ┌──────────┐   ┌──────────┐   ┌──────────────┐                     │
│  │ Research  │──▶│ Analysis │──▶│Implementation│                     │
│  │(Firecrawl)│   │ (Score)  │   │(Claude CLI)  │                     │
│  │12+ sources│   │Relevance │   │Branch+PR     │                     │
│  └──────────┘   │≥5 filter │   └──────────────┘                     │
│                  └──────────┘                                         │
│                                                                       │
│  Phase 6-8: REVENUE GENERATION                                        │
│  ┌──────────┐   ┌──────────┐   ┌──────────────┐                     │
│  │ Scrape   │──▶│  Score   │──▶│   Propose    │                     │
│  │Jobicy    │   │Relevance │   │Claude drafts │                     │
│  │Remotive  │   │×3 +      │   │professional  │                     │
│  │Upwork    │   │Budget×2 +│   │proposals     │                     │
│  │Freelancer│   │WinProb   │   │              │                     │
│  │PPH       │   │= Rank    │   │Min $50/hr    │                     │
│  └──────────┘   └────┬─────┘   └──────────────┘                     │
│                      │                                                │
│                      ▼                                                │
│              ┌───────────────┐                                        │
│              │   Bayesian    │                                        │
│              │  Adjustment   │◀── feedback.jsonl (outcomes)           │
│              │               │                                        │
│              │ multiplier =  │                                        │
│              │ 1+(raw-1)×conf│                                        │
│              └───────┬───────┘                                        │
│                      │                                                │
│                      ▼                                                │
│              ┌───────────────┐   ┌───────────────┐                   │
│              │pipeline.jsonl │   │ revenue.jsonl  │                   │
│              │(all scored    │   │(won gigs,      │                   │
│              │ opportunities)│   │ cumulative $)  │                   │
│              └───────────────┘   └───────────────┘                   │
│                                                                       │
│  Phase 9: EMAIL REPORT                                                │
│  ┌──────────────────────────────────────────────────┐                │
│  │ Daily: findings, PRs, top 5 opps, revenue total  │                │
│  │ Weekly (Sunday): trends, milestones, action items │                │
│  │ Sent via Resend API → vortex.dispatch01@gmail.com │                │
│  └──────────────────────────────────────────────────┘                │
└──────────────────────────────────────────────────────────────────────┘
```

### 2.6 LLM Provider Topology

```
┌───────────────────────────────────────────────────────────┐
│                    LLM CLIENT LAYER                        │
│                                                            │
│  ┌──────────┐   ┌──────────┐   ┌──────────┐              │
│  │ Anthropic │   │ Google AI│   │  OpenAI  │              │
│  │  Client   │   │  Client  │   │  Client  │              │
│  └────┬──────┘   └────┬─────┘   └──────────┘              │
│       │               │                                    │
│       │          ┌────▼─────┐                              │
│       │          │ToolCall  │ (Gemma tool protocol)        │
│       │          │ Adapter  │                              │
│       │          └────┬─────┘                              │
│       │               │                                    │
│  ┌────▼───────────────▼──────┐                             │
│  │      RetryClient          │ (exponential backoff)       │
│  │  └─ 2-3 retries, 2s base │                             │
│  └────────────┬──────────────┘                             │
│               │                                            │
│  ┌────────────▼──────────────┐                             │
│  │     FallbackClient        │                             │
│  │  Primary: Google AI       │                             │
│  │  Secondary: Claude CLI    │                             │
│  │  Trigger: 429, 529, 90%   │                             │
│  │  quota exhaustion         │                             │
│  └────────────┬──────────────┘                             │
│               │                                            │
│  ┌────────────▼──────────────┐                             │
│  │     Claude CLI Client     │ (subscription, unlimited)   │
│  │  ├── OAuth auth           │                             │
│  │  ├── Clears API key       │                             │
│  │  └── JSON output parsing  │                             │
│  └───────────────────────────┘                             │
│                                                            │
│  Testing:                                                  │
│  ┌───────────────────────────┐                             │
│  │     ReplayClient          │ (deterministic responses)   │
│  └───────────────────────────┘                             │
└───────────────────────────────────────────────────────────┘
```

---

## 3. Package Dependency Graph

### 3.1 Layered Architecture (7 Layers)

```
Layer 7 ─ Entry Points
  cmd/vxd/main.go ──▶ cli.Execute()
  cmd/vxd-improve/main.go ──▶ 9-phase pipeline

Layer 6 ─ CLI (Orchestrator)
  internal/cli/ (600 LOC, imports 17 packages)
  └── wires all packages via Cobra commands

Layer 5 ─ Features
  internal/improve/  ──▶ llm
  internal/preflight/ ──▶ config, engine

Layer 4 ─ Orchestration (Hub)
  internal/engine/ (22k LOC, 50 files)
  └── imports: agent, artifact, codegraph, config, git,
               graph, llm, repolearn, runtime, scratchboard, state

Layer 3 ─ Analysis
  internal/repolearn/ ──▶ llm
  internal/codegraph/ (standalone)
  internal/graph/ (standalone)

Layer 2 ─ Domain Models
  internal/agent/  ──▶ config
  internal/llm/    ──▶ agent
  internal/runtime/ ──▶ config, tmux

Layer 1 ─ Foundation (zero internal dependencies)
  internal/config/
  internal/state/
  internal/artifact/
  internal/scratchboard/
  internal/git/
  internal/tmux/
  internal/memory/
  internal/web/
  internal/dashboard/
```

### 3.2 Dependency Matrix

```
                 config state agent llm runtime engine git graph improve
config              .     .     .    .    .       .     .    .     .
state               .     .     .    .    .       .     .    .     .
artifact            .     .     .    .    .       .     .    .     .
scratchboard        .     .     .    .    .       .     .    .     .
git                 .     .     .    .    .       .     .    .     .
tmux                .     .     .    .    .       .     .    .     .
agent               ✓     .     .    .    .       .     .    .     .
llm                 .     .     ✓    .    .       .     .    .     .
runtime             ✓     .     .    .    .       .     .    .     .
repolearn           .     .     .    ✓    .       .     .    .     .
codegraph           .     .     .    .    .       .     .    .     .
graph               .     .     .    .    .       .     .    .     .
engine              ✓     ✓     ✓    ✓    ✓       .     ✓    ✓     .
improve             .     .     .    ✓    .       .     .    .     .
preflight           ✓     .     .    .    .       ✓     .    .     .
cli                 ✓     ✓     ✓    ✓    ✓       ✓     ✓    ✓     ✓

. = no dependency   ✓ = imports
```

### 3.3 Architecture Health

| Metric | Status |
|--------|--------|
| Circular dependencies | **None** (clean DAG) |
| Global state | **None** (all DI via constructors) |
| Interface contracts | **All major deps injected** |
| Largest package (engine) | 22k LOC — justified as coordination hub |
| CLI imports 17 packages | By design — orchestrator role |

---

## 4. Executor Design Rationale

### 4.1 Problem: Monolithic Spawn

The legacy `Runtime.Spawn()` coupled three concerns:
1. **Command building** — assembling CLI args, env vars, prompts
2. **File I/O** — writing prompt files, CLAUDE.md
3. **Session management** — creating tmux sessions, environment propagation

This made the executor untestable, uninspectable, and locked to tmux.

### 4.2 Solution: Adapter/Runner Separation

| Aspect | Adapter (Pure) | Runner (Effects) |
|--------|----------------|-------------------|
| **Responsibility** | Build the command | Execute it |
| **I/O** | None | File writes, tmux calls |
| **Testability** | 100% unit-testable | Integration test |
| **Swappability** | One per CLI tool | One per execution target |
| **Implementations** | CLIAdapter | TmuxRunner, DockerRunner, SSHRunner |

### 4.3 Prompt-Via-File Strategy

**Problem:** `cat file | claude -p` fails in detached tmux sessions (stdin not inherited).

**Solution:**
1. Write combined prompt to `{worktree}/.vxd-prompts/prompt.txt`
2. Reference via shell substitution: `claude -p "$(cat prompt.txt)"`
3. `$(cat)` executes at spawn time when stdin is available
4. File guaranteed to exist before agent starts (TmuxRunner writes setup files first)
5. Survives tmux detach/reattach cycles

**Why not direct argument?** Multi-line prompts with special characters break shell quoting. File-based approach eliminates all escaping concerns.

### 4.4 Worktree Isolation

Each agent gets its own git worktree:
- **Path:** `~/.vxd/worktrees/{storyID}/`
- **Branch:** `vxd/{storyID}`
- **Idempotent:** reuse if valid, recreate if broken
- **Shared objects:** all worktrees share `.git/objects` (no disk duplication)
- **Setup on every spawn:** CLAUDE.md written fresh each time (worktrees persist across runs)

### 4.5 Why CLAUDE.md in Every Worktree

Claude Code plugins (brainstorming, skills) have higher priority than prompt instructions. Writing a project-level CLAUDE.md into each worktree overrides these plugins, ensuring agents follow VXD's prompts rather than plugin behavior. This was discovered through production failures where brainstorming skills hijacked agent execution.

---

## 5. Revenue Pipeline Design

### 5.1 Dual Revenue Model

VXD generates revenue through two channels:

#### Channel A: Client Work (Billable)

```
Requirement → Estimate → Approve → Execute → Report → Invoice
```

- **Estimation:** Fibonacci complexity → hours → cost at configured rate
- **Default rate:** $150/hr (configurable in `billing.default_rate`)
- **Fibonacci-to-hours mapping:**

| Fibonacci Points | Hours (Low) | Hours (High) | Cost @ $150/hr |
|:---:|:---:|:---:|:---:|
| 1 | 0.5 | 1.0 | $75 – $150 |
| 2 | 1.0 | 2.0 | $150 – $300 |
| 3 | 2.0 | 3.0 | $300 – $450 |
| 5 | 4.0 | 6.0 | $600 – $900 |
| 8 | 8.0 | 12.0 | $1,200 – $1,800 |
| 13 | 16.0 | 24.0 | $2,400 – $3,600 |

#### Channel B: Autonomous Opportunity Discovery

```
Scrape → Score → Bayesian Adjust → Propose → Send → Win → Revenue
```

- **Sources:** Jobicy, Remotive, Upwork, Freelancer, PeoplePerHour
- **7-day keyword rotation** covering backend, fullstack, API, automation, AI/ML, Python, and JavaScript ecosystems
- **Weekly source discovery** adds new platforms automatically

### 5.2 Opportunity Lifecycle

```
new → reviewed → interested → proposal_drafted → sent → won/lost/expired
```

### 5.3 Scoring Formula

```
Rank = (RelevanceScore × 3) + (BudgetScore × 2) + WinProbability
```

Where each score is 0-10, giving a maximum raw rank of **60**.

After Bayesian adjustment:
```
AdjustedRank = Rank × ∏(matching_multipliers)
```

### 5.4 Revenue Tracking

Revenue entries track per-opportunity amounts with running cumulative totals. Mission milestones trigger email alerts at: **$1K, $5K, $10K, $25K, $50K, $100K, $250K, $500K, $1M**.

---

## 6. Bayesian Feedback Mathematics

### 6.1 The Learning Problem

When the system drafts proposals, some win and some lose. Without learning, every opportunity of the same rank appears equal. The Bayesian feedback loop makes the system **smarter over time** by adjusting opportunity scores based on empirical outcomes.

### 6.2 Data Collection

Each outcome is recorded as a `FeedbackEntry` with fields: `type` (proposal/pr/finding), `category`, `source`, `skill_set`, `price_range`, and `outcome` (won/lost/merged/rejected/acted/ignored).

Outcomes are classified as:
- **Success:** `won`, `merged`, `acted`
- **Failure:** `lost`, `rejected`, `ignored`

### 6.3 Success Rate Computation

Entries are grouped by **4 dimensions**: source, category, skill_set, price_range.

For each dimension value `d`:

```
                    count(success outcomes in d)
SuccessRate(d) = ──────────────────────────────────
                    count(all outcomes in d)
```

**Example:**
```
source:jobicy       → 7 won / 15 total = 0.467
skill_set:Go,REST   → 12 won / 20 total = 0.600
price_range:$5K-$10K → 5 won / 8 total = 0.625
```

### 6.4 Confidence Scoring

Small sample sizes can mislead. VXD uses a confidence function to dampen adjustments when data is sparse:

```
                    min(n, 30)
Confidence(n) = ──────────────
                      30
```

Where `n` = total samples for that dimension.

| Samples (n) | Confidence | Effect |
|:---:|:---:|:---|
| < 5 | — | **No adjustment** (too few samples, dimension skipped) |
| 5 | 0.167 | Very conservative |
| 10 | 0.333 | Moderate dampening |
| 20 | 0.667 | Mostly trusting data |
| 30+ | 1.000 | Full trust in empirical rate |

### 6.5 Multiplier Derivation

The raw multiplier uses a **50% baseline** — a dimension with exactly 50% success rate gets a neutral multiplier of 1.0:

```
RawMultiplier(d) = 0.5 + SuccessRate(d)
```

| SuccessRate | RawMultiplier | Effect |
|:---:|:---:|:---|
| 0.00 | 0.50 | Heavy penalty (halve the rank) |
| 0.25 | 0.75 | Moderate penalty |
| 0.50 | 1.00 | Neutral (no change) |
| 0.75 | 1.25 | Moderate boost |
| 1.00 | 1.50 | Maximum boost (50% increase) |

### 6.6 Conservative Blending

The raw multiplier is blended toward 1.0 (neutral) based on confidence, preventing overreaction to small sample sizes:

```
BlendedMultiplier(d) = 1.0 + (RawMultiplier(d) - 1.0) × Confidence(n)
```

**Worked Example A (Low Confidence):**

```
Dimension:       source:jobicy
Samples (n):     15
SuccessRate:     7/15 = 0.467
Confidence:      min(15, 30) / 30 = 0.500
RawMultiplier:   0.5 + 0.467 = 0.967
BlendedMultiplier: 1.0 + (0.967 - 1.0) × 0.5
                 = 1.0 + (-0.033 × 0.5)
                 = 0.983

→ Jobicy gets a mild 1.7% penalty (slightly below 50% win rate, dampened by low confidence)
```

**Worked Example B (High Confidence):**

```
Dimension:       skill_set:Go,PostgreSQL
Samples (n):     30
SuccessRate:     21/30 = 0.700
Confidence:      min(30, 30) / 30 = 1.000
RawMultiplier:   0.5 + 0.7 = 1.200
BlendedMultiplier: 1.0 + (1.2 - 1.0) × 1.0
                 = 1.200

→ Go+PostgreSQL gets a full 20% rank boost (high confidence, no dampening)
```

### 6.7 Composite Adjustment

When an opportunity matches multiple dimensions, multipliers compound multiplicatively:

```
FinalRank = BaseRank × ∏(BlendedMultiplier for each matching dimension)
```

**Full Example:** A Go+PostgreSQL job from Jobicy at $5K-$10K:

```
Base Rank:                        45
× source:jobicy multiplier:       0.983
× skill_set:Go,PostgreSQL:        1.200
× price_range:$5K-$10K:           1.125

AdjustedRank = 45 × 0.983 × 1.200 × 1.125
             = 45 × 1.327
             = 59.7
             ≈ 60
```

### 6.8 Convergence Properties

| Property | Guarantee |
|----------|-----------|
| **Cold start** | No adjustments until ≥5 samples per dimension |
| **Stability** | Blending dampens oscillation from noisy data |
| **Bounded** | Multiplier range: [0.5, 1.5] per dimension |
| **Monotonic confidence** | More data → more trust, never decreases |
| **Immutable** | `AdjustOpportunityScore()` returns new Opportunity, never mutates |
| **Deterministic** | Same inputs → same outputs (sorted dimensions, no randomness) |

---

## 7. 3-Phase Deployment Strategy

### Phase 1: Solo Operator (Current — Months 1-6)

**Infrastructure:**
- macOS workstation, launchd cron (6am daily)
- Claude Max subscription (unlimited CLI), Google AI free tier
- GitHub private repo, Resend free tier email

**Revenue Target:** $0 → $10,000 cumulative

**Architecture Focus:**
- Event sourcing stability
- Crash recovery hardening
- Bayesian feedback loop seeding (needs 30+ proposal outcomes)
- Self-improvement engine tuning (relevance threshold calibration)

**Deployment Model:**
```
macOS (single machine)
├── vxd binary (~/.local/bin/vxd)
├── vxd-improve daemon (launchd, 6am daily)
├── tmux sessions (agents)
├── SQLite + events.jsonl (~/.vxd/)
└── MemPalace (semantic memory)
```

**Key Risks:**
- Single machine = single point of failure
- WiFi dependency for 6am cron (mitigated by 30s DNS poll)
- Claude Max subscription dependency

---

### Phase 2: Small Team (Months 6-18)

**Infrastructure:**
- Linux VPS or dedicated server
- Docker containers for agent isolation (DockerRunner)
- PostgreSQL or replicated SQLite for state
- CI/CD pipeline (GitHub Actions)
- Remote dashboard access

**Revenue Target:** $10,000 → $100,000 cumulative

**Architecture Changes:**
- **DockerRunner replaces TmuxRunner** for agent isolation
- **SSHRunner** for distributed execution across machines
- **Multi-project isolation** (already built — `~/.vxd/projects/<name>/`)
- **API server** for external integrations
- **Webhook-based notifications** (Slack, Discord)

**Deployment Model:**
```
VPS / Dedicated Server
├── vxd API server (HTTP)
├── vxd-improve daemon (systemd)
├── Docker containers (per-agent)
│   ├── agent-1 (worktree A)
│   ├── agent-2 (worktree B)
│   └── agent-n
├── PostgreSQL (state)
└── Nginx reverse proxy (dashboard)
```

**Key Milestones:**
- First paying client onboarded
- Bayesian loop has 30+ samples per top dimension
- DockerRunner in production
- CI/CD pipeline unblocked
- Proposal win rate > 20%

---

### Phase 3: Platform (Months 18-36)

**Infrastructure:**
- Kubernetes for agent orchestration
- Multi-tenant with client isolation
- Stripe billing integration
- SLA guarantees (99.5% uptime)
- Geographic distribution (multi-region)

**Revenue Target:** $100,000 → $1,000,000+ cumulative

**Architecture Changes:**
- **Kubernetes Operator** replaces Docker/tmux
- **Multi-tenant event stores** (per-client isolation)
- **API-first architecture** (REST + WebSocket)
- **Agent marketplace** (custom agent types)
- **White-label capability** (client-branded dashboards)

**Deployment Model:**
```
Kubernetes Cluster
├── Control Plane
│   ├── API Server (vxd-api)
│   ├── Scheduler (vxd-dispatcher)
│   ├── Monitor (vxd-monitor)
│   └── Revenue Engine (vxd-improve)
├── Agent Pool
│   ├── Node 1: Claude Code agents
│   ├── Node 2: Codex agents
│   └── Node 3: Gemini agents
├── Data Layer
│   ├── PostgreSQL (HA, per-tenant schemas)
│   ├── S3 (artifacts, logs)
│   └── Redis (queues, caching)
└── Ingress
    ├── Client API Gateway
    ├── Dashboard (React SPA)
    └── Webhook receivers
```

---

## 8. Technical Decision Table

| # | Decision | Chosen | Alternatives Considered | Rationale |
|---|----------|--------|------------------------|-----------|
| 1 | **State Management** | Event sourcing (JSONL + SQLite) | Pure CRUD, PostgreSQL, Redis | Full audit trail, replay capability, temporal queries. JSONL is append-only (crash-safe with fsync). SQLite provides fast queries without external dependencies. |
| 2 | **Agent Isolation** | Git worktrees + tmux | Docker, VMs, chroot | Worktrees share git objects (minimal disk). Tmux survives monitor crashes. Agents can be inspected live (`tmux attach`). Docker/SSH available for Phase 2. |
| 3 | **Prompt Delivery** | File-based (`$(cat prompt.txt)`) | stdin pipe, CLI argument, env var | stdin fails in detached tmux. CLI args break with special chars. Env vars have length limits. File approach is reliable and inspectable. |
| 4 | **LLM Abstraction** | Unified Client interface + wrappers | Provider-specific code paths | Single interface enables FallbackClient, RetryClient, ReplayClient composition. New providers added with zero engine changes. |
| 5 | **Cost Optimization** | Claude CLI (subscription) + Google AI (free tier) | All-API, all-subscription | Claude Max = unlimited CLI calls. Google AI free tier = 10 RPM / 1500 RPD for execution roles. Fallback chain minimizes paid API usage. |
| 6 | **Complexity Scoring** | Fibonacci (1,2,3,5,8,13) | T-shirt (S/M/L/XL), linear | Fibonacci naturally maps to effort: large gaps between high values prevent underestimation. Industry standard in agile. |
| 7 | **Dependency Resolution** | DAG with topological sort | Linear queue, priority queue | Wave-based parallelism requires understanding which stories can execute concurrently. DAG enables optimal dispatch. |
| 8 | **Configuration** | YAML chain (repo → global → defaults) | JSON, TOML, env-only | YAML is human-readable for complex nested config. Chain allows repo-specific overrides. All fields have sensible defaults. |
| 9 | **QA Pipeline** | Declarative criteria (YAML-defined) | Hardcoded checks, plugin system | Declarative criteria are testable as pure functions. New check types require only adding a `kind` handler. No dynamic loading risk. |
| 10 | **Merge Strategy** | Squash merge via `gh pr merge --squash` | Regular merge, rebase | Squash keeps main branch clean. One commit per story = easy revert. PR preserves full commit history for audit. |
| 11 | **Monitoring** | Polling (10s interval) + fingerprinting | WebSocket, file watch, signals | Polling is simple and reliable across tmux/Docker/SSH. Fingerprinting detects stuck agents without parsing output. |
| 12 | **Error Classification** | 8-category enum | Free-text, LLM classification | Enum categories map to deterministic fix suggestions. LLM classification would be non-deterministic and expensive at retry frequency. |
| 13 | **CLAUDE.md Injection** | Write on every spawn | Write once, skip if exists | Plugins have higher priority than prompt instructions. Must override with project-level CLAUDE.md. Fresh write handles worktree persistence. |
| 14 | **Revenue Scoring** | Weighted formula + Bayesian adjustment | Pure ML model, manual ranking | Formula is interpretable and debuggable. Bayesian layer adds learning without black-box risk. Needs only 5 samples to activate. |
| 15 | **Crash Recovery** | Checkpoint + event replay + 5 scenarios | WAL-only, manual recovery | Event log is crash-safe (append+fsync). Checkpoints record phase transitions. 5 scenarios cover all observed failure modes. |

---

## 9. Risk Assessment

### 9.1 Technical Risks

| Risk | Severity | Probability | Mitigation | Status |
|------|----------|-------------|------------|--------|
| **LLM API rate limiting** | HIGH | MEDIUM | FallbackClient with quota tracking, RetryClient with exponential backoff, Claude CLI subscription as fallback | MITIGATED |
| **Tmux session crash** | MEDIUM | LOW | Crash recovery with 5 scenarios, checkpoint writes, STORY_RESET event | MITIGATED |
| **Git worktree corruption** | MEDIUM | LOW | Idempotent creation (reuse/recreate), Reaper cleanup, `--force` removal | MITIGATED |
| **SQLite lock contention** | LOW | LOW | WAL mode enables concurrent reads, single writer (event append), projection is synchronous | MITIGATED |
| **Prompt injection via agent input** | HIGH | MEDIUM | Input sanitization (ValidateModelName, ValidateSessionName, ValidateShellArg), QuoteShellArg wrapping | MITIGATED |
| **Agent infinite loop** | MEDIUM | MEDIUM | Watchdog fingerprinting detects stuck agents, configurable stuck threshold, AGENT_STUCK event triggers escalation | MITIGATED |
| **Merge conflicts between parallel agents** | MEDIUM | HIGH | Serialized merge (mergeMu), rebase onto latest base, LLM ConflictResolver (optional), reset to draft on failure | MITIGATED |
| **Stale worktrees consuming disk** | LOW | HIGH | Reaper auto-cleanup, configurable retention, `worktree_prune: immediate` option | MITIGATED |
| **Single machine failure (Phase 1)** | HIGH | LOW | Phase 2 Docker/SSH runners distribute execution. Event log is replayable for recovery. | ACCEPTED |
| **Claude Max subscription dependency** | HIGH | LOW | FallbackClient chains to Google AI. Code works with raw API keys. Not dependent on single provider. | PARTIALLY MITIGATED |

### 9.2 Business Risks

| Risk | Severity | Probability | Mitigation |
|------|----------|-------------|------------|
| **Low proposal win rate** | HIGH | MEDIUM | Bayesian feedback loop improves targeting over time. Source discovery finds better-fit platforms weekly. Min 30 samples before trusting adjustments. |
| **Client delivery quality** | HIGH | LOW | 5-tier escalation catches failures. Code review + QA pipeline enforces quality. Human review gates available (plan_only, manual modes). |
| **LLM cost overrun** | MEDIUM | LOW | Claude CLI = subscription (unlimited). Google AI free tier for junior roles. LLMCostConfig tracks per-token costs. MarginPercent computed per estimate. |
| **Competitor emergence** | MEDIUM | MEDIUM | Self-improvement engine tracks competitors daily (SWE-agent, OpenHands, AgentFlow). Feature parity maintained. Event sourcing + escalation chain is differentiated. |
| **API key exposure** | CRITICAL | LOW | Keys in env vars, never in code. ANTHROPIC_API_KEY cleared for agents (use OAuth). Secret scanning in self-improvement quality gates. |
| **Solo operator burnout** | MEDIUM | HIGH | Full autonomy: vxd-improve runs without human input. Dashboard provides visibility without hands-on. Phase 2 introduces team scaling. |

### 9.3 Risk Heat Map

```
                    LOW PROB        MEDIUM PROB       HIGH PROB
              ┌─────────────────┬─────────────────┬─────────────────┐
   CRITICAL   │ API key exposure│                 │                 │
              ├─────────────────┼─────────────────┼─────────────────┤
   HIGH       │ Machine failure │ Prompt injection│                 │
              │ Subscription    │ Low win rate    │                 │
              │ revoked         │ LLM rate limit  │                 │
              ├─────────────────┼─────────────────┼─────────────────┤
   MEDIUM     │ Tmux crash      │ Agent loop      │ Merge conflicts │
              │ Worktree corrupt│ Competitor      │ Burnout         │
              ├─────────────────┼─────────────────┼─────────────────┤
   LOW        │ SQLite lock     │ LLM cost overrun│ Stale worktrees │
              └─────────────────┴─────────────────┴─────────────────┘
```

---

## 10. Revenue Projections

### 10.1 Assumptions

| Parameter | Value | Basis |
|-----------|-------|-------|
| Hourly rate | $150 | Config default, global market benchmark |
| Opportunities scraped/day | 10-30 | 5 sources × 7-day keyword rotation |
| Proposal draft rate | 3-5/week | Top-ranked after Bayesian filter |
| Initial win rate | 5-10% | Cold start, no Bayesian history |
| Mature win rate | 15-25% | After 30+ feedback samples per dimension |
| Average gig value | $2,000-$5,000 | Freelance backend/API work |
| Self-improvement PRs/week | 3-7 | Quality-gated, automated |
| LLM operational cost | ~$0/month | Claude Max subscription + Google AI free tier |

### 10.2 Phase 1 Projection (Solo — Months 1-6)

```
Month 1:  Seeding. 0 wins. Building feedback history.
          Proposals sent: ~12    Revenue: $0
          Feedback entries: ~12  Confidence: LOW

Month 2:  Early traction. 1-2 wins expected.
          Proposals sent: ~20    Revenue: $2,000-$5,000
          Feedback entries: ~32  Confidence: ACTIVATING

Month 3:  Bayesian loop warming up. Better targeting.
          Proposals sent: ~20    Revenue: $3,000-$8,000
          Win rate: ~10-15%      Cumulative: $5,000-$13,000

Month 4-6: Loop mature. Compounding returns.
          Proposals sent: ~20/mo Revenue: $5,000-$10,000/mo
          Win rate: ~15-20%      Cumulative: $20,000-$43,000
```

**6-month range: $20,000 – $43,000**

### 10.3 Phase 2 Projection (Team — Months 6-18)

```
Month 6-9:   Scale sources. Add team capacity.
             Active clients: 2-5
             Monthly revenue: $8,000-$15,000
             Cumulative: $43,000-$88,000

Month 9-12:  Repeat clients. Referral network.
             Active clients: 5-8
             Monthly revenue: $12,000-$25,000
             Cumulative: $88,000-$163,000

Month 12-18: Pipeline matures.
             Active clients: 8-15
             Monthly revenue: $20,000-$40,000
             Cumulative: $163,000-$403,000
```

**18-month range: $163,000 – $403,000**

### 10.4 Phase 3 Projection (Platform — Months 18-36)

```
Month 18-24: Multi-tenant platform. SaaS pricing.
             Clients: 15-30
             Monthly revenue: $30,000-$60,000
             Cumulative: $403,000-$763,000

Month 24-36: Market establishment.
             Clients: 30-50+
             Monthly revenue: $50,000-$100,000+
             Cumulative: $763,000-$1,963,000+
```

**36-month range: $763,000 – $1,963,000+**

### 10.5 Revenue Growth Trajectory

```
Cumulative Revenue ($)

$2M ─                                                           ╱
     │                                                        ╱
     │                                                      ╱
$1.5M                                                    ╱
     │                                                 ╱
     │                                              ╱
$1M ─                                           ╱─── Optimistic
     │                                       ╱
     │                                   ╱──── Base
$500K                                ╱
     │                           ╱
     │                      ╱──────── Conservative
$200K                  ╱
     │            ╱
$50K ─       ╱
     │   ╱
$0  ─╱─────────┬─────────┬─────────┬─────────┬──────────┬───
     0    M6        M12       M18       M24       M30     M36

     ◄── Phase 1 ──▶◄──── Phase 2 ────▶◄──── Phase 3 ────▶
```

### 10.6 Break-Even Analysis

| Cost | Monthly | Annual |
|------|---------|--------|
| Claude Max subscription | $200 | $2,400 |
| VPS (Phase 2) | $50-200 | $600-$2,400 |
| Domain + email | $10 | $120 |
| GitHub Pro | $4 | $48 |
| **Total Phase 1** | **~$214** | **~$2,568** |
| **Total Phase 2** | **~$464** | **~$5,568** |

**Break-even:** Month 2 (first won gig covers ~6 months of operational costs)

### 10.7 Mission Milestone Timeline (Projected)

| Milestone | Conservative | Base | Optimistic |
|-----------|:---:|:---:|:---:|
| $1,000 | Month 2 | Month 2 | Month 1 |
| $5,000 | Month 3 | Month 3 | Month 2 |
| $10,000 | Month 5 | Month 4 | Month 3 |
| $25,000 | Month 8 | Month 6 | Month 5 |
| $50,000 | Month 12 | Month 9 | Month 7 |
| $100,000 | Month 16 | Month 13 | Month 10 |
| $250,000 | Month 22 | Month 18 | Month 14 |
| $500,000 | Month 28 | Month 24 | Month 20 |
| $1,000,000 | Month 36 | Month 30 | Month 24 |

---

## Appendix A: LLM Provider Configuration

### Model Routing by Role

| Role | Default Provider | Default Model | Max Tokens | Purpose |
|------|:---:|:---|:---:|:---|
| Tech Lead | Anthropic | claude-opus-4-20250514 | 16,000 | Requirement decomposition |
| Senior | Anthropic | claude-sonnet-4-20250514 | 8,000 | Complex features, code review |
| Intermediate | Google AI | gemma-4-27b-it | 4,000 | Moderate tasks |
| Junior | Google AI | gemma-4-27b-it | 4,000 | Simple features |
| QA | Anthropic | claude-sonnet-4-20250514 | 8,000 | Quality assurance |
| Supervisor | Google AI | gemma-4-27b-it | 4,000 | Drift detection |
| Manager | Anthropic | claude-sonnet-4-20250514 | 8,000 | Failure diagnosis |

### Cost Strategy

| Tier | Provider | Cost Model | Rationale |
|------|----------|:---:|:---|
| Verification (TL, Senior, QA, Manager) | Anthropic | Subscription (unlimited) | Accuracy matters, cost irrelevant |
| Execution (Junior, Intermediate, Supervisor) | Google AI | Free tier (10 RPM, 1500 RPD) | Volume work, free is better |
| Fallback | Anthropic API | Per-token | Safety net, rarely hit |

---

## Appendix B: Event Type Reference

| Event | Key Payload Fields | Projection Effect |
|-------|---------------|-------------------|
| `REQ_SUBMITTED` | requirement, project | INSERT requirements |
| `REQ_PLANNED` | story_count | UPDATE requirements.status |
| `STORY_CREATED` | title, complexity, depends_on, wave_hint | INSERT stories + story_deps |
| `STORY_ASSIGNED` | agent_id, wave, role | UPDATE stories.status, agent |
| `STORY_STARTED` | worktree, runtime, tier, role | UPDATE stories.status |
| `STORY_COMPLETED` | — | UPDATE stories.status |
| `STORY_REVIEW_PASSED` | summary | UPDATE stories.status |
| `STORY_REVIEW_FAILED` | comments | UPDATE stories.status → draft |
| `STORY_QA_PASSED` | checks, quality_score | UPDATE stories.status |
| `STORY_QA_FAILED` | checks, errors | UPDATE stories.status → draft |
| `STORY_MERGED` | pr_url, pr_number | UPDATE stories.status, pr_url |
| `STORY_ESCALATED` | from_tier, to_tier, reason | INSERT escalations |
| `STORY_REWRITTEN` | new_title, new_description | UPDATE stories |
| `STORY_SPLIT` | child_ids | UPDATE stories |
| `STORY_RESET` | reason, scenario | UPDATE stories.status → draft |
| `AGENT_SPAWNED` | session_name, model | INSERT agents |
| `AGENT_STUCK` | fingerprint | UPDATE agents.status |

---

## Appendix C: Key File Reference

| Component | File | Purpose |
|-----------|------|:---|
| Planner | `engine/planner.go` | Tech Lead LLM decomposition |
| Dispatcher | `engine/dispatcher.go` | Wave-based parallel dispatch |
| Executor | `engine/executor.go` | Worktree + agent spawning |
| Monitor | `engine/monitor.go` | Polling loop + post-execution pipeline |
| Escalation | `engine/escalation.go` | 5-tier escalation machine |
| Manager | `engine/manager.go` | Tier 2 diagnosis |
| QA | `engine/qa.go` | Lint/build/test execution |
| Merger | `engine/merger.go` | PR creation + auto-merge |
| Cost | `engine/cost.go` | Fibonacci-to-hours mapping |
| CLIAdapter | `runtime/cli_adapter.go` | Pure command building |
| TmuxRunner | `runtime/tmux_runner.go` | Tmux session execution |
| DockerRunner | `runtime/docker_runner.go` | Container execution |
| SSHRunner | `runtime/ssh_runner.go` | Remote execution |
| Feedback | `improve/feedback.go` | Bayesian scoring loop |
| Opportunities | `improve/opportunities.go` | Opportunity lifecycle |
| Research | `improve/research.go` | Daily source scraping |
| Proposals | `improve/proposal.go` | LLM-drafted proposals |
| Events | `state/events.go` | Event type definitions |
| SQLite | `state/sqlite.go` | Projection store |
| Config | `config/config.go` | Configuration types |
| Roles | `agent/roles.go` | Role definitions + routing |
| Prompts | `agent/prompts.go` | System/goal prompt templates |
| Sanitize | `runtime/sanitize.go` | Input validation |

---

## 11. Security & Data Privacy

### 11.1 Data Flow Classification

Every piece of data that leaves VXD's process boundary is classified below:

```
┌─────────────────────────────────────────────────────────────────────────┐
│                     DATA FLOW TO EXTERNAL SYSTEMS                       │
│                                                                         │
│  ┌─────────────┐                                                        │
│  │ Requirement  │──▶ Tech Lead LLM (Anthropic API / Claude CLI)         │
│  │ text (user)  │    Contains: requirement description, tech stack,     │
│  │              │    repo profile summary                               │
│  └─────────────┘                                                        │
│                                                                         │
│  ┌─────────────┐                                                        │
│  │ Story meta   │──▶ Manager LLM (diagnosis), Reviewer LLM (review)    │
│  │              │    Contains: title, description, acceptance criteria, │
│  │              │    git diff, file tree, agent logs (last 4KB)         │
│  └─────────────┘                                                        │
│                                                                         │
│  ┌─────────────┐                                                        │
│  │ Agent prompt │──▶ Agent runtime (Claude Code / Codex / Gemini CLI)   │
│  │              │    Contains: system prompt, goal, wave context,       │
│  │              │    review feedback, attempt history, repo profile     │
│  └─────────────┘                                                        │
│                                                                         │
│  ┌─────────────┐                                                        │
│  │ Opportunity  │──▶ Claude CLI (proposal drafting)                     │
│  │ data         │    Contains: company, title, budget, skills,          │
│  │              │    job description                                    │
│  └─────────────┘                                                        │
│                                                                         │
│  ┌─────────────┐                                                        │
│  │ Email digest │──▶ Resend API                                         │
│  │              │    Contains: finding summaries, opportunity rankings,  │
│  │              │    revenue totals, action items                       │
│  └─────────────┘                                                        │
└─────────────────────────────────────────────────────────────────────────┘
```

### 11.2 LLM Provider Data Policies

| Provider | Data Retention | Training on Input | Zero-Retention Option | VXD Usage |
|----------|:---:|:---:|:---:|:---|
| **Anthropic API** | 30 days (safety) | No (commercial) | Yes (enterprise) | Tech Lead, Senior, QA, Manager |
| **Claude CLI (Max)** | Per subscription terms | No | N/A | Primary agent runtime |
| **Google AI** | Per Gemini API terms | May be used | Enterprise tier | Junior, Intermediate, Supervisor |
| **OpenAI API** | 30 days (default) | No (opted out) | Yes (enterprise) | Optional, not default |

**Client recommendation:** For sensitive client work, restrict to Anthropic-only models with enterprise zero-retention agreement. Configure in `vxd.yaml`:
```yaml
models:
  junior: {provider: anthropic, model: claude-haiku-4-5-20251001}
  intermediate: {provider: anthropic, model: claude-sonnet-4-20250514}
```

### 11.3 Secret Management Lifecycle

```
Secret Ingestion          Runtime Propagation          Cleanup
─────────────────         ────────────────────         ──────────
Environment vars          tmux env exports             Not auto-rotated
├── ANTHROPIC_API_KEY     ├── Stripped for agents       ├── Agent uses OAuth
│                         │   (forces subscription)    │
├── GOOGLE_API_KEY        ├── Passed to agents          ├── Visible in tmux env
├── OPENAI_API_KEY        ├── Passed to agents          ├── Visible in tmux env
├── RESEND_API_KEY        ├── vxd-improve only          ├── Not propagated
└── GITHUB_TOKEN          └── gh CLI inherits           └── Per-session
```

**Security gaps identified:**

| Gap | Severity | Location | Recommendation |
|-----|----------|----------|----------------|
| Google API key in URL query string | HIGH | `llm/google.go:116` | Move to header-based auth when Google AI supports it |
| No prompt sanitization for requirements | MEDIUM | `engine/planner.go` | Run `ScanForSecrets()` + `DetectPromptInjection()` on user input before LLM calls |
| Log retention config not enforced | MEDIUM | `config/loader.go:74` | Implement background job using `workspace.log_retention_days` |
| Proposal files unencrypted on disk | MEDIUM | `improve/proposal.go:93` | Encrypt at rest or restrict file permissions to 0600 |
| State files world-readable (0644) | LOW | All file writes | Use 0600 for sensitive files (events.jsonl, store.db, proposals) |

### 11.4 Data Retention & GDPR

**Current state:** No automated data deletion or GDPR support.

| Data Type | Current Retention | Recommended | GDPR Relevance |
|-----------|:---:|:---:|:---|
| Event log (events.jsonl) | Forever | 1 year + archive | Contains requirement text, story descriptions |
| Agent logs | Forever (config says 30d, unenforced) | 30 days | Contains code output, paths, error traces |
| SQLite projections | Forever | Rebuild from events | Derived data, can be regenerated |
| Opportunity records | Forever | 6 months after expiry | Contains company names, budgets |
| Proposal drafts | Forever | Delete after outcome | Contains client-identifiable information |
| Revenue ledger | Forever | 7 years (tax) | Financial records, exempt from deletion |

**GDPR Action Items (Phase 2):**
1. Implement `vxd purge --project <name>` to delete all project data
2. Add `vxd export --project <name>` for data subject access requests
3. Enforce `log_retention_days` via background cleanup
4. Add `expired` status auto-transition for opportunities older than 90 days
5. Encrypt sensitive JSONL files at rest (proposals, feedback, revenue)

---

## 12. Competitive Positioning

### 12.1 Competitive Landscape

VXD actively monitors 16 competitors via its self-improvement engine (`improve/research.go`):

| Competitor | Category | Key Strength | VXD Advantage |
|-----------|----------|:---|:---|
| **SWE-agent** | Research agent | Code search + repo archaeology | VXD: production-grade with escalation, not research prototype |
| **OpenHands** | Multi-tool agent | Docker + web browsing ecosystem | VXD: event sourcing + crash recovery + cost estimation |
| **Devin** | Proprietary agent | Polished UX, full-stack optimization | VXD: open, pluggable, multi-client, language-agnostic |
| **Cursor** | IDE agent | Integrated IDE experience | VXD: service orchestrator for CI/CD and remote teams |
| **Aider** | Pair programmer | Fast iteration, single-file edits | VXD: multi-agent team, not pair programming |
| **CrewAI** | Framework | Easy agent composition | VXD: end-to-end system, not a framework |
| **AutoGen** | Framework | Microsoft-backed multi-agent | VXD: simpler, event-sourced, production-focused |
| **AgentFlow** | DAG orchestrator | DSL-based workflows | VXD: adopted best patterns (criteria, attempts, templates) |

### 12.2 Feature Differentiation Matrix

| Feature | VXD | SWE-agent | OpenHands | Devin | Cursor |
|---------|:---:|:---:|:---:|:---:|:---:|
| 5-tier escalation chain | **UNIQUE** | — | — | — | — |
| Manager diagnosis role | **UNIQUE** | — | — | — | — |
| Event sourcing architecture | **UNIQUE** | — | — | — | — |
| Smart error analysis (8 categories) | **UNIQUE** | — | — | — | — |
| Crash recovery (5 scenarios) | **UNIQUE** | — | — | — | — |
| Cost estimation + client quotes | **UNIQUE** | — | — | — | — |
| Self-improvement engine | **UNIQUE** | — | — | — | — |
| Revenue engine + Bayesian proposals | **UNIQUE** | — | — | — | — |
| Declarative QA criteria | **UNIQUE** | — | — | — | — |
| Per-attempt tracking | **UNIQUE** | — | — | — | — |
| Wave context sharing | **UNIQUE** | — | — | — | — |
| Repo learning (3-pass) | **UNIQUE** | — | — | — | — |
| Client delivery reports | **UNIQUE** | — | — | — | — |
| 3 human review modes | **DIFF** | — | — | — | Partial |
| Multi-project isolation | **DIFF** | — | — | — | Partial |
| Wave-based parallel dispatch | Common | Partial | Partial | Yes | Yes |
| Pluggable runtimes | Common | Limited | Limited | Proprietary | Proprietary |

### 12.3 Architectural Moat

Five structural advantages that are hard to replicate:

1. **Event sourcing as core** — Complete audit trail, crash recovery by replay, temporal queries. Retrofitting event sourcing into a CRUD system is a rewrite, not a feature add.

2. **Tiered agent hierarchy** — Fibonacci complexity routing uses cheap models first, escalates only on failure. Competitors use best-available-model for everything (higher cost, no learning).

3. **Manager diagnosis tier** — When stories fail repeatedly, a dedicated LLM analyzes the failure pattern and can rewrite, split, or retry with targeted fixes. No competitor has autonomous failure diagnosis.

4. **Self-improving revenue loop** — System researches, implements improvements, discovers opportunities, drafts proposals, and learns from outcomes. This compounds: more data → better Bayesian scoring → higher win rate → more revenue → more data.

5. **Adapter/Runner separation** — Pure command building (testable) + pluggable execution (tmux/Docker/SSH). Moving from single-machine to distributed is a config change, not an architecture change.

### 12.4 Positioning Strategy

**Tagline:** *"The autonomous development team that gets smarter every day."*

**By audience:**

| Audience | Message | Proof Point |
|----------|---------|:---|
| **Technical buyers** | "Event-sourced orchestration with 5-tier escalation" | 30+ wiring tests, crash recovery, full audit trail |
| **Business buyers** | "AI team that estimates, executes, and reports like a human team" | Cost estimation, delivery reports, human review gates |
| **Investors** | "Self-improving revenue engine with Bayesian feedback loop" | Autonomous opportunity discovery, compounding win rate |
| **Freelance clients** | "Ship faster with AI agents, pay only for results" | Fibonacci-based quotes, transparent reporting |

---

## 13. Agent Quality Metrics & SLA

### 13.1 Quality Metrics Taxonomy

VXD tracks quality across 4 dimensions:

```
┌─────────────────────────────────────────────────────────────┐
│                   QUALITY METRICS                            │
│                                                              │
│  Per-Story                    Per-Agent (Reputation)         │
│  ──────────                   ──────────────────────         │
│  QA Quality Score (1-5)       OverallScore (0-100)           │
│  ├── 5: All checks pass      ├── Quality (50%)              │
│  ├── 4: ≥80% pass            │   └── Avg QA score (1-5)     │
│  ├── 3: ≥60% pass            ├── Reliability (30%)          │
│  ├── 2: ≥40% pass            │   └── 5 if no escalation,   │
│  └── 1: Failed               │       2 if escalated         │
│                               └── Speed (20%)               │
│  Escalation Count                 └── Inverse of duration   │
│  Retry Count                          (capped at 3600s)     │
│  First-Pass Success (bool)                                   │
│  Duration (seconds)           Per-Requirement                │
│  Attempt History              ──────────────────             │
│                               FirstPassRate (%)              │
│  Per-Pipeline                 AvgPlanningTime                │
│  ────────────                 AvgExecutionTime               │
│  TotalStories                 AvgReviewTime                  │
│  StoriesMerged                AvgQATime                      │
│  StoriesEscalated             EscalationsByTier              │
│  FirstPassRate (%)                                           │
└─────────────────────────────────────────────────────────────┘
```

### 13.2 Trace-Based Activity Metrics

The trace normalizer (`engine/trace.go`) extracts 8 event kinds from agent logs:

| Trace Kind | Pattern | What It Measures |
|-----------|---------|:---|
| `tool_call` | Read, Write, Edit, Bash, Grep, Glob | Agent tool usage frequency |
| `file_edit` | "Edited/Updated/Modified" + filename | Files touched per story |
| `file_create` | "Created/Wrote" + filename | New files per story |
| `command` | `$` prefix shell commands | Shell activity |
| `error` | Error, FAIL, panic, fatal | Error frequency |
| `test` | PASS/FAIL/ok patterns | Test execution |
| `commit` | `[branch hash]` | Commit frequency |
| `progress` | General activity indicators | Overall activity level |

### 13.3 Client-Facing Quality Proof

The report builder (`engine/report.go`) generates evidence of quality:

| Report Field | Source | Client Value |
|-------------|--------|:---|
| ReportStatus | DONE / DONE_WITH_CONCERNS / BLOCKED | At-a-glance outcome |
| Stories[].EscalationCount | Event log | How many retries were needed |
| Stories[].Duration | STORY_STARTED → STORY_MERGED | Time per deliverable |
| Stories[].PRUrl | STORY_PR_CREATED payload | Verifiable output |
| AgentStats[].WorkCount | Event aggregation | Effort transparency |
| Effort.Summary | Cost estimation | Budget vs. actual |
| Timeline | Event timestamps | Full execution history |
| Attempts[].Outcome | Attempt tracker | Detailed retry narrative |

### 13.4 SLA Framework (Phase 2+)

**SLA Definitions:**

| SLA Metric | Target | Measurement | Breach Action |
|-----------|:---:|:---|:---|
| **Story completion time** | ≤ 2hr (complexity ≤3), ≤8hr (complexity ≤8), ≤24hr (complexity 13) | STORY_STARTED → STORY_MERGED | Auto-escalate to next tier |
| **First-pass success rate** | ≥ 60% | StoriesMerged without escalation / TotalStories | Weekly review, adjust prompt templates |
| **Pipeline uptime** | ≥ 99.5% | Monitor polling loop availability | Alert + auto-restart via systemd |
| **Estimate accuracy** | ≤ 30% variance | Actual hours vs. estimated hours | Recalibrate Fibonacci-to-hours mapping |
| **Proposal response time** | ≤ 24hr from scrape | ScrapedAt → ProposalDraftedAt | Priority queue for high-rank opportunities |

**SLA implementation roadmap:**

```
Phase 1 (now):     Track metrics manually via `vxd metrics`
Phase 2 (M6-12):   Add configurable deadlines to stories
                    Implement auto-escalation on SLA breach
                    Add structured logging (slog) for observability
Phase 3 (M18+):    External alerting (Slack, PagerDuty webhooks)
                    Prometheus metric export
                    Client-facing SLA dashboard
```

### 13.5 Observability Roadmap

**Current state** (what exists vs. what's missing):

| Capability | Status | Gap |
|-----------|:---:|:---|
| Event-driven audit trail | Exists | None — 45+ event types cover all state changes |
| Agent reputation scoring | Exists | No trending over time |
| QA quality scores (1-5) | Exists | No threshold-based alerting |
| Trace normalization | Exists | Not persisted to dedicated store |
| CLI metrics command | Exists | No export format (Prometheus, StatsD) |
| WebSocket dashboard | Exists | No historical view (live only) |
| Structured logging | Missing | Using basic `log` package, not `slog` |
| Health endpoint | Missing | No `/health` or system status API |
| Alert rules engine | Missing | No configurable thresholds or notifications |
| External notifications | Missing | No Slack, PagerDuty, webhook integrations |
| Cost variance alerting | Missing | Estimates exist but no overrun detection |
| Escalation trend analysis | Missing | Count exists but no trend detection |

**Priority implementation order:**
1. Structured logging (`log/slog`) — foundation for everything else
2. Health endpoint — required for Phase 2 systemd/Docker monitoring
3. Story deadline + auto-escalation — core SLA enforcement
4. Slack/webhook notifications — external alerting
5. Prometheus export — metric aggregation for dashboards

---

## 14. Capacity, Disaster Recovery & Evolution

### 14.1 Capacity Planning

#### Current Limits (Phase 1 — Single macOS Machine)

| Resource | Limit | Constraint | Bottleneck Point |
|----------|:---:|:---|:---|
| **Concurrent agents** | ~5-8 | No hard cap; limited by file conflicts in dispatcher | Tmux sessions + CPU |
| **Worktree disk** | ~500MB-2GB per story | Full repo checkout per worktree, shared `.git/objects` | Disk space |
| **SQLite connections** | 1 writer + N readers | WAL mode; single append thread | Not a bottleneck |
| **Event log size** | Unbounded | Append-only, ~1KB per event | ~100MB after 100K events |
| **Agent memory** | ~500MB-2GB per agent | LLM CLI process + workspace | RAM |
| **Total concurrent** | ~3-5 stories | CPU + memory + disk compound | Phase 2 trigger |

#### Scaling Triggers (When to Move to Phase 2)

| Trigger | Threshold | Action |
|---------|:---:|:---|
| Concurrent agents > 5 | CPU > 80% sustained | Move to Docker/SSH runners |
| Worktree disk > 20GB | `df` shows <20% free | Enable `worktree_prune: immediate` |
| Event log > 100MB | FileStore.List() > 5s | Implement log compaction |
| SQLite > 500MB | Query latency > 100ms | Add indexes on foreign keys |
| Agent memory > 8GB total | System swap triggered | Add `max_concurrent_agents` config |

#### Phase 2 Capacity Targets

| Resource | Target | Mechanism |
|----------|:---:|:---|
| Concurrent agents | 20-50 | DockerRunner with memory limits per container |
| Worktree disk | Shared NFS/EBS | Docker volume mounts |
| Event store | PostgreSQL | Partitioned by project, indexed |
| Agent memory | 2GB cap per container | Docker `--memory` flag |

### 14.2 Disaster Recovery

#### Recovery Point Objective (RPO) and Recovery Time Objective (RTO)

| Scenario | RPO | RTO | Current | Target (Phase 2) |
|----------|:---:|:---:|:---:|:---:|
| Process crash | 0 (fsync'd) | <1 min | Checkpoint + 5 scenarios | Same |
| SQLite corruption | Last event | <5 min | Rebuild from events.jsonl | Automated rebuild script |
| Event log disk full | Last successful write | Manual | No protection | Pre-flight disk check |
| VPS dies | Last backup | Depends | No backup | Daily rsync to S3 |
| Git repo corruption | Last push | <10 min | Re-clone from GitHub | Automated re-clone |

#### Backup Strategy (Phase 2)

```
Daily Backup Pipeline
─────────────────────

1. events.jsonl → S3 (incremental append sync)
   RPO: 24 hours (worst case), typically <1 hour

2. store.db → S3 snapshot (after VACUUM)
   RPO: 24 hours

3. vxd.yaml + config → Git (already tracked)
   RPO: 0 (committed)

4. Opportunity/revenue JSONL → S3
   RPO: 24 hours

Recovery:
  vxd restore --from s3://bucket/backup/2026-04-15/
  1. Download events.jsonl
  2. Rebuild SQLite from events (replay all)
  3. Restore config + JSONL files
  4. Verify with vxd preflight
```

#### Failure Modes Not Yet Covered

| Failure | Impact | Mitigation Needed |
|---------|:---|:---|
| Event log append fails (disk full) | Pipeline halts, events lost | Pre-flight disk space check + alert at 90% |
| SQLite schema mismatch after upgrade | Projection queries fail | Schema version table + migration runner |
| Concurrent `vxd resume` on same project | Data corruption | Lock file exists but needs network-aware locking for Phase 2 |
| Tmux server crash (all sessions) | All agents die simultaneously | Checkpoint covers this; add tmux server health check to preflight |

### 14.3 Event Schema Versioning

**Current state:** No version field in event payloads. Payload is `[]byte` (raw JSON). Lenient decoding (missing fields → zero values).

**Problem:** When event payloads evolve (add fields, rename fields, change types), old events in the append-only log can't be distinguished from new ones. Silent data loss on schema evolution.

**Recommended approach: Additive-only schema evolution**

```
Rule 1: NEVER remove or rename payload fields
Rule 2: NEVER change field types
Rule 3: New fields MUST have sensible zero values
Rule 4: Add "schema_version" field to new events (optional, for future migration)

Example — adding priority_score to STORY_CREATED:

  Old event: {"id":"s-001","title":"Add auth","complexity":5}
  New event: {"id":"s-002","title":"Add cache","complexity":3,"priority_score":8}

  Old events decode fine: priority_score = 0 (zero value)
  New code handles both: if priority_score > 0 { use it }
```

**Migration strategy for breaking changes:**

```
1. Add schema_version to new Config (version: "1.1")
2. Write migration function: migrateV1ToV1_1(oldConfig) → newConfig
3. On LoadConfigChain(), detect version and migrate
4. For events: write adapter that enriches old payloads on replay
5. Never modify events.jsonl in place — always forward-compatible
```

### 14.4 Multi-Tenant Isolation Model (Phase 3)

```
┌─────────────────────────────────────────────────────────────┐
│                   MULTI-TENANT ARCHITECTURE                  │
│                                                              │
│  Tenant A                    Tenant B                        │
│  ────────                    ────────                        │
│  ┌──────────────┐            ┌──────────────┐               │
│  │ Event Store A │            │ Event Store B │               │
│  │ (PostgreSQL   │            │ (PostgreSQL   │               │
│  │  schema: a)   │            │  schema: b)   │               │
│  └──────────────┘            └──────────────┘               │
│         │                           │                        │
│         ▼                           ▼                        │
│  ┌──────────────┐            ┌──────────────┐               │
│  │ Agent Pool A  │            │ Agent Pool B  │               │
│  │ (namespace)   │            │ (namespace)   │               │
│  │ CPU: 4 cores  │            │ CPU: 8 cores  │               │
│  │ RAM: 8GB      │            │ RAM: 16GB     │               │
│  └──────────────┘            └──────────────┘               │
│                                                              │
│  Shared Infrastructure                                       │
│  ─────────────────────                                       │
│  ┌──────────────────────────────────────────┐                │
│  │ API Gateway (auth + tenant routing)      │                │
│  │ LLM Client Pool (shared, rate-limited)   │                │
│  │ Monitoring (per-tenant dashboards)       │                │
│  │ Billing (Stripe, per-tenant metering)    │                │
│  └──────────────────────────────────────────┘                │
└─────────────────────────────────────────────────────────────┘
```

**Isolation guarantees:**

| Layer | Isolation Method | Prevents |
|-------|:---|:---|
| Data | PostgreSQL schemas (one per tenant) | Cross-tenant data access |
| Compute | Kubernetes namespaces + resource quotas | Resource starvation |
| Network | Network policies (deny cross-namespace) | Lateral movement |
| LLM | Per-tenant rate limits + quota tracking | API exhaustion by one tenant |
| Git | Separate worktree directories per tenant | File access leaks |
| Secrets | Kubernetes secrets (namespace-scoped) | Key exposure |

### 14.5 API Contract Design (Phase 2)

**Resource model:**

```
REST API (v1)
─────────────

POST   /api/v1/requirements          Submit new requirement
GET    /api/v1/requirements           List all requirements
GET    /api/v1/requirements/:id       Get requirement detail
POST   /api/v1/requirements/:id/resume  Resume paused requirement

GET    /api/v1/stories                List stories (filter by req_id, status)
GET    /api/v1/stories/:id            Get story detail + attempts
POST   /api/v1/stories/:id/approve    Approve story for merge
POST   /api/v1/stories/:id/reject     Reject story

GET    /api/v1/agents                 List active agents
GET    /api/v1/agents/:id/logs        Stream agent log (tail)

GET    /api/v1/metrics                Pipeline metrics
GET    /api/v1/metrics/:req_id        Per-requirement metrics

POST   /api/v1/estimate               Quick cost estimate
GET    /api/v1/projects               List projects
GET    /api/v1/health                 System health check

WebSocket
─────────
WS     /api/v1/ws                     Real-time state updates + events
```

**Authentication:** API key per tenant (Phase 2), OAuth2 (Phase 3).

### 14.6 Licensing & Intellectual Property

| Question | Position | Basis |
|----------|:---|:---|
| **Who owns VXD-generated code?** | Client owns deliverables | VXD is a tool, not an author. Same as hiring a developer. |
| **Anthropic terms on generated code** | Anthropic grants usage rights to API output | Anthropic Terms of Service §4 — output belongs to user |
| **Google AI terms** | Google grants usage rights | Google AI Studio Terms — output belongs to user |
| **Open-source in generated code** | Agent may include OSS snippets | Client responsibility to audit. VXD can flag via QA criteria. |
| **VXD source code license** | Private (VXD), MIT (NXD) | VXD is proprietary; NXD is public |
| **Liability for generated bugs** | Limited to re-execution | Contract should cap liability at project value |

**Recommendations for client contracts:**
1. Include "AI-assisted development" disclosure
2. Specify that client owns all generated code
3. Include clause that client is responsible for code audit before production deployment
4. Limit liability to re-execution or refund of project fee
5. Require client to maintain their own test suite as acceptance gate

### 14.7 Upgrade & Migration Path

**Semantic versioning policy:**

```
vxd v{MAJOR}.{MINOR}.{PATCH}

MAJOR: Breaking changes to event schema, config format, or CLI interface
MINOR: New features, new event types, new config fields (backward-compatible)
PATCH: Bug fixes, performance improvements

Current: v0.x (pre-1.0, breaking changes expected)
Target: v1.0 at Phase 2 launch (stable event schema + config format)
```

**Migration scenarios:**

| From → To | Affected | Migration |
|-----------|:---|:---|
| Config v1.0 → v1.1 | New fields added | Auto-filled from defaults (chain loading handles this) |
| Config v1.x → v2.0 | Fields renamed/removed | `vxd migrate-config` command transforms YAML |
| Event schema v1 → v2 | New payload fields | Additive-only; old events decode with zero values |
| SQLite schema change | New columns | `ALTER TABLE ADD COLUMN` (already done inline) |
| Binary upgrade | CLI commands | `go build -o ~/.local/bin/vxd ./cmd/vxd` (overwrite) |
| Phase 1 → Phase 2 | State directory | `vxd export --project <name>` → PostgreSQL import |

**Phase 1 → Phase 2 migration checklist:**

1. Export event log: `cp events.jsonl /backup/`
2. Install PostgreSQL, create database
3. Run `vxd migrate --to postgres --connection "postgres://..."`
4. Update `vxd.yaml`: `workspace.backend: postgres`
5. Verify: `vxd preflight`
6. Switch to DockerRunner: update `runtimes` config
7. Enable systemd service for `vxd-improve`
8. Configure Nginx reverse proxy for dashboard

### 14.8 Secrets Manager Integration (Phase 2)

#### Recommended: HashiCorp Vault

| Criteria | Vault | AWS Secrets Manager | 1Password CLI | Doppler |
|----------|:---:|:---:|:---:|:---:|
| **Self-hosted option** | Yes | No (AWS only) | No (cloud) | No (cloud) |
| **Free tier** | Open source | $0.40/secret/mo | $7.99/mo | Free (3 envs) |
| **Go SDK** | Official (`vault/api`) | AWS SDK | Connect SDK | REST API |
| **Secret rotation** | Built-in | Built-in | Manual | Built-in |
| **Dynamic secrets** | Yes (DB creds, tokens) | No | No | No |
| **Local dev** | `vault server -dev` | LocalStack | CLI | CLI |
| **CI/CD integration** | Native (GHA, GitLab) | Native (AWS) | GHA action | Native |
| **Audit log** | Yes | CloudTrail | Yes | Yes |

#### Why Vault

1. **Self-hosted** — VXD processes client code; keeping secrets infrastructure on-prem matters for trust
2. **Dynamic secrets** — generate short-lived DB credentials per agent session (Phase 3 PostgreSQL)
3. **Go SDK** — `github.com/hashicorp/vault/api` integrates directly into VXD startup
4. **Dev mode** — `vault server -dev` for local testing, zero cloud dependency
5. **Free** — open source core, no per-secret pricing. Enterprise features available if needed later
6. **Lease/TTL model** — per-agent tokens auto-expire, reducing blast radius of compromise

#### Integration Pattern

```
Phase 2 Secret Flow:

  vxd startup
    └─▶ vault.Logical().Read("secret/data/vxd")
        └─▶ inject ANTHROPIC_API_KEY, GOOGLE_API_KEY, RESEND_API_KEY, GITHUB_TOKEN

  Per-agent spawn:
    └─▶ vault.Auth().Token().CreateOrphan(TTL=1h)
        └─▶ short-lived token → agent env → auto-expires after story completes

  Secret types:
    static:  API keys (Anthropic, Google, Resend, GitHub)
    dynamic: per-agent DB credentials (Phase 3, PostgreSQL)
    transit: encryption keys for proposal/feedback JSONL at rest

  Config (vxd.yaml):
    secrets:
      provider: vault          # vault | env | doppler
      vault_addr: http://127.0.0.1:8200
      vault_path: secret/data/vxd
      vault_token_file: ~/.vault-token  # or VAULT_TOKEN env var
```

#### Alternative: Doppler (SaaS-first, simpler)

If self-hosting is not required, **Doppler** is the simpler choice:
- Free tier covers 3 environments (dev, staging, prod)
- `doppler run -- vxd req "..."` injects secrets automatically, zero code changes
- No infrastructure to manage — SaaS handles rotation, access control, audit
- Good CI/CD integration (GitHub Actions, GitLab, CircleCI)
- **Trade-off:** secrets leave your infrastructure; not ideal for sensitive client work

#### Migration Path from Environment Variables

```
Phase 1 (now):    Secrets in shell env vars (manual, unrotated, no audit)
                  └─▶ Risk: keys persist in shell history, tmux env, crash dumps

Phase 2 (M6):     Vault dev server, static secrets
                  └─▶ vxd reads at startup via Go SDK
                  └─▶ Per-agent tokens with 1h TTL
                  └─▶ Audit log of every secret access

Phase 2 (M9):     Vault production (systemd, TLS, auto-unseal)
                  └─▶ Dynamic DB credentials (if PostgreSQL migration happens)
                  └─▶ Transit encryption for JSONL files at rest

Phase 3 (M18):    Vault HA cluster (Raft storage, 3-node)
                  └─▶ Multi-tenant secret isolation (per-client namespaces)
                  └─▶ AppRole auth for CI/CD pipelines
                  └─▶ Sentinel policies for access governance
```

#### Quick Start (Phase 2 Day 1)

```bash
# Install Vault
brew install vault

# Start dev server (in-memory, auto-unsealed, root token = "dev-token")
vault server -dev &

# Store VXD secrets
export VAULT_ADDR=http://127.0.0.1:8200
export VAULT_TOKEN=dev-token

vault kv put secret/vxd \
  ANTHROPIC_API_KEY="sk-ant-..." \
  GOOGLE_API_KEY="AIza..." \
  GITHUB_TOKEN="ghp_..." \
  RESEND_API_KEY="re_..."

# Verify
vault kv get secret/vxd

# Add to vxd.yaml
# secrets:
#   provider: vault
#   vault_addr: http://127.0.0.1:8200
```
