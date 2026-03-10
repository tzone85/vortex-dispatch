# Execution Bridge Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Wire up the missing execution step so `vxd resume` actually creates worktrees, spawns tmux sessions with configured runtimes, monitors agents, and progresses stories through review → QA → merge → next wave.

**Architecture:** Three new components: (1) an `Executor` in `internal/engine/` that takes assignments and spawns real tmux sessions with git worktrees, (2) a `Monitor` loop that polls agents and drives the post-execution pipeline, (3) fixes to dependency storage so wave ordering works. The resume CLI command orchestrates all three.

**Tech Stack:** Go, tmux, git worktrees, pluggable CLI runtimes (Claude Code, Codex, Gemini CLI)

---

## Bug Fixes (Prerequisites)

### Task 1: Store depends_on in STORY_CREATED event payload

The planner creates stories but never stores `depends_on` in the event payload, so the `story_deps` SQLite table is never populated and DAG reconstruction fails.

**Files:**
- Modify: `internal/engine/planner.go` (line ~128-140, event emission loop)
- Modify: `internal/state/sqlite.go` (`projectStoryCreated` function)
- Test: `internal/engine/planner_test.go`
- Test: `internal/state/sqlite_test.go`

**Step 1: Write failing test for depends_on in planner event payload**

In `internal/engine/planner_test.go`, add a test that verifies STORY_CREATED events contain `depends_on`:

```go
func TestPlanner_StoryCreatedEventContainsDependsOn(t *testing.T) {
    // Use replay LLM that returns stories with dependencies
    replayResp := `[
        {"id":"s-001","title":"Base","description":"Base setup","acceptance_criteria":"Done","complexity":2,"depends_on":[]},
        {"id":"s-002","title":"Feature","description":"Build feature","acceptance_criteria":"Works","complexity":3,"depends_on":["s-001"]}
    ]`
    client := llm.NewReplayClient(replayResp)
    es := newTestEventStore()
    ps := newTestProjectionStore(t)
    cfg := testConfig()

    planner := NewPlanner(client, cfg, es, ps)
    _, err := planner.Plan(context.Background(), "REQ-01", "Test req", "/tmp")
    if err != nil {
        t.Fatalf("plan: %v", err)
    }

    events, _ := es.List(state.EventFilter{Type: state.EventStoryCreated})
    for _, evt := range events {
        var payload map[string]any
        json.Unmarshal(evt.Payload, &payload)
        if _, ok := payload["depends_on"]; !ok {
            t.Errorf("STORY_CREATED event for %s missing depends_on", evt.StoryID)
        }
    }
}
```

**Step 2: Run test — expect FAIL**

Run: `go test ./internal/engine/ -run TestPlanner_StoryCreatedEventContainsDependsOn -v`

**Step 3: Fix planner to include depends_on in event payload**

In `internal/engine/planner.go`, add `depends_on` to the story event payload (around line 130):

```go
storyPayload := map[string]any{
    "id":                  s.ID,
    "req_id":              reqID,
    "title":               s.Title,
    "description":         s.Description,
    "acceptance_criteria": string(s.AcceptanceCriteria),
    "complexity":          s.Complexity,
    "depends_on":          s.DependsOn,  // ADD THIS LINE
}
```

**Step 4: Fix SQLite projection to populate story_deps table**

In `internal/state/sqlite.go`, update `projectStoryCreated`:

```go
func (s *SQLiteStore) projectStoryCreated(payload map[string]any) error {
    complexity := payloadInt(payload, "complexity")
    storyID := payloadStr(payload, "id")
    _, err := s.db.Exec(
        `INSERT INTO stories (id, req_id, title, description, complexity, status)
         VALUES (?, ?, ?, ?, ?, 'draft')`,
        storyID,
        payloadStr(payload, "req_id"),
        payloadStr(payload, "title"),
        payloadStr(payload, "description"),
        complexity,
    )
    if err != nil {
        return err
    }

    // Populate story_deps table
    if deps, ok := payload["depends_on"]; ok {
        if depSlice, ok := deps.([]any); ok {
            for _, dep := range depSlice {
                if depStr, ok := dep.(string); ok && depStr != "" {
                    _, err := s.db.Exec(
                        `INSERT OR IGNORE INTO story_deps (story_id, depends_on_id) VALUES (?, ?)`,
                        storyID, depStr,
                    )
                    if err != nil {
                        return fmt.Errorf("insert story dep %s -> %s: %w", storyID, depStr, err)
                    }
                }
            }
        }
    }
    return nil
}
```

**Step 5: Run tests — expect PASS**

Run: `go test ./internal/engine/ -v && go test ./internal/state/ -v`

**Step 6: Commit**

```bash
git add internal/engine/planner.go internal/state/sqlite.go
git commit -m "fix: store depends_on in STORY_CREATED events and populate story_deps table"
```

---

### Task 2: Fix DAG reconstruction in resume command

Replace the broken `rebuildDAGFromEvents` with a query against the `story_deps` table.

**Files:**
- Modify: `internal/state/sqlite.go` (add `ListStoryDeps` method)
- Modify: `internal/cli/resume.go` (`rebuildDAGFromEvents` → `rebuildDAGFromStore`)

**Step 1: Add ListStoryDeps to SQLiteStore**

In `internal/state/sqlite.go`:

```go
// StoryDep represents a dependency edge between stories.
type StoryDep struct {
    StoryID     string
    DependsOnID string
}

// ListStoryDeps returns all dependency edges for stories belonging to the given requirement.
func (s *SQLiteStore) ListStoryDeps(reqID string) ([]StoryDep, error) {
    rows, err := s.db.Query(
        `SELECT sd.story_id, sd.depends_on_id
         FROM story_deps sd
         JOIN stories s ON sd.story_id = s.id
         WHERE s.req_id = ?`, reqID,
    )
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var deps []StoryDep
    for rows.Next() {
        var d StoryDep
        if err := rows.Scan(&d.StoryID, &d.DependsOnID); err != nil {
            return nil, err
        }
        deps = append(deps, d)
    }
    return deps, rows.Err()
}
```

**Step 2: Rewrite rebuildDAGFromEvents in resume.go**

Replace the entire function with:

```go
func rebuildDAG(proj *state.SQLiteStore, reqID string, stories []state.Story) (*graph.DAG, []engine.PlannedStory, error) {
    dag := graph.New()

    planned := make([]engine.PlannedStory, 0, len(stories))
    for _, story := range stories {
        dag.AddNode(story.ID)
        planned = append(planned, engine.PlannedStory{
            ID:          story.ID,
            Title:       story.Title,
            Description: story.Description,
            Complexity:  story.Complexity,
        })
    }

    // Reconstruct edges from story_deps table
    deps, err := proj.ListStoryDeps(reqID)
    if err != nil {
        return nil, nil, fmt.Errorf("list story deps: %w", err)
    }
    for _, dep := range deps {
        dag.AddEdge(dep.StoryID, dep.DependsOnID)
    }

    return dag, planned, nil
}
```

Update the call site in `runResume` to use `rebuildDAG(s.Proj, reqID, stories)` instead of `rebuildDAGFromEvents(s.Events, reqID, stories)`.

**Step 3: Run tests — expect PASS**

Run: `go test ./... -v`

**Step 4: Commit**

```bash
git add internal/state/sqlite.go internal/cli/resume.go
git commit -m "fix: reconstruct DAG from story_deps table instead of broken event parsing"
```

---

## Core Implementation

### Task 3: Create the Executor component

The Executor takes assignments from the Dispatcher and actually creates worktrees, spawns tmux sessions, and emits STORY_STARTED events.

**Files:**
- Create: `internal/engine/executor.go`
- Create: `internal/engine/executor_test.go`

**Step 1: Write failing test**

```go
func TestExecutor_SpawnAssignment(t *testing.T) {
    // Test that Executor creates a worktree, spawns tmux, and emits STORY_STARTED
}
```

**Step 2: Implement Executor**

In `internal/engine/executor.go`:

```go
package engine

import (
    "fmt"
    "path/filepath"

    "github.com/tzone85/vortex-dispatch/internal/agent"
    "github.com/tzone85/vortex-dispatch/internal/config"
    vxdgit "github.com/tzone85/vortex-dispatch/internal/git"
    "github.com/tzone85/vortex-dispatch/internal/runtime"
    "github.com/tzone85/vortex-dispatch/internal/state"
)

// Executor spawns agents for dispatched assignments by creating git worktrees,
// launching tmux sessions with configured runtimes, and emitting lifecycle events.
type Executor struct {
    registry   *runtime.Registry
    config     config.Config
    eventStore state.EventStore
    projStore  state.ProjectionStore
}

// NewExecutor creates an Executor wired to the runtime registry, configuration,
// event store, and projection store.
func NewExecutor(reg *runtime.Registry, cfg config.Config, es state.EventStore, ps state.ProjectionStore) *Executor {
    return &Executor{
        registry:   reg,
        config:     cfg,
        eventStore: es,
        projStore:  ps,
    }
}

// SpawnResult holds the outcome of spawning an agent for one assignment.
type SpawnResult struct {
    Assignment   Assignment
    WorktreePath string
    RuntimeName  string
    Error        error
}

// SpawnAll creates worktrees and launches tmux sessions for each assignment.
// It returns results for every assignment (including failures) so callers
// can decide how to handle partial spawns.
func (e *Executor) SpawnAll(repoDir string, assignments []Assignment, stories map[string]PlannedStory) []SpawnResult {
    results := make([]SpawnResult, 0, len(assignments))

    for _, a := range assignments {
        result := e.spawn(repoDir, a, stories[a.StoryID])
        results = append(results, result)
    }

    return results
}

func (e *Executor) spawn(repoDir string, a Assignment, story PlannedStory) SpawnResult {
    result := SpawnResult{Assignment: a}

    // Determine worktree path
    worktreeBase := filepath.Join(expandHome(e.config.Workspace.StateDir), "worktrees")
    worktreePath := filepath.Join(worktreeBase, a.StoryID)
    result.WorktreePath = worktreePath

    // Create worktree with branch
    if err := vxdgit.CreateWorktree(repoDir, worktreePath, a.Branch); err != nil {
        result.Error = fmt.Errorf("create worktree for %s: %w", a.StoryID, err)
        return result
    }

    // Resolve runtime for this role
    rtName := e.runtimeForRole(a.Role)
    result.RuntimeName = rtName

    rt, err := e.registry.Get(rtName)
    if err != nil {
        result.Error = fmt.Errorf("get runtime %s: %w", rtName, err)
        return result
    }

    // Build the agent goal prompt
    promptCtx := agent.PromptContext{
        StoryID:            a.StoryID,
        StoryTitle:         story.Title,
        StoryDescription:   story.Description,
        AcceptanceCriteria: string(story.AcceptanceCriteria),
        RepoPath:           worktreePath,
        Complexity:         story.Complexity,
    }
    goal := agent.GoalPrompt(a.Role, promptCtx)

    // Resolve model for this role
    modelCfg := a.Role.ModelConfig(e.config.Models)

    // Spawn the runtime session
    if err := rt.Spawn(runtime.SessionConfig{
        SessionName:  a.SessionName,
        WorkDir:      worktreePath,
        Model:        modelCfg.Model,
        Goal:         goal,
        SystemPrompt: agent.SystemPrompt(a.Role, promptCtx),
    }); err != nil {
        result.Error = fmt.Errorf("spawn runtime for %s: %w", a.StoryID, err)
        return result
    }

    // Emit STORY_STARTED event
    startEvt := state.NewEvent(state.EventStoryStarted, a.AgentID, a.StoryID, map[string]any{
        "worktree_path": worktreePath,
        "runtime":       rtName,
        "session_name":  a.SessionName,
        "branch":        a.Branch,
    })
    if err := e.eventStore.Append(startEvt); err != nil {
        result.Error = fmt.Errorf("emit story started: %w", err)
        return result
    }
    if err := e.projStore.Project(startEvt); err != nil {
        result.Error = fmt.Errorf("project story started: %w", err)
        return result
    }

    return result
}

// runtimeForRole returns the configured runtime name for an agent role.
// Defaults to the first available runtime if no specific mapping exists.
func (e *Executor) runtimeForRole(role agent.Role) string {
    // Use CLI runtimes - pick first available
    for name := range e.config.Runtimes {
        return name
    }
    return "claude-code"
}

// expandHome replaces a leading ~ with the user's home directory.
func expandHome(path string) string {
    if len(path) == 0 || path[0] != '~' {
        return path
    }
    home, err := os.UserHomeDir()
    if err != nil {
        return path
    }
    return filepath.Join(home, path[1:])
}
```

Note: The `expandHome` function duplicates `cli/helpers.go`. Move to a shared location or accept the duplication for now.

**Step 3: Add GoalPrompt to agent package**

In `internal/agent/prompts.go`, add a `GoalPrompt` function that builds the goal string sent to the runtime CLI:

```go
// GoalPrompt builds the goal/task description sent to the runtime CLI for a given role.
func GoalPrompt(role Role, ctx PromptContext) string {
    return fmt.Sprintf("Implement story %s: %s\n\nDescription: %s\n\nAcceptance Criteria:\n%s\n\nWork in the current directory. Commit your changes when done.",
        ctx.StoryID, ctx.StoryTitle, ctx.StoryDescription, ctx.AcceptanceCriteria)
}
```

**Step 4: Run tests — expect PASS**

Run: `go test ./internal/engine/ -v && go test ./internal/agent/ -v`

**Step 5: Commit**

```bash
git add internal/engine/executor.go internal/engine/executor_test.go internal/agent/prompts.go
git commit -m "feat: add Executor component to spawn agents with worktrees and tmux sessions"
```

---

### Task 4: Create the Monitor loop

The Monitor polls running agents, detects completion, and drives stories through review → QA → merge.

**Files:**
- Create: `internal/engine/monitor.go`
- Create: `internal/engine/monitor_test.go`

**Step 1: Implement Monitor**

In `internal/engine/monitor.go`:

```go
package engine

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/tzone85/vortex-dispatch/internal/config"
    "github.com/tzone85/vortex-dispatch/internal/runtime"
    "github.com/tzone85/vortex-dispatch/internal/state"
)

// ActiveAgent tracks a running agent session.
type ActiveAgent struct {
    Assignment   Assignment
    WorktreePath string
    RuntimeName  string
}

// Monitor polls running agents and progresses completed stories through
// review → QA → merge. It also triggers the next wave when the current
// wave finishes.
type Monitor struct {
    registry   *runtime.Registry
    watchdog   *Watchdog
    reviewer   *Reviewer
    qa         *QA
    merger     *Merger
    config     config.Config
    eventStore state.EventStore
    projStore  state.ProjectionStore
}

// NewMonitor creates a Monitor wired to all pipeline components.
func NewMonitor(
    reg *runtime.Registry,
    wd *Watchdog,
    rev *Reviewer,
    qa *QA,
    merger *Merger,
    cfg config.Config,
    es state.EventStore,
    ps state.ProjectionStore,
) *Monitor {
    return &Monitor{
        registry:   reg,
        watchdog:   wd,
        reviewer:   rev,
        qa:         qa,
        merger:     merger,
        config:     cfg,
        eventStore: es,
        projStore:  ps,
    }
}

// Run polls active agents at the configured interval until all are done
// or the context is cancelled.
func (m *Monitor) Run(ctx context.Context, agents []ActiveAgent, repoDir string) error {
    pollInterval := time.Duration(m.config.Monitor.PollIntervalMs) * time.Millisecond
    if pollInterval == 0 {
        pollInterval = 10 * time.Second
    }

    ticker := time.NewTicker(pollInterval)
    defer ticker.Stop()

    active := make(map[string]ActiveAgent, len(agents))
    for _, a := range agents {
        active[a.Assignment.SessionName] = a
    }

    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-ticker.C:
            if len(active) == 0 {
                return nil
            }

            for sessionName, ag := range active {
                rt, err := m.registry.Get(ag.RuntimeName)
                if err != nil {
                    continue
                }

                // Watchdog check (handles permission prompts, stuck detection)
                m.watchdog.Check(sessionName, rt)

                // Check if agent is done
                status, err := rt.DetectStatus(sessionName)
                if err != nil {
                    continue
                }

                if status == runtime.StatusDone || status == runtime.StatusTerminated {
                    log.Printf("[monitor] agent %s finished (status: %s)", ag.Assignment.AgentID, status)

                    // Emit story completed
                    completedEvt := state.NewEvent(state.EventStoryCompleted, ag.Assignment.AgentID, ag.Assignment.StoryID, map[string]any{
                        "status": string(status),
                    })
                    m.eventStore.Append(completedEvt)
                    m.projStore.Project(completedEvt)

                    // Drive post-execution pipeline (review → QA → merge)
                    go m.postExecutionPipeline(ctx, ag, repoDir)

                    // Remove from active tracking
                    m.watchdog.ClearFingerprint(sessionName)
                    delete(active, sessionName)
                }
            }
        }
    }
}

// postExecutionPipeline runs review, QA, and merge for a completed story.
func (m *Monitor) postExecutionPipeline(ctx context.Context, ag ActiveAgent, repoDir string) {
    storyID := ag.Assignment.StoryID
    branch := ag.Assignment.Branch

    // 1. Code Review (if reviewer is configured)
    if m.reviewer != nil {
        diff, err := getDiff(ag.WorktreePath)
        if err == nil && diff != "" {
            result, err := m.reviewer.Review(ctx, storyID, ag.Assignment.StoryID, "", diff)
            if err != nil {
                log.Printf("[monitor] review failed for %s: %v", storyID, err)
                return
            }
            if !result.Passed {
                log.Printf("[monitor] review rejected %s: %s", storyID, result.Summary)
                return
            }
        }
    }

    // 2. QA
    if m.qa != nil {
        result, err := m.qa.Run(ctx, storyID, ag.WorktreePath)
        if err != nil {
            log.Printf("[monitor] QA failed for %s: %v", storyID, err)
            return
        }
        if !result.Passed {
            log.Printf("[monitor] QA rejected %s", storyID)
            return
        }
    }

    // 3. Merge
    if m.merger != nil {
        result, err := m.merger.Merge(storyID, storyID, repoDir, branch)
        if err != nil {
            log.Printf("[monitor] merge failed for %s: %v", storyID, err)
            return
        }
        log.Printf("[monitor] %s -> PR #%d (%s) merged=%v", storyID, result.PRNumber, result.PRURL, result.Merged)
    }
}

// getDiff returns the git diff for a worktree.
func getDiff(worktreePath string) (string, error) {
    cmd := exec.CommandContext(context.Background(), "git", "diff", "HEAD~1")
    cmd.Dir = worktreePath
    out, err := cmd.CombinedOutput()
    return string(out), err
}
```

**Step 2: Run tests**

Run: `go test ./internal/engine/ -v`

**Step 3: Commit**

```bash
git add internal/engine/monitor.go internal/engine/monitor_test.go
git commit -m "feat: add Monitor loop for agent polling and post-execution pipeline"
```

---

### Task 5: Wire everything into the resume command

Replace the stub in `internal/cli/resume.go` with the full execution flow.

**Files:**
- Modify: `internal/cli/resume.go`

**Step 1: Rewrite runResume**

```go
func runResume(cmd *cobra.Command, args []string) error {
    reqID := args[0]

    cfgPath, _ := cmd.Flags().GetString("config")
    s, err := loadStores(cfgPath)
    if err != nil {
        return err
    }
    defer s.Close()

    out := cmd.OutOrStdout()

    // Verify the requirement exists
    req, err := s.Proj.GetRequirement(reqID)
    if err != nil {
        return fmt.Errorf("requirement not found: %w", err)
    }
    fmt.Fprintf(out, "Resuming requirement: %s (%s)\n", req.Title, req.Status)

    // Load all stories
    stories, err := s.Proj.ListStories(state.StoryFilter{ReqID: reqID})
    if err != nil {
        return fmt.Errorf("list stories: %w", err)
    }
    if len(stories) == 0 {
        fmt.Fprintf(out, "No stories found.\n")
        return nil
    }

    // Rebuild DAG from story_deps table
    dag, plannedStories, err := rebuildDAG(s.Proj, reqID, stories)
    if err != nil {
        return fmt.Errorf("rebuild DAG: %w", err)
    }

    // Determine completed stories
    completed := make(map[string]bool)
    for _, story := range stories {
        if story.Status == "merged" || story.Status == "pr_submitted" {
            completed[story.ID] = true
        }
    }
    fmt.Fprintf(out, "Stories: %d total, %d completed\n", len(stories), len(completed))

    if len(completed) == len(stories) {
        fmt.Fprintf(out, "All stories are complete.\n")
        return nil
    }

    // Dispatch next wave
    dispatcher := engine.NewDispatcher(s.Config, s.Events, s.Proj)
    assignments, err := dispatcher.DispatchWave(dag, completed, reqID, plannedStories)
    if err != nil {
        return fmt.Errorf("dispatch wave: %w", err)
    }
    if len(assignments) == 0 {
        fmt.Fprintf(out, "No stories ready for dispatch.\n")
        return nil
    }
    fmt.Fprintf(out, "\nWave: dispatching %d stories\n\n", len(assignments))

    // Build story map for executor
    storyMap := make(map[string]engine.PlannedStory, len(plannedStories))
    for _, ps := range plannedStories {
        storyMap[ps.ID] = ps
    }

    // Set up runtime registry
    reg, err := runtime.NewRegistry(s.Config.Runtimes)
    if err != nil {
        return fmt.Errorf("init runtime registry: %w", err)
    }

    // Detect repo path
    repoDir, err := os.Getwd()
    if err != nil {
        return fmt.Errorf("get working directory: %w", err)
    }

    // Spawn agents
    executor := engine.NewExecutor(reg, s.Config, s.Events, s.Proj)
    results := executor.SpawnAll(repoDir, assignments, storyMap)

    activeAgents := make([]engine.ActiveAgent, 0, len(results))
    for _, r := range results {
        if r.Error != nil {
            fmt.Fprintf(out, "  [FAIL] %s: %v\n", r.Assignment.StoryID, r.Error)
            continue
        }
        fmt.Fprintf(out, "  [%s] %s -> %s (session: %s, branch: %s)\n",
            r.Assignment.Role, r.Assignment.StoryID, r.RuntimeName,
            r.Assignment.SessionName, r.Assignment.Branch)
        activeAgents = append(activeAgents, engine.ActiveAgent{
            Assignment:   r.Assignment,
            WorktreePath: r.WorktreePath,
            RuntimeName:  r.RuntimeName,
        })
    }

    if len(activeAgents) == 0 {
        return fmt.Errorf("no agents spawned successfully")
    }

    fmt.Fprintf(out, "\n%d agents working. Monitoring progress...\n", len(activeAgents))
    fmt.Fprintf(out, "Use 'vxd dashboard' in another terminal to watch progress.\n")
    fmt.Fprintf(out, "Press Ctrl+C to detach (agents continue in tmux).\n\n")

    // Build pipeline components
    llmClient, err := buildLLMClient(s.Config)
    if err != nil {
        // Run without review if LLM fails
        log.Printf("Warning: LLM client unavailable, skipping code review: %v", err)
    }

    var reviewer *engine.Reviewer
    if llmClient != nil {
        seniorModel := s.Config.Models.Senior
        reviewer = engine.NewReviewer(llmClient, seniorModel.Model, seniorModel.MaxTokens, s.Events, s.Proj)
    }

    qaRunner := engine.NewQA(engine.QAConfig{}, &engine.ExecRunner{}, s.Events, s.Proj)

    var merger *engine.Merger
    if vxdgit.GHAvailable() {
        merger = engine.NewMerger(s.Config.Merge, &gitHubOpsImpl{repoDir: repoDir}, s.Events, s.Proj)
    }

    watchdog := engine.NewWatchdog(engine.WatchdogConfig{
        StuckThresholdS: s.Config.Monitor.StuckThresholdS,
    }, s.Events)

    // Start monitoring loop
    ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
    defer cancel()

    monitor := engine.NewMonitor(reg, watchdog, reviewer, qaRunner, merger, s.Config, s.Events, s.Proj)
    return monitor.Run(ctx, activeAgents, repoDir)
}
```

**Step 2: Add required imports and helpers**

Add `buildLLMClient` helper (reuse pattern from `req.go`):

```go
func buildLLMClient(cfg config.Config) (llm.Client, error) {
    apiKey := os.Getenv("ANTHROPIC_API_KEY")
    if apiKey != "" {
        return llm.NewAnthropicClient(apiKey), nil
    }
    openaiKey := os.Getenv("OPENAI_API_KEY")
    if openaiKey != "" {
        return llm.NewOpenAIClient(openaiKey), nil
    }
    return nil, fmt.Errorf("no API key found")
}
```

Add `gitHubOpsImpl`:

```go
type gitHubOpsImpl struct {
    repoDir string
}

func (g *gitHubOpsImpl) PushBranch(repoDir, branch string) error {
    return vxdgit.PushBranch(repoDir, branch)
}

func (g *gitHubOpsImpl) CreatePR(repoDir, title, body, baseBranch string) (engine.PRCreationResult, error) {
    pr, err := vxdgit.CreatePR(repoDir, title, body, baseBranch)
    if err != nil {
        return engine.PRCreationResult{}, err
    }
    return engine.PRCreationResult{Number: pr.Number, URL: pr.URL}, nil
}

func (g *gitHubOpsImpl) MergePR(repoDir string, prNumber int) error {
    return vxdgit.MergePR(repoDir, prNumber)
}
```

**Step 3: Run full test suite**

Run: `go test ./... -v`

**Step 4: Build and install**

Run: `make build && make install`

**Step 5: Commit**

```bash
git add internal/cli/resume.go internal/engine/executor.go internal/engine/monitor.go internal/agent/prompts.go
git commit -m "feat: wire execution bridge into resume command — spawn agents, monitor, review, QA, merge"
```

---

### Task 6: Integration test — full resume pipeline

**Files:**
- Modify: `internal/engine/integration_test.go`

**Step 1: Add integration test**

Write a test using replay LLM and mock runtime that verifies the full flow:
plan → dispatch → spawn → complete → review → QA → merge

**Step 2: Run**

Run: `go test ./internal/engine/ -run TestIntegration -v`

**Step 3: Commit**

```bash
git add internal/engine/integration_test.go
git commit -m "test: add integration test for full resume execution pipeline"
```

---

## Summary

| Task | Component | Purpose |
|------|-----------|---------|
| 1 | Planner + SQLite | Store depends_on so DAG reconstruction works |
| 2 | Resume + SQLite | Query story_deps table for proper wave ordering |
| 3 | Executor | Create worktrees and spawn tmux sessions |
| 4 | Monitor | Poll agents, drive review → QA → merge pipeline |
| 5 | Resume CLI | Wire Executor + Monitor into the resume command |
| 6 | Integration test | Verify end-to-end pipeline |

After all tasks: `vxd resume <req-id>` will create worktrees, spawn agents in tmux, monitor them, run code review, QA, create PRs, and auto-merge — the full pipeline.
