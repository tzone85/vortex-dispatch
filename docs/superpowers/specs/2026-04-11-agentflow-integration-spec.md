# Feature Specification: AgentFlow-Inspired Integration

**Feature Branch**: `feat/agentflow-integration`
**Created**: 2026-04-11
**Status**: Implemented
**Input**: Analysis of shouc/agentflow (~28k lines Python DAG orchestrator) to extract patterns for VXD
**SDD Template**: Retroactively applied from `.specify/templates/spec-template.md`

## User Scenarios & Testing

### User Story 1 - Declarative QA Success Criteria (Priority: P1)

As a project owner, I want to define custom QA checks in `vxd.yaml` so that QA validation matches my project's specific quality gates without modifying Go code.

**Why this priority**: QA is the most common failure point. Hardcoded lint/build/test checks don't cover project-specific requirements (coverage thresholds, output patterns, required files).

**Independent Test**: Configure `qa.success_criteria` in vxd.yaml with `output_contains: "PASS"` and `file_exists: coverage.html`. Run a story — QA should evaluate these criteria alongside standard checks.

**Acceptance Scenarios**:

1. **Given** a vxd.yaml with `qa.success_criteria: [{kind: output_contains, value: "PASS"}]`, **When** a story's agent output contains "PASS", **Then** the criterion passes and QA succeeds.
2. **Given** a criterion `{kind: file_exists, path: coverage.html}`, **When** the file does NOT exist in the worktree, **Then** QA fails with message "file coverage.html not found".
3. **Given** an invalid criterion kind "unknown_check", **When** config validation runs, **Then** `vxd.yaml` validation rejects it with a descriptive error.
4. **Given** no `qa.success_criteria` configured, **When** QA runs, **Then** only standard lint/build/test checks execute (backward compatible).

**Implementation**: `internal/engine/criteria.go`, `internal/config/config.go` (QAConfig), wired in `internal/cli/resume.go`
**Tests**: 16 tests in `criteria_test.go`, config validation, wiring test

---

### User Story 2 - Per-Attempt Tracking (Priority: P1)

As a developer analyzing failures, I want to see the full history of every retry attempt for a story so I can understand what each tier tried and why it failed.

**Why this priority**: Retry count alone provides no insight. Knowing that attempt 1 (junior) failed with "undefined: foo" and attempt 2 (senior) failed with "test timeout" tells a completely different story than "2 retries".

**Independent Test**: Submit a requirement that fails QA, escalates to senior, and eventually passes. Run `vxd report <req-id>` — each attempt should appear with role, outcome, and error detail.

**Acceptance Scenarios**:

1. **Given** a story that started, failed QA, then succeeded on retry, **When** AttemptTracker.ListAttempts is called, **Then** it returns 2 attempts with correct outcomes and error detail.
2. **Given** a story escalated from junior (tier 0) to senior (tier 1), **When** attempts are listed, **Then** each attempt shows the correct tier and role.
3. **Given** a story currently in progress, **When** attempts are listed, **Then** the last attempt has outcome "in_progress".
4. **Given** attempt data exists, **When** `vxd report` renders, **Then** attempt history appears in markdown and HTML for stories with >1 attempt.

**Implementation**: `internal/engine/attempts.go`, wired into `report.go` and `report_render.go`
**Tests**: 6 tests in `attempts_test.go`, wiring test

---

### User Story 3 - Template-Based Prompt Rendering with Retry History (Priority: P2)

As a pipeline operator, I want retry agents to receive structured context about prior failed attempts so they take a different approach.

**Why this priority**: Blind retries waste compute. An agent that knows "attempt 1 failed because TestLogin timed out" can focus on the timeout issue.

**Independent Test**: Trigger a retry. Verify the goal prompt contains "Prior Attempts (LEARN FROM THESE)" with attempt details.

**Acceptance Scenarios**:

1. **Given** a first attempt (no prior feedback), **When** executor builds the goal prompt, **Then** it uses standard `GoalPrompt()` with no attempt history.
2. **Given** a retry with 2 prior failed attempts, **When** executor builds the goal prompt, **Then** it uses `RenderGoalWithAttempts()` with attempt numbers, roles, outcomes, errors.
3. **Given** an invalid Go template, **When** `RenderTemplate` is called, **Then** it returns the raw string as fallback.

**Implementation**: `internal/agent/render.go`, wired in `internal/engine/executor.go`
**Tests**: 7 tests in `render_test.go`, 3 wiring tests

---

### User Story 4 - Adapter/Runner Separation (Priority: P2)

As a platform engineer, I want to decouple command building from execution so VXD can run agents in tmux, Docker, or SSH without changing the core engine.

**Why this priority**: Monolithic `Runtime.Spawn()` mixes pure functions with side effects, making testing hard and preventing alternative execution environments.

**Independent Test**: Call `CLIAdapter.Prepare()` — verify it returns `PreparedExecution` without side effects. Verify executor uses adapter/runner when set, falls back to legacy when not.

**Acceptance Scenarios**:

1. **Given** a valid SessionConfig, **When** CLIAdapter.Prepare() is called, **Then** model is quoted with `%q`, prompt written to SetupFiles, env vars collected.
2. **Given** a malicious model name, **When** Prepare() is called, **Then** it returns an error.
3. **Given** executor has adapter/runner set, **When** spawn() runs, **Then** it uses Prepare() + Run().
4. **Given** executor has NO adapter/runner, **When** spawn() runs, **Then** it falls back to legacy Runtime.Spawn().

**Implementation**: `internal/runtime/adapter.go`, `runner.go`, `cli_adapter.go`, `tmux_runner.go`, wired in executor.go
**Tests**: 15 tests, wiring test

---

### User Story 5 - Trace Normalization (Priority: P2)

As a pipeline operator, I want VXD to parse agent log files for structured progress data visible in `vxd metrics`.

**Why this priority**: `vxd metrics` only knows event-level data. Trace parsing reveals tool usage, error rates, commit activity.

**Independent Test**: Parse a log file with Claude Code output. Verify extraction of tool_call, file_edit, error, test, commit events. Verify false positive filtering.

**Acceptance Scenarios**:

1. **Given** "Read(path=...)" in a log, **When** parsed, **Then** TraceToolCall event extracted.
2. **Given** "errors.go" in a log, **When** parsed, **Then** it is NOT classified as error (false positive filter).
3. **Given** trace data for stories, **When** `vxd metrics` runs, **Then** output includes Agent Activity section.

**Implementation**: `internal/engine/trace.go`, wired into `metrics.go`
**Tests**: 10 tests in `trace_test.go`, metrics integration test

---

### User Story 6 - Docker Runner (Priority: P3)

As a platform engineer, I want to run agents inside Docker containers for stronger isolation.

**Acceptance Scenarios**:

1. **Given** DockerConfig with image, **When** created, **Then** network defaults to "host".
2. **Given** PreparedExecution, **When** Run() builds args, **Then** includes volume mounts, env vars, session name.
3. **Given** SendInput called, **Then** returns error (non-interactive).

**Implementation**: `internal/runtime/docker_runner.go`
**Tests**: 13 tests

---

### User Story 7 - SSH Runner (Priority: P3)

As a platform engineer, I want to run agents on remote machines via SSH for horizontal scaling.

**Acceptance Scenarios**:

1. **Given** SSHConfig with host, **When** created, **Then** remoteDir defaults to "/tmp/vxd-agent".
2. **Given** key file configured, **When** buildSSHCmd runs, **Then** `-i keyfile` included.
3. **Given** SendInput called, **Then** returns error (non-interactive).

**Implementation**: `internal/runtime/ssh_runner.go`
**Tests**: 16 tests

---

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| Adapter/Runner over extending Runtime | Separates pure functions from side effects. Follows AgentFlow's Prepare/Execute pattern. |
| Declarative criteria over custom scripts | YAML config per-project, no code changes. `EvaluateCriteria` is a pure function. |
| Reconstruct attempts from events | Event sourcing principle — attempts are a derived view, not a second source of truth. |
| Go text/template over Jinja2 | Standard library, zero deps, sufficient for prompts. Fallback-on-error prevents pipeline crashes. |
| Trace regex over JSON parsing | Agent tmux output is terminal text, not structured JSON. Regex with false-positive filtering is practical. |

## Cross-Feature Dependencies

```
Declarative Criteria ──> QA Pipeline ──> Monitor (post-execution)
Attempt Tracking ──> Report Builder ──> vxd report
                 ──> Template Prompts ──> Executor (retry path)
Adapter/Runner ──> Executor (spawn) ──> Docker/SSH Runners
Trace Normalization ──> Metrics ──> vxd metrics
```

## Test Coverage Summary

| Feature | Unit Tests | Wiring Tests | Total |
|---------|-----------|--------------|-------|
| Declarative Criteria | 16 | 1 | 17 |
| Attempt Tracking | 6 | 1 | 7 |
| Template Prompts | 7 | 3 | 10 |
| Adapter/Runner | 15 | 1 | 16 |
| Trace Normalization | 10 | 0 | 10 |
| Docker Runner | 13 | 0 | 13 |
| SSH Runner | 16 | 0 | 16 |
| **Total** | **83** | **6** | **89** |
