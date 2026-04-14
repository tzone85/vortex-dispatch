# Pre-Flight Validation — `vxd preflight`

## Overview

A centralized environment validation system that catches misconfiguration and missing dependencies before agent spawning begins. Runs implicitly before `vxd req` and `vxd resume` (the two dispatch commands), and explicitly via `vxd preflight` for diagnostics.

## Design Principles

- **Proactive, not reactive** — catch issues in 2 seconds before a 20-minute run, not 15 minutes into it
- **Tiered severity** — CRITICAL blocks execution, WARNING prints but proceeds, INFO shown only in explicit mode
- **Minimal overhead** — implicit checks add < 1 second to command startup
- **Single source of truth** — all checks in one package, shared by all commands

## Severity Levels

| Level | Behavior (implicit) | Behavior (`vxd preflight`) | Example |
|-------|--------------------|-----------------------------|---------|
| CRITICAL | Block execution, print error | Print with ✗ | tmux not installed |
| WARNING | Print warning, proceed | Print with ⚠ | gh not authenticated |
| INFO | Not shown | Print with ℹ | Config: vxd.yaml (repo) |

## Check List

### CRITICAL (blocks execution)

| Check | What it validates | Pass message | Fail message |
|-------|-------------------|-------------|-------------|
| `CheckTmux` | `tmux` binary on PATH | "tmux installed (version)" | "tmux not found on PATH — install with: brew install tmux" |
| `CheckClaudeCLI` | `claude` binary on PATH | "claude CLI installed (path)" | "claude CLI not found — install from https://claude.ai/download" |
| `CheckGitRepo` | Current dir is a git repo with commits | "Git repo valid (project-name)" | "Not in a git repository" or "Git repo has no commits" |
| `CheckLLMAvailable` | At least one LLM path exists (API key or CLI) | "LLM available (sources)" | "No LLM available — set ANTHROPIC_API_KEY or install claude CLI" |

### WARNING (prints but proceeds)

| Check | What it validates | Pass message | Fail message |
|-------|-------------------|-------------|-------------|
| `CheckGHCLI` | `gh` on PATH and authenticated | "gh CLI authenticated (username)" | "gh CLI not authenticated — PR auto-merge disabled" |
| `CheckNetwork` | Single DNS lookup succeeds | "Network connectivity (DNS OK)" | "Network unavailable — LLM API calls may fail" |
| `CheckStaleSessions` | No orphaned `vxd-*` tmux sessions | "No stale tmux sessions" | "N stale tmux sessions found (vxd-*) — run: tmux kill-server to clean up" |
| `CheckGoogleAPIKey` | `GOOGLE_AI_API_KEY` env var set | "GOOGLE_AI_API_KEY set" | "GOOGLE_AI_API_KEY not set — Gemma execution roles unavailable" |

### INFO (shown only in `vxd preflight`)

| Check | What it validates | Message |
|-------|-------------------|---------|
| `CheckConfig` | Config loads and validates | "Config: vxd.yaml (repo)" or "Config: defaults (no file found)" |
| `CheckProject` | Project name resolves | "Project: acme-corp (auto-detected from git)" |
| `CheckStateDir` | `~/.vxd/projects/` writable | "State dir: ~/.vxd/projects/project-name" |
| `CheckBillingConfig` | Billing has valid rate | "Billing: $150/hr USD" |

## Core Types

```go
type Severity int

const (
    SeverityCritical Severity = iota
    SeverityWarning
    SeverityInfo
)

type Result struct {
    Name     string
    Severity Severity
    Passed   bool
    Message  string
}

type Check func() Result

type Report struct {
    Results     []Result
    HasCritical bool
    HasWarning  bool
}
```

## Architecture

### New Files

| File | Responsibility |
|------|---------------|
| `internal/preflight/preflight.go` | Core types (`Severity`, `Result`, `Check`, `Report`), `RunAll()` function |
| `internal/preflight/checks.go` | All 12 check function implementations |
| `internal/preflight/format.go` | `FormatVerbose()` for explicit mode, `FormatCompact()` for implicit mode |
| `internal/preflight/preflight_test.go` | Tests for `RunAll()` and report logic |
| `internal/preflight/checks_test.go` | Tests for individual check functions |
| `internal/cli/preflight.go` | Cobra command registration and `runPreflight()` handler |

### Modified Files

| File | Change |
|------|--------|
| `internal/cli/root.go` | Register `newPreflightCmd()`, add `--skip-preflight` persistent flag |
| `internal/cli/req.go` | Add 6-line preflight block at top of `runReq()` |

## Check Sets

```go
// DispatchChecks — run implicitly before vxd req / vxd resume.
// Only CRITICAL and WARNING checks.
func DispatchChecks() []Check {
    return []Check{
        CheckTmux, CheckClaudeCLI, CheckGitRepo, CheckLLMAvailable,
        CheckGHCLI, CheckNetwork, CheckStaleSessions, CheckGoogleAPIKey,
    }
}

// AllChecks — run by vxd preflight.
// Includes INFO checks for full diagnostic view.
func AllChecks() []Check {
    return append(DispatchChecks(),
        CheckConfig, CheckProject, CheckStateDir, CheckBillingConfig,
    )
}
```

## Runner

```go
func RunAll(checks []Check) Report {
    var report Report
    for _, check := range checks {
        result := check()
        report.Results = append(report.Results, result)
        if !result.Passed {
            switch result.Severity {
            case SeverityCritical:
                report.HasCritical = true
            case SeverityWarning:
                report.HasWarning = true
            }
        }
    }
    return report
}
```

## Output Formats

### Verbose (`vxd preflight`)

```
VXD Pre-Flight Check
─────────────────────

  ✓ tmux installed (3.4)
  ✓ claude CLI installed (/usr/local/bin/claude)
  ✓ Git repo valid (vortex-dispatch)
  ✓ LLM available (Anthropic API + Claude CLI)
  ⚠ gh CLI not authenticated — PR auto-merge disabled
  ✓ Network connectivity (DNS OK)
  ✓ No stale tmux sessions
  ✓ GOOGLE_AI_API_KEY set
  ℹ Config: vxd.yaml (repo)
  ℹ Project: vortex-dispatch (auto-detected from git)
  ℹ State dir: ~/.vxd/projects/vortex-dispatch
  ℹ Billing: $150/hr USD

1 warning. Ready to dispatch (non-critical).
```

### Compact (implicit before `vxd req` / `vxd resume`)

Only prints failures:

```
⚠ Pre-flight: gh CLI not authenticated — PR auto-merge disabled
```

Or for critical failures:

```
✗ Pre-flight: tmux not found on PATH — install with: brew install tmux
✗ Pre-flight: Not in a git repository
Aborting: 2 critical issues must be resolved before dispatching.
```

Prints nothing when all CRITICAL and WARNING checks pass.

## CLI Command

```
vxd preflight              # run all checks, verbose output
vxd preflight --json       # structured JSON output
vxd req --skip-preflight   # bypass checks (persistent flag on root)
```

### `--skip-preflight` Flag

Added as a persistent flag on the root command. When set, `vxd req` and `vxd resume` skip the preflight block entirely. Not recommended for normal use — intended for CI or environments where checks are known to be irrelevant.

## Integration Into Existing Commands

At the top of `runReq()` (and similarly in `runResume()`):

```go
skipPreflight, _ := cmd.Flags().GetBool("skip-preflight")
if !skipPreflight {
    report := preflight.RunAll(preflight.DispatchChecks())
    if report.HasCritical {
        preflight.FormatCompact(cmd.ErrOrStderr(), report)
        return fmt.Errorf("aborting: critical pre-flight issues")
    }
    if report.HasWarning {
        preflight.FormatCompact(cmd.ErrOrStderr(), report)
    }
}
```

## Check Implementation Details

### CheckTmux
Runs `tmux -V` to get version. Passes if exit code 0.

### CheckClaudeCLI
Runs `exec.LookPath("claude")`. Passes if binary found. Reports resolved path.

### CheckGitRepo
Runs `git rev-parse --show-toplevel` and `git rev-parse HEAD`. First checks we're in a repo, second checks it has commits.

### CheckLLMAvailable
Checks three sources: `ANTHROPIC_API_KEY` env var, `claude` CLI on PATH, `GOOGLE_AI_API_KEY` env var. Passes if any one is available. Message lists which sources are available.

### CheckGHCLI
Runs `gh auth status`. Passes if exit code 0. Parses username from output.

### CheckNetwork
Single `net.LookupHost("api.anthropic.com")` call. No retry loop — instant pass/fail. Times out after 3 seconds.

### CheckStaleSessions
Runs `tmux list-sessions -F '#{session_name}'` and filters for `vxd-*` prefix. Warns with count if any found.

### CheckGoogleAPIKey
Checks `os.Getenv("GOOGLE_AI_API_KEY") != ""`.

### CheckConfig
Calls `config.LoadConfigChain()` with standard paths. Reports which config file was loaded or "defaults".

### CheckProject
Calls project resolution logic (git remote detection). Reports project name and detection source.

### CheckStateDir
Checks `~/.vxd/projects/{project}/` exists and is writable. Creates a temp file and removes it.

### CheckBillingConfig
Loads config, checks `Billing.DefaultRate > 0`. Reports rate and currency.

## Out of Scope

- Automatic fix/remediation of failures (e.g., auto-installing tmux)
- Check for Claude CLI authentication status (no reliable CLI flag to test this)
- Check for sufficient disk space
- Integration into `vxd-improve` daily run (already has its own env validation)
