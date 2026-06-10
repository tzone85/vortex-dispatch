---
tags: [architecture, pipeline]
---

# Pipeline Flow

The path from a requirement to merged code.

```
vxd req "requirement"
  → Tech Lead decomposes into stories (Fibonacci complexity)
  → Dispatcher assigns to agents by tier (junior/intermediate/senior)
  → Executor creates git worktrees + spawns tmux sessions
  → Monitor polls agent status every 10s
  → Agent finishes → post-execution pipeline:
       Code Review (LLM) → QA (lint/build/test + declarative criteria)
       → Merge (rebase → push → PR → squash-merge)
  → Auto-resume: dispatch next wave of ready stories
  → All stories done → requirement complete
```

## Stages and owners
- **Planning** — `internal/engine/planner.go` (`Plan`, `RePlan`). Validates
  LLM-generated story IDs via [[Security Model|state.ValidateStoryID]].
- **Dispatch** — `internal/engine/dispatcher.go` builds the branch `vxd/<id>`
  and routes by complexity/reputation.
- **Execution** — `internal/engine/executor.go` + [[Runtime and Adapters]].
- **Monitoring** — `internal/engine/monitor.go` (the poll loop, ~1800 lines).
- **Post-execution** — review (`reviewer.go`), QA (`qa.go`), merge (`merger.go`).
- **Recovery** — [[Escalation Chain]] and [[Conflict Resolution]].

## Key safety points in the flow
- Story IDs are validated before they become git refs / paths.
- The merge step errors (rather than silently "succeeding") if no PR number is
  returned — preventing dependents from dispatching against unmerged work.
- The post-merge integration fixer runs on a **detached context** so it isn't
  killed by the pipeline returning.

See [[Event Sourcing]] for how each transition is recorded.
