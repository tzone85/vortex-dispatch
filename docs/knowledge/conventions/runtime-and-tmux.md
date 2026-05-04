# VXD Runtime / Tmux Conventions

## Runtime resolution
- Each "runtime" in vxd.yaml is a CLI tool (claude, codex, gemini) wrapped by
  internal/runtime.CLIRuntime
- Registry: runtime.NewRegistry(cfg.Runtimes) returns a name-keyed registry
- For autoresearch, pickAutoresearchRuntime picks the alphabetically-first
  runtime (deterministic) — see internal/cli/autoresearch.go

## Spawn lifecycle (CLIRuntime.Spawn → tmux)
1. Write CLAUDE.md unconditionally to the worktree (suppresses brainstorming/
   planning plugins that override -p prompts). This must happen on EVERY spawn,
   not just first worktree creation, because reused worktrees may have stale content.
2. Propagate critical env vars from process to tmux global env via
   tmux.PropagateCriticalEnv() — refreshes long-lived tmux server state.
3. BuildCommand assembles `cmd -p "<prompt>" --max-turns N`, prefixed with
   `unset CLAUDECODE; unset ANTHROPIC_API_KEY;` to prevent nested-session errors
   and to force Max-subscription auth instead of API credits.
4. tmux.CreateSession kills any existing session with the same name first.

## Detecting agent status
CLIRuntime.DetectStatus reads last 20 lines of tmux pane output and matches
against three regex patterns (idle / permission / plan_mode). Returns:
- StatusDone — idle pattern matched (agent finished)
- StatusPermissionPrompt — agent stuck waiting for human
- StatusPlanMode — agent in plan mode
- StatusWorking — none of the above
- StatusTerminated — tmux session no longer exists

## ANTHROPIC_API_KEY is hostile to Claude CLI subscription mode
- When ANTHROPIC_API_KEY is set AND `claude` CLI is installed, the CLI uses API
  credits instead of the user's Max subscription
- VXD removes the key from tmux global env: `tmux set-environment -g -u ANTHROPIC_API_KEY`
- The CLI adapter also unsets it in the command string
- Preflight check warns about this. User fix: `unset ANTHROPIC_API_KEY` before running VXD

## Stdin pipe is broken in detached tmux
- `cat file | claude -p` produces empty output in detached tmux
- `claude -p "$(cat file)"` works
- Multi-line prompts with special chars need careful $(cat) escaping
- VXD uses prompt-via-file: write to .vxd-prompts/prompt.txt, pass via $(cat ...)

## Why prompt-via-file
Shell escaping multi-line prompts with quotes/backticks/dollar signs through
tmux's command layer is a quoting nightmare. Writing to a file and using $(cat)
sidesteps it entirely.

## Auto-commit in autoresearch driver
LiveAgentDriver.RunAgent (internal/autoresearch/driver.go) calls autoCommit()
after the agent exits OR is killed by budget timeout. This ensures the diff
reflects whatever the agent wrote, even if it didn't reach a clean checkpoint.
The runner uses that diff for tripwire/metric; missing auto-commit would mean
killed agents always look like "no_diff" → Bayes loss.
