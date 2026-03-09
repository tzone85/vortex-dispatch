# VXD (Vortex Dispatch) — Architecture Design

**Date:** 2026-03-09
**Status:** Approved
**Repo:** github.com/tzone85/vortex-dispatch

---

## 1. Overview

VXD is a Go-based CLI tool that orchestrates autonomous AI agents to build software end-to-end with minimal human intervention. It combines event-sourced state management, a full agile team hierarchy, pluggable CLI runtimes, and version-controlled state via Dolt.

**Core philosophy:** Hand off a requirement, walk away, come back to merged PRs.

### Research Lineage

| Source Repo | Key Adoption |
|-------------|-------------|
| Gastown (steveyegge) | Git-backed persistence, runtime abstraction, convoy/formula system |
| Beads (steveyegge) | Hash-based task IDs, dependency-aware graph, memory decay |
| Dolt (dolthub) | Version-controlled SQL state, branch-per-agent, row-level diffing |
| Hungry Ghost Hive (nikrich) | Agile team hierarchy, complexity routing, micromanager daemon |
| Wasteland (gastownhall) | Reputation scoring, embedded web UI, tiered cleanup |

---

## 2. Decisions

| Decision | Choice |
|----------|--------|
| Primary user | Solo developer |
| Agent execution | Hybrid (API planning + CLI worker sessions) |
| AI runtimes | Claude Code + Codex + Gemini CLI |
| State backend | Dolt (primary) + SQLite (fallback) |
| Agent hierarchy | Full agile team (Tech Lead, Senior, Intermediate, Junior, QA) |
| Cleanup strategy | Tiered (immediate worktree prune, Dolt-archived logs, branch GC) |
| Language | Go 1.23+ |
| License | Apache 2.0 |

---

## 3. Core Architecture

Components: CLI Layer (cobra), API Server (gin/chi), TUI Dashboard (bubbletea), Embedded Web UI (React SPA), Orchestrator Engine (Planner, Dispatcher, Monitor, Reaper), Agent Runtime Layer (Claude Code, Codex, Gemini CLI - pluggable), State Layer (Event Store append-only + Projection Store Dolt/SQLite), Infrastructure Layer (tmux, git, GitHub API, worktrees).

### Component Responsibilities

| Component | Responsibility | Runs As |
|-----------|---------------|---------|
| Planner | Decompose requirements into stories, build dependency graph, assign complexity | Opus/Sonnet API calls |
| Dispatcher | Bundle stories into convoys, route by complexity, spawn agents in worktrees | Go process |
| Monitor (Watchdog) | Stuck detection via screen fingerprinting, permission bypass, context freshness | Go goroutine (continuous) |
| Monitor (Supervisor) | Periodic LLM check-in: drift detection, reprioritization | Sonnet API call (periodic) |
| Reaper | Tiered cleanup: prune worktrees, archive logs, GC branches | Go process (triggered) |
| Reviewer | Senior code review of agent diffs via API | Sonnet API call |
| QA | Run lint/build/test, analyze results, approve/reject | Sonnet API + shell |
| Merger | Create PRs, auto-merge, trigger next dispatch wave | Go process |

---

## 4. Agent Hierarchy & Task Routing

### Roles

| Role | Model Tier | Runs As | Responsibility |
|------|-----------|---------|---------------|
| Tech Lead | Opus | API call | Decompose requirements, create stories, manage cross-story deps |
| Senior | Sonnet | API call (review) / CLI session (complex stories) | Estimate, delegate, review, handle 6+ complexity |
| Intermediate | Haiku/Sonnet | CLI session (tmux + worktree) | Implement 4-5 complexity stories |
| Junior | Haiku/GPT-4o-mini | CLI session (tmux + worktree) | Implement 1-3 complexity stories |
| QA | Sonnet | API call + shell | Lint, build, test, approve/reject |
| Supervisor | Sonnet | API call (periodic) | Progress review, drift detection, reprioritization |

### Complexity Routing (Fibonacci)

- Score 1-3: Junior
- Score 4-5: Intermediate
- Score 6-8: Senior (own CLI session)
- Score 9-13: Senior decomposes further before assigning

### Escalation Flow

Junior stuck (2 retries) -> Senior -> Tech Lead -> Human

---

## 5. Event Store & State Model

### Event Structure

```go
type Event struct {
    ID        string    // ULID (time-sortable, unique)
    Type      string    // e.g., "STORY_CREATED"
    Timestamp time.Time
    AgentID   string
    StoryID   string
    Payload   []byte    // JSON blob
}
```

### Event Types

REQUIREMENT: REQ_SUBMITTED, REQ_ANALYZED, REQ_PLANNED, REQ_COMPLETED
STORY: STORY_CREATED, STORY_ESTIMATED, STORY_ASSIGNED, STORY_STARTED, STORY_PROGRESS, STORY_COMPLETED, STORY_REVIEW_REQUESTED, STORY_REVIEW_PASSED, STORY_REVIEW_FAILED, STORY_QA_STARTED, STORY_QA_PASSED, STORY_QA_FAILED, STORY_PR_CREATED, STORY_MERGED
AGENT: AGENT_SPAWNED, AGENT_CHECKPOINT, AGENT_RESUMED, AGENT_STUCK, AGENT_TERMINATED
ESCALATION: ESCALATION_CREATED, ESCALATION_RESOLVED
SUPERVISOR: SUPERVISOR_CHECK, SUPERVISOR_REPRIORITIZE, SUPERVISOR_DRIFT_DETECTED
CLEANUP: WORKTREE_PRUNED, BRANCH_DELETED, GC_COMPLETED

### Projection Tables (Dolt / SQLite)

requirements, stories, agents, escalations, story_deps, agent_scores, events

### Dolt-Specific Features

- Branch per requirement for state isolation
- dolt_diff for supervisor progress checks
- DOLT_REVERT for instant rollback
- dolt_blame for agent attribution

---

## 6. Runtime Abstraction

Config-driven runtime registration. Runtime interface: Spawn, Resume, Terminate, SendInput, ReadOutput, DetectStatus, DetectCompletion, Name, SupportedModels.

Adding a new CLI tool requires only a YAML block with command, args, and detection regex patterns.

Tmux session naming: vxd-{req-id}-{role}-{team}-{n}
Git worktree path: ~/.vxd/worktrees/vxd-{req-id}-{role}-{team}-{n}/

---

## 7. Workflow Pipeline

Phase 1: INTAKE - vxd req "..." emits REQ_SUBMITTED
Phase 2: PLANNING - Tech Lead (API) creates stories + deps + estimates
Phase 3: DISPATCH - Topo sort, convoy bundling, spawn agents
Phase 4: EXECUTION - CLI sessions in worktrees, Monitor watchdog running
Phase 5: REVIEW + QA - Senior review (API), QA lint/build/test
Phase 6: MERGE - Create PR, auto-merge, unblock next wave
Phase 7: CLEANUP - Reaper prunes worktrees, archives logs, deferred branch GC

Wave-based parallelism: stories dispatched in topological waves.

---

## 8. Directory Structure

~55 Go files, ~8800 lines estimated. Key packages: cmd/vxd, internal/{cli, engine, agent, runtime, state, graph, git, tmux, llm, config, dashboard, web}, migrations, formulas.

---

## 9. Testing Strategy

- Unit (~150): Graph, routing, events, config, prompts, scoring. Pure Go, table-driven.
- Integration (~30): Event store roundtrip, git worktrees, Dolt branching. Real FS+DB, mock LLM.
- E2E (3-5): Full pipeline with fixture repo, LLM replay client, real tmux.

---

## 10. Distribution

Homebrew, go install, GitHub Releases (cross-compiled), Docker.
CI: GitHub Actions with test+lint+build+release pipeline.
License: Apache 2.0.
