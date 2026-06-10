---
tags: [architecture, runtime, security]
---

# Runtime and Adapters

How an agent's command line is built and executed, with a clean test seam.

## The split
- **Adapter** (`internal/runtime/cli_adapter.go`) — `Prepare(SessionConfig)` is a
  **pure** function that returns a `PreparedExecution` (command string, env,
  setup files). No I/O, so it's fully testable.
- **Runner** (`tmux_runner.go`, `docker_runner.go`, `ssh_runner.go`) — performs
  the actual file writes and process spawn. Swappable backend.

## How the prompt reaches the agent
The system prompt + goal are written to `.vxd-prompts/prompt.txt` and piped via
`cat … | <cli> -p` rather than passed as a shell argument. This avoids shell
escaping of arbitrary prompt content. A project-level `CLAUDE.md` is written into
the worktree to override Claude Code plugins that would hijack agent behavior.

## Input validation (defense)
`internal/runtime/sanitize.go` provides:
- `ValidateModelName` — `^[a-zA-Z0-9._:/-]+$`
- `ValidateSessionName` — `^[a-zA-Z0-9._-]+$`
- `ValidateShellArg` / `QuoteShellArg` — reject/escape shell metacharacters

Story IDs (which become worktree paths and branch names) are validated separately
by `state.ValidateStoryID` — see [[Security Model]].

## Environment hygiene
- `ANTHROPIC_API_KEY` and `CLAUDECODE` are unset for the agent so Claude Code uses
  the Max subscription, not API credits, and to avoid nested-session errors.
- tmux global env is scrubbed of stale keys (`internal/tmux/env.go`).

## Related
- [[Pipeline Flow]] — where execution sits in the sequence.
- [[Configuration]] — runtime selection per role.
