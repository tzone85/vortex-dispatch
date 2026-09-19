# Configuration Reference

VXD is configured via a YAML file (`vxd.yaml`) in your project root. Run `vxd init` to generate one from the example template, or copy `vxd.config.example.yaml` manually.

## Viewing and Validating

```bash
# Pretty-print current config
vxd config show

# Validate config structure and values
vxd config validate
```

## Configuration Sections

### workspace

Controls where VXD stores state and how verbose it is.

```yaml
workspace:
  state_dir: ~/.vxd              # Directory for event log + SQLite DB
  backend: sqlite                 # Projection backend: "sqlite" or "dolt"
  log_level: info                 # Logging: debug, info, warn, error
  log_retention_days: 30          # Auto-cleanup events older than N days
```

| Key | Default | Notes |
|-----|---------|-------|
| `state_dir` | `~/.vxd` | Supports `~` expansion. Created by `vxd init`. |
| `backend` | `sqlite` | SQLite is the active local projection backend. `dolt` remains accepted for legacy configs. |
| `log_level` | `info` | Set to `debug` for troubleshooting agent issues. |
| `log_retention_days` | `30` | Set to `0` to keep events indefinitely. |

### models

Binds each agent role to an LLM provider, model, and token limit. These models are used for VXD's **internal operations** only — planning, code review, and QA. They make direct API calls, so they require an API key (`ANTHROPIC_API_KEY` or `OPENAI_API_KEY`).

> **Important:** These are **not** the models used by the spawned coding agents. The runtimes (Claude Code, Codex, Gemini CLI) authenticate via their own sessions — for Claude Code, that's your Max/Pro subscription via `claude login`. No API key is passed to spawned agents, so they run at no additional cost beyond your subscription. See [Getting Started — Authentication](getting-started.md#authentication) for details.

Defaults shipped by `vxd init` (see `internal/config/loader.go DefaultConfig`):

```yaml
models:
  tech_lead:
    provider: anthropic
    model: claude-opus-4-8
    max_tokens: 16000
  senior:
    provider: anthropic
    model: claude-opus-4-7
    max_tokens: 8000
  intermediate:
    provider: anthropic
    model: claude-haiku-4-5
    max_tokens: 4000
  junior:
    provider: anthropic
    model: claude-haiku-4-5
    max_tokens: 4000
  qa:
    provider: anthropic
    model: claude-opus-4-7
    max_tokens: 8000
  supervisor:
    provider: anthropic
    model: claude-haiku-4-5
    max_tokens: 4000
  manager:
    provider: anthropic
    model: claude-opus-4-7
    max_tokens: 8000
```

Every role defaults to the `anthropic` provider. To put a role on Gemma (Google AI Studio free tier), OpenAI or Codex, change that role's `provider` and `model`; see the cost-optimised and quality-maximised examples below.

| Role | Recommended Model | Why |
|------|-------------------|-----|
| Tech Lead | Opus | Needs deep reasoning for decomposition and dependency analysis |
| Senior | Sonnet | Balances quality and cost for reviews and complex stories |
| Intermediate | Haiku/Sonnet | Medium stories; Haiku for cost savings, Sonnet for quality |
| Junior | Haiku/GPT-4o-mini | Simple stories; cheapest models work well here |
| QA | Sonnet | Needs to understand code quality; Sonnet is the sweet spot |
| Supervisor | — | Not wired into the pipeline yet, so this binding has no effect |

**Tuning tip:** Start with the defaults. If junior agents produce low-quality code that fails review repeatedly, upgrade them to Sonnet. If costs are a concern, move QA or Senior from Opus to Sonnet, or put the execution roles on Gemma.

### routing

Controls how stories are assigned to agent roles based on complexity.

```yaml
routing:
  junior_max_complexity: 3          # Stories with score <= 3 go to Junior
  intermediate_max_complexity: 5    # Stories with score 4-5 go to Intermediate
  max_retries_before_escalation: 2  # Escalate after N failed attempts
  max_qa_failures_before_escalation: 3
```

| Key | Default | Notes |
|-----|---------|-------|
| `junior_max_complexity` | `3` | Stories scoring 1-3 route to Junior |
| `intermediate_max_complexity` | `5` | Stories scoring 4-5 route to Intermediate |
| `max_retries_before_escalation` | `2` | Review/execution failures before escalating to next tier |
| `max_qa_failures_before_escalation` | `3` | QA failures before escalating |

Stories above `intermediate_max_complexity` (6+) always route to **Senior**.

**Tuning tip:** If you see many escalations from Junior to Intermediate, lower `junior_max_complexity` to `2`. If Seniors are underutilized, raise `intermediate_max_complexity` to `8`.

### planning

Controls requirement decomposition and the planning guidance passed into agent prompts.

```yaml
planning:
  sequential_file_patterns:
    - "package.json"
    - "*.config.*"
    - "src/core/*"
  max_story_complexity: 5
  godmode: false
  design_approach: ddd-tdd
```

| Key | Default | Notes |
|-----|---------|-------|
| `sequential_file_patterns` | `package.json`, `*.config.*`, `src/core/*` | Matching files are treated as sequencing hazards when stories are planned. |
| `max_story_complexity` | `5` | Planner target for splitting large work into smaller stories. |
| `godmode` | `false` | Skips CLI LLM permission prompts when enabled; command flags still take precedence. |
| `design_approach` | `ddd-tdd` | Prompt strategy for implementation and review: `ddd-tdd`, `tdd`, or `standard`. |

### monitor

Controls the Watchdog that monitors running agents.

```yaml
monitor:
  poll_interval_ms: 10000         # Check agent output every 10 seconds
  stuck_threshold_s: 600          # Default: 10 minutes (matches loader DefaultConfig)
  context_freshness_tokens: 150000 # Warn when context window approaches limit
```

| Key | Default | Notes |
|-----|---------|-------|
| `poll_interval_ms` | `10000` | Lower values detect issues faster but increase overhead |
| `stuck_threshold_s` | `600` | Lower for fast models, higher for deep-thinking models. AGENT_STUCK is informational only (does not kill the agent). |
| `context_freshness_tokens` | `150000` | Triggers context refresh warning |

**Tuning tip:** Default `600` (10 min) accommodates Sonnet/Opus on complex stories with long file reads. For fast models that should fail fast, lower to 120-180s.

### cleanup

Controls post-merge cleanup behavior.

```yaml
cleanup:
  worktree_prune: immediate       # "immediate" or "deferred"
  branch_retention_days: 7        # Keep merged branches for N days
  log_archive: dolt               # "dolt", "file", or "none"
```

| Key | Default | Notes |
|-----|---------|-------|
| `worktree_prune` | `immediate` | `deferred` keeps worktrees until `vxd gc` |
| `branch_retention_days` | `7` | Set to `0` for immediate branch deletion after merge |
| `log_archive` | `dolt` | Where to archive old events |

**Tuning tip:** Use `deferred` + longer retention if you want to inspect agent work after the fact. Use `immediate` + `0` days for maximum cleanliness.

### merge

Controls PR creation and auto-merge behavior.

```yaml
merge:
  auto_merge: true                # Automatically merge PRs that pass QA
  base_branch: main               # Target branch for PRs
  pr_template: |                  # PR description template
    ## Story: {story_id}
    {description}
    ### Acceptance Criteria
    {acceptance_criteria}
```

| Key | Default | Notes |
|-----|---------|-------|
| `auto_merge` | `true` | Set to `false` for manual merge review |
| `base_branch` | `main` | Change if your default branch is `develop`, `master`, etc. |
| `pr_template` | (see above) | Supports `{story_id}`, `{description}`, `{acceptance_criteria}` placeholders |

**Tuning tip:** Set `auto_merge: false` when getting started so you can manually review the first few PRs. Once you trust the pipeline, enable it.

### qa (completion gate)

Once every story of a requirement is complete (merged, PR submitted, awaiting approval or split), the completion gate verifies the composed mainline before `REQ_COMPLETED` is emitted — architecture.md, step 10, has the verify / auto-fix / `REQ_BLOCKED` flow. `vxd resume <req>` re-runs the gate after an interruption or after fixing a blocked requirement (a blocked requirement is unblocked first with `REQ_RESUMED`, so `vxd status` shows it as `planned` while the gate re-runs). It exits 1 when the requirement is blocked, when the gate reached no verdict — including a signal that interrupts the gate once every story is complete — and when stories remain unfinished with nothing left running (escalated, failed, or waiting on a dependency; see `vxd status`). Detaching from agents that are still running stays exit 0. A gate-only re-run does not regenerate the project documentation; that happened on the pass that completed the stories. With `auto_merge: false` an open PR counts as complete, so the mainline verified is the one without it.

```yaml
qa:
  disable_completion_gate: false   # true = legacy advisory verification; the requirement always completes
  completion_fix_cycles: 2         # auto-fix agent runs on a red mainline before REQ_BLOCKED
  completion_test_timeout_s: 1200  # one test-suite run inside the gate; 0 = the 20-minute default
```

| Key | Default | Notes |
|-----|---------|-------|
| `disable_completion_gate` | `false` | `true` turns the gate off: verification still runs and writes `.vxd-fix-gaps.md`, but the requirement completes regardless |
| `completion_fix_cycles` | `2` (when `0`) | Fix-agent runs before blocking; a negative value verifies once and blocks on red with no auto-fix |
| `completion_test_timeout_s` | `1200` (when `0` or negative) | Bound on one test-suite run inside the gate, in seconds |

A suite that does not finish within `completion_test_timeout_s` is recorded as a critical gap and blocks the requirement **without** dispatching a fix agent (an agent cannot repair a suite that does not finish): make the suite finish, or raise the bound, then `vxd resume <req>` re-runs the gate.

`.vxd-fix-gaps.md` and the fix agent's prompt carry the runner's and compiler's output (truncated; API keys, tokens, private-key blocks, connection-string passwords and env-style secret assignments in known shapes are redacted). The file is added to `.gitignore` automatically — treat it as a local artefact.

### runtimes

Defines the AI CLI tools VXD can use to run agents.

```yaml
runtimes:
  claude-code:
    command: claude
    args: ["--dangerously-skip-permissions"]
    models: ["opus-4", "sonnet-4", "haiku-4"]
    detection:
      idle_pattern: "^\\$\\s*$"
      permission_pattern: "\\[Y/n\\]"
      plan_mode_pattern: "Plan mode"
  codex:
    command: codex
    args: ["--approval-mode", "full-auto"]
    models: ["o3", "o4-mini"]
    detection:
      idle_pattern: "Codex>"
      permission_pattern: "approve|deny"
  gemini:
    command: gemini
    args: ["--sandbox"]
    models: ["gemini-2.5-pro", "gemini-2.5-flash"]
    detection:
      idle_pattern: "gemini>"
      permission_pattern: "Allow|Deny"
```

Each runtime defines:

| Field | Purpose |
|-------|---------|
| `command` | CLI executable name (must be on PATH) |
| `args` | Default arguments passed on every invocation |
| `models` | List of model names this runtime supports |
| `detection.idle_pattern` | Regex matching "agent is done / waiting for input" |
| `detection.permission_pattern` | Regex matching "needs permission approval" |
| `detection.plan_mode_pattern` | Regex matching "entered plan mode" (optional) |

**Adding a new runtime:** Add a new entry under `runtimes:` with the CLI command, arguments, supported models, and detection patterns. VXD will automatically register it at startup.

## Example: Minimal Cost-Optimized Config

```yaml
version: "1.0"
workspace:
  state_dir: ~/.vxd
  backend: sqlite
models:
  tech_lead:
    provider: anthropic
    model: claude-sonnet-4-6    # Sonnet instead of Opus
    max_tokens: 8000
  senior:
    provider: anthropic
    model: claude-sonnet-4-6
    max_tokens: 8000
  intermediate:
    provider: openai
    model: gpt-4o-mini                  # Cheapest for medium work
    max_tokens: 4000
  junior:
    provider: openai
    model: gpt-4o-mini
    max_tokens: 4000
  qa:
    provider: openai
    model: gpt-4o-mini
    max_tokens: 4000
  supervisor:
    provider: openai
    model: gpt-4o-mini
    max_tokens: 4000
routing:
  junior_max_complexity: 5              # More stories go to cheaper agents
  intermediate_max_complexity: 8
merge:
  auto_merge: true
  base_branch: main
```

## Example: Maximum Quality Config

```yaml
version: "1.0"
workspace:
  state_dir: ~/.vxd
  backend: sqlite
  log_level: debug
models:
  tech_lead:
    provider: anthropic
    model: claude-opus-4-8
    max_tokens: 16000
  senior:
    provider: anthropic
    model: claude-opus-4-8       # Opus for reviews too
    max_tokens: 16000
  intermediate:
    provider: anthropic
    model: claude-sonnet-4-6
    max_tokens: 8000
  junior:
    provider: anthropic
    model: claude-sonnet-4-6     # Sonnet even for simple stories
    max_tokens: 8000
  qa:
    provider: anthropic
    model: claude-sonnet-4-6
    max_tokens: 8000
  supervisor:
    provider: anthropic
    model: claude-opus-4-8
    max_tokens: 8000
routing:
  junior_max_complexity: 2              # Only trivial stories go to Junior
  intermediate_max_complexity: 4
  max_retries_before_escalation: 1      # Escalate quickly
monitor:
  stuck_threshold_s: 180                # More patience for complex work
merge:
  auto_merge: false                     # Manual review of all PRs
  base_branch: main
```
