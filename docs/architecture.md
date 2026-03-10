# Architecture

This guide covers VXD's internal architecture for contributors who want to understand, debug, or extend the system.

## Design Principles

1. **Event sourcing** — all state changes are immutable events; current state is derived
2. **Pluggable runtimes** — AI CLI tools are abstracted behind a common interface
3. **Wave-based parallelism** — independent stories execute concurrently within dependency constraints
4. **Isolation** — each agent works in its own git worktree and tmux session

## Package Map

```
cmd/vxd/              Entry point — wires Cobra commands
internal/
  agent/              Role definitions, complexity scoring, prompt templates
  cli/                Cobra command implementations (one file per command)
  config/             YAML config loading, validation, defaults
  dashboard/          Bubbletea TUI (4-panel layout)
  engine/             Core orchestration pipeline
    planner.go        Tech Lead decomposition (LLM)
    dispatcher.go     Wave-based parallel dispatch (DAG)
    watchdog.go       Session monitoring (fingerprinting)
    supervisor.go     Drift detection (LLM)
    reviewer.go       Code review (LLM)
    qa.go             Lint/build/test execution
    merger.go         PR creation and auto-merge (gh CLI)
    reaper.go         Worktree/branch cleanup
  git/                Repository scanning, branch/worktree/GitHub operations
  graph/              Dependency DAG with topological sort
  llm/                LLM client abstraction (Anthropic, OpenAI, Replay)
  runtime/            Pluggable runtime registry (CLIRuntime)
  state/              Event store + SQLite projections
  tmux/               Terminal session management
  web/                (placeholder — future web dashboard)
migrations/           SQLite schema (7 tables)
test/                 E2E tests
```

## Event Sourcing Model

VXD uses event sourcing as its core state management pattern.

### Write Path

```
Engine Component ──► NewEvent() ──► FileStore.Append() ──► events.jsonl
                                         │
                                         ▼
                                  SQLiteStore.Project() ──► SQLite tables
```

Every action in the system creates an `Event`:

```go
type Event struct {
    ID        string    // ULID (time-sortable unique ID)
    Type      EventType // e.g., STORY_CREATED, AGENT_SPAWNED
    Timestamp time.Time
    AgentID   string
    StoryID   string
    Payload   []byte    // JSON — event-specific data
}
```

Events are appended to `events.jsonl` (append-only, never modified) and simultaneously projected into SQLite tables for fast queries.

### Read Path

```
CLI Command ──► SQLiteStore.ListStories() ──► SQLite ──► Response
```

All queries read from SQLite projections, never from the event log directly. The event log is the source of truth; projections are derived and could be rebuilt.

### Event Types (35+)

Events are grouped by lifecycle:

| Group | Events |
|-------|--------|
| Request | `REQ_SUBMITTED`, `REQ_ANALYZED`, `REQ_PLANNED`, `REQ_COMPLETED` |
| Story | `STORY_CREATED`, `STORY_ESTIMATED`, `STORY_ASSIGNED`, `STORY_STARTED`, `STORY_PROGRESS`, `STORY_COMPLETED`, `STORY_REVIEW_REQUESTED`, `STORY_REVIEW_PASSED`, `STORY_REVIEW_FAILED`, `STORY_QA_STARTED`, `STORY_QA_PASSED`, `STORY_QA_FAILED`, `STORY_PR_CREATED`, `STORY_MERGED` |
| Agent | `AGENT_SPAWNED`, `AGENT_CHECKPOINT`, `AGENT_RESUMED`, `AGENT_STUCK`, `AGENT_TERMINATED` |
| Escalation | `ESCALATION_CREATED`, `ESCALATION_RESOLVED` |
| Supervisor | `SUPERVISOR_CHECK`, `SUPERVISOR_REPRIORITIZE`, `SUPERVISOR_DRIFT_DETECTED` |
| Cleanup | `WORKTREE_PRUNED`, `BRANCH_DELETED`, `GC_COMPLETED` |

### Projection Store (SQLite)

7 tables materialized from events:

| Table | Primary Key | Updated By |
|-------|-------------|------------|
| `requirements` | `id` | `REQ_*` events |
| `stories` | `id` | `STORY_*` events |
| `agents` | `id` | `AGENT_*` events |
| `story_deps` | `story_id, depends_on` | `STORY_CREATED` |
| `escalations` | `id` | `ESCALATION_*` events |
| `agent_scores` | `agent_id, story_id` | Score computation |
| `projections` | `event_id` | All events (tracking) |

The `Project(event)` method on `SQLiteStore` contains a switch statement that maps each event type to the appropriate SQL mutations.

## Dependency Graph

The `graph` package implements a directed acyclic graph:

```go
type Graph struct {
    nodes map[string]bool
    edges map[string][]string  // node -> dependencies
}
```

Key operations:
- `AddNode(id)` / `AddEdge(from, to)` — build the graph
- `TopologicalSort()` — returns nodes in dependency order
- `ReadyNodes(completed)` — returns nodes whose dependencies are all in the completed set
- `HasCycle()` — validates the graph is acyclic

The Dispatcher uses `ReadyNodes()` to determine each wave of parallel execution.

## LLM Client Abstraction

```go
type Client interface {
    Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
}
```

Three implementations:
- **AnthropicClient** — calls the Anthropic Messages API
- **OpenAIClient** — calls the OpenAI Chat Completions API
- **ReplayClient** — returns pre-configured responses (for testing)

The `ReplayClient` is essential for testing — it eliminates live API calls from unit and integration tests.

## Runtime Abstraction

```go
type Runtime interface {
    Spawn(cfg SessionConfig) error
    Terminate(sessionID string) error
    SendInput(sessionID string, input string) error
    ReadOutput(sessionID string, lines int) (string, error)
    DetectStatus(sessionID string) (AgentStatus, error)
    Name() string
    SupportedModels() []string
}
```

The `CLIRuntime` implementation delegates to the `tmux` package for session management and uses compiled regex patterns for status detection.

Status detection cycle:

```
ReadOutput() ──► match idle_pattern?      ──► StatusDone
              ──► match permission_pattern? ──► StatusPermissionPrompt
              ──► match plan_mode_pattern?  ──► StatusPlanMode
              ──► fingerprint changed?      ──► StatusWorking
              ──► fingerprint stale?        ──► StatusStuck
```

## Engine Component Interfaces

Each engine component depends on interfaces, not concrete implementations. This enables testing with mocks:

| Component | Interface Dependency | Test Double |
|-----------|---------------------|-------------|
| Planner | `llm.Client` | `ReplayClient` |
| Reviewer | `llm.Client` | `ReplayClient` |
| Supervisor | `llm.Client` | `ReplayClient` |
| QA | `CommandRunner` | `mockRunner` |
| Merger | `GitHubOps` | `mockGitHubOps` |
| Reaper | `GitCleanupOps` | `mockCleanupOps` |

## Data Flow: Full Pipeline

```
1. vxd req "..."
   │
   ├─ CLI parses args
   ├─ loadStores() opens FileStore + SQLiteStore
   ├─ buildLLMClient() creates Anthropic/OpenAI client
   │
   ▼
2. Planner.Plan(requirement, repoDir)
   │
   ├─ Calls Tech Lead LLM with system prompt
   ├─ Parses JSON response into PlannedStory[]
   ├─ Builds dependency Graph
   ├─ Emits: REQ_SUBMITTED, STORY_CREATED (×N), REQ_PLANNED
   │
   ▼
3. Dispatcher.Dispatch(graph, completedStories)
   │
   ├─ graph.ReadyNodes(completed) → wave
   ├─ RouteByComplexity(story.Complexity) → role
   ├─ git.CreateWorktree() + git.CreateBranch()
   ├─ runtime.Spawn(sessionConfig)
   ├─ Emits: AGENT_SPAWNED, STORY_ASSIGNED (per story in wave)
   │
   ▼
4. Watchdog.Monitor(sessions)
   │
   ├─ Loop: ReadOutput → Fingerprint → Compare
   ├─ Auto-actions: approve permissions, escape plan mode
   ├─ Detect completion → Emits: STORY_COMPLETED
   ├─ Detect stuck → Emits: AGENT_STUCK
   │
   ▼
5. Reviewer.Review(storyID, diff)
   │
   ├─ Calls Senior LLM with diff + criteria
   ├─ Parses verdict: approve/request_changes
   ├─ Emits: STORY_REVIEW_PASSED or STORY_REVIEW_FAILED
   │
   ▼
6. QA.Run(storyID, worktreePath)
   │
   ├─ Sequential: lint → build → test
   ├─ Records pass/fail + output + elapsed per check
   ├─ Emits: STORY_QA_STARTED, STORY_QA_PASSED or STORY_QA_FAILED
   │
   ▼
7. Merger.Merge(storyID, repoDir, branch)
   │
   ├─ git.PushBranch()
   ├─ github.CreatePR() → PR URL
   ├─ github.MergePR() (if auto_merge)
   ├─ Emits: STORY_PR_CREATED, STORY_MERGED
   │
   ▼
8. Reaper.Reap(storyID, repoDir, worktreePath, branch)
   │
   ├─ git.DeleteWorktree() (if immediate)
   ├─ git.DeleteBranch() (if past retention)
   ├─ Emits: WORKTREE_PRUNED, BRANCH_DELETED
   │
   ▼
9. Back to step 3 for next wave (if stories remain)
```

## Testing Strategy

| Level | Location | What's Tested |
|-------|----------|---------------|
| Unit | `*_test.go` in each package | Individual functions with mocks |
| Integration | `state/integration_test.go`, `engine/integration_test.go` | Multi-component flows with real stores |
| E2E | `test/e2e_test.go` | Full pipeline with ReplayClient + mock git ops |

Key testing patterns:
- **ReplayClient** for deterministic LLM responses
- **Interface-based mocks** for git and command execution
- **Temp directories** for isolated file/SQLite stores
- **Build tag `e2e`** separates E2E tests from unit tests
