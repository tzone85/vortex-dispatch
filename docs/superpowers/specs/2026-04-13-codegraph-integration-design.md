# Design: Code Review Graph Integration

**Date:** April 13, 2026
**Author:** SFT Engineering
**Status:** Approved
**Epic:** Code Review Quality & Blast-Radius Analysis

---

## Problem

When a VXD agent finishes work, the reviewer and QA pipeline only see the direct diff. They have no structural understanding of how the changed code connects to the rest of the codebase. This means:

- **Reviewer** can't warn about ripple effects (e.g., "you changed function X but its 5 callers may break")
- **QA** runs the full test suite instead of focusing on affected tests
- **Planner** has no dependency data when estimating complexity
- **Agents** don't know the blast radius of their changes before submitting

## Solution

Integrate [code-review-graph](https://github.com/tirth8205/code-review-graph) — a Python tool that builds structural dependency graphs using Tree-sitter, stores them in SQLite, and provides blast-radius analysis.

**Approach:** Hybrid — use code-review-graph CLI as a subprocess for graph building/updating; query the SQLite DB directly from Go for analysis.

---

## Architecture

```
                    ┌─────────────────────────┐
                    │  code-review-graph CLI   │
                    │  (Python subprocess)     │
                    │  build / update          │
                    └────────────┬────────────┘
                                 │ writes
                                 ▼
                    ┌─────────────────────────┐
                    │  .code-review-graph/     │
                    │  graph.db (SQLite)       │
                    └────────────┬────────────┘
                                 │ reads
                                 ▼
                    ┌─────────────────────────┐
                    │  internal/codegraph/     │
                    │  Go package              │
                    │  ├── runner.go  (CLI)    │
                    │  ├── graphdb.go (SQLite) │
                    │  └── analysis.go (logic) │
                    └────────────┬────────────┘
                                 │ used by
                    ┌────────────┼────────────┐
                    │            │            │
                    ▼            ▼            ▼
               Reviewer       QA          RepoLearn
              (extra[1])   (affected    (graph stats
                           tests)       as signals)
```

---

## Package: `internal/codegraph/`

### Types

```go
// GraphInfo holds summary statistics about a code graph.
type GraphInfo struct {
    NodeCount   int
    EdgeCount   int
    FileCount   int
    Languages   []string
    LastUpdated time.Time
    CommitHash  string
}

// ImpactAnalysis is the result of a blast-radius analysis.
type ImpactAnalysis struct {
    RiskScore        float64
    Summary          string
    ChangedFunctions []ChangedNode
    TestGaps         []TestGap
    ReviewPriorities []ChangedNode
    AffectedFiles    []string  // unique file paths from review priorities
}

// ChangedNode is a function/class affected by a change.
type ChangedNode struct {
    Name      string
    FilePath  string
    Kind      string   // Function, Class, Test
    LineStart int
    LineEnd   int
    RiskScore float64
    IsTest    bool
}

// TestGap identifies a function without test coverage.
type TestGap struct {
    Name      string
    FilePath  string
    LineStart int
    LineEnd   int
}
```

### Runner (subprocess wrapper)

```go
// Runner wraps the code-review-graph CLI.
type Runner struct {
    BinPath string // path to code-review-graph binary
}

// Build runs a full graph build for the given repo.
func (r *Runner) Build(ctx context.Context, repoPath string) error

// Update runs an incremental update against a base ref.
func (r *Runner) Update(ctx context.Context, repoPath, baseRef string) error

// DetectChanges runs blast-radius analysis and returns parsed JSON.
func (r *Runner) DetectChanges(ctx context.Context, repoPath, baseRef string) (*ImpactAnalysis, error)

// Status returns graph info for the given repo.
func (r *Runner) Status(ctx context.Context, repoPath string) (*GraphInfo, error)

// Available checks if the code-review-graph binary exists.
func (r *Runner) Available() bool
```

### GraphDB (direct SQLite reads)

```go
// GraphDB reads the code-review-graph SQLite database directly.
type GraphDB struct {
    dbPath string
}

// Open opens the graph database at the given repo path.
func Open(repoPath string) (*GraphDB, error)

// Stats returns node/edge/file counts.
func (g *GraphDB) Stats() (GraphInfo, error)

// CallersOf returns functions that call the given qualified name.
func (g *GraphDB) CallersOf(qualifiedName string) ([]ChangedNode, error)

// TestsFor returns test nodes that reference the given file path.
func (g *GraphDB) TestsFor(filePath string) ([]ChangedNode, error)

// Close closes the database.
func (g *GraphDB) Close() error
```

---

## Integration Points

### 1. Reviewer (`engine/reviewer.go:54`)

**When:** Before calling `Review()` in `postExecutionPipeline` (monitor.go:314)

**How:** Run `DetectChanges` on the worktree with base = story branch point. Format the impact analysis as a markdown section and pass as `extra[1]` to `Review()`.

**Prompt injection format:**
```
## Blast Radius Analysis (risk: 0.6/1.0)
28 changed functions, 16 test gaps, 0 affected flows

### Review Priorities (by risk):
1. setupBareRepo (repolearn_test.go:254-282) — risk 0.6
2. truncateStr (repolearn.go:312-317) — risk 0.5
...

### Test Gaps:
- LearningRepo (repolearn.go:17-21) — no test coverage
- Learning (repolearn.go:24-32) — no test coverage
...
```

### 2. QA (`engine/qa.go:101`)

**When:** Before running tests in `Run()`.

**How:** If `ImpactAnalysis.AffectedFiles` contains test files, log them as "affected tests" for observability. The test command still runs the full suite (safer), but the QA result includes which tests were in the blast radius.

**Not changing test scope** — too risky to skip tests. Instead, add an `AffectedTests` field to `QAResult` for reporting.

### 3. RepoLearn (`repolearn/scanner.go`)

**When:** During Pass 1 (static scan) or as a new Pass 4.

**How:** If `.code-review-graph/graph.db` exists, open it and read stats. Add signals:
- `codegraph_stats` — "Code graph: 2191 nodes, 24176 edges across 241 files"
- `codegraph_languages` — "Graph languages: go, javascript"

### 4. Executor / Agent Prompt (future)

**Deferred.** Could inject graph data into agent prompts so they understand the blast radius before starting work. Not in scope for this iteration.

---

## Graceful Degradation

code-review-graph is an **optional** dependency. If the binary isn't installed or the graph DB doesn't exist:

- `Runner.Available()` returns `false`
- All analysis methods return empty results (not errors)
- Reviewer gets no blast-radius context (existing behavior)
- QA runs normally with no affected-test data
- RepoLearn skips graph stats signal

No feature behind `codegraph` should block the pipeline.

---

## File Plan

| File | Lines (est.) | Purpose |
|------|-------------|---------|
| `internal/codegraph/runner.go` | ~120 | CLI subprocess wrapper |
| `internal/codegraph/graphdb.go` | ~100 | Direct SQLite reader |
| `internal/codegraph/analysis.go` | ~80 | Types + formatting helpers |
| `internal/codegraph/codegraph_test.go` | ~200 | Tests for runner, graphdb, analysis |
| `internal/engine/reviewer.go` | +15 | Pass blast-radius as extra context |
| `internal/engine/qa.go` | +10 | Add AffectedTests to QAResult |
| `internal/engine/monitor.go` | +20 | Load codegraph before review/QA |
| `internal/repolearn/scanner.go` | +15 | Add graph stats signal |
| `internal/engine/wiring_test.go` | +30 | Wiring tests for codegraph features |

**Total new code:** ~400 lines source + ~230 lines test

---

## Testing Strategy

1. **Unit tests:** Runner (mock exec), GraphDB (test fixture SQLite), analysis formatting
2. **Integration test:** Build graph on test repo, verify blast-radius output parsing
3. **Wiring tests:** Verify reviewer receives blast-radius, QA exposes affected tests, repolearn emits graph signal

---

## NXD Port

Same `internal/codegraph/` package. code-review-graph is Python-based and Ollama-independent, so no NXD-specific changes needed. Just adjust the Go module import path.

---

**Created:** April 13, 2026
**Last Updated:** April 13, 2026
