# Test Interfaces, Prod Adapters

A pattern used heavily across VXD that pays off in test simplicity.

## The pattern

For any component that touches I/O (git, tmux, network, disk, LLM), define:
1. A small interface declaring just what THIS component needs
2. A "Default*" struct that wraps the real-world dependency
3. Tests inject a fake that satisfies the interface

## Why a *small* interface

Don't reuse the full internal/git or internal/runtime API surface. Define what
you need at the call site:

```go
// In internal/autoresearch/gate.go:
type GateOps interface {
    CreateBranch(repoDir, name string) error
    BranchExists(repoDir, name string) bool
    PushBranch(repoDir, branch string) error
    CreatePR(repoDir, title, body, baseBranch, headBranch string) (string, error)
    MergePR(repoDir, prURL string) error
    RebaseOnto(worktreePath, upstream string) error
    FastForwardWinning(repoDir, sourceBranch, winningBranch string) error
}

type DefaultGateOps struct{}  // wraps internal/git
```

Tests inject a fakeGateOps that records calls. Easy to assert what happened
without spinning up real git.

## Concrete examples in autoresearch

| Interface | File | Real impl | Test fake |
|---|---|---|---|
| GateOps | gate.go | DefaultGateOps → internal/git | fakeGateOps |
| WorktreeOps | runner.go | DefaultWorktreeOps → internal/git | fakeWorktreeOps |
| AgentDriver | runner.go | LiveAgentDriver → internal/runtime | fakeAgentDriver |
| WorkspaceWriter | evolver.go | DefaultWorkspaceWriter → ephemeral worktree | fakeWorkspaceWriter |
| Tiebreaker | metric.go | LLMTiebreaker → internal/llm | fakeTiebreaker |
| PromptBuilder | coordinator.go | SimplePromptBuilder | countingPromptBuilder |
| EventSink | runner.go | state.FileStore | (none — use real store in tests) |

Result: 100% of unit tests run without git, tmux, network, or disk-state
beyond t.TempDir(). The integration test in coordinator_test.go uses the real
event store but fake everything else.

## When NOT to do this

If the dependency is a pure function (parsing, math, string manipulation), don't
wrap it in an interface — just call it directly. The interface tax is only worth
paying for I/O.

## When to inline a fake into the test file

Default to colocating fakes with their tests (e.g. fakeGateOps lives in
gate_test.go, used by gate_test.go and runner_test.go via package-internal
sharing). Don't create separate _test_helpers.go files unless multiple test
files in different packages need them.
