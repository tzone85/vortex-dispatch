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
  backend: dolt                   # Projection backend: "dolt" or "sqlite"
  log_level: info                 # Logging: debug, info, warn, error
  log_retention_days: 30          # Auto-cleanup events older than N days
```

| Key | Default | Notes |
|-----|---------|-------|
| `state_dir` | `~/.vxd` | Supports `~` expansion. Created by `vxd init`. |
| `backend` | `dolt` | `sqlite` is recommended for local use. `dolt` enables versioned state. |
| `log_level` | `info` | Set to `debug` for troubleshooting agent issues. |
| `log_retention_days` | `30` | Set to `0` to keep events indefinitely. |

### models

Binds each agent role to an LLM provider, model, and token limit. These models are used for VXD's **internal operations** only — planning, code review, and QA. They make direct API calls, so they require an API key (`ANTHROPIC_API_KEY` or `OPENAI_API_KEY`).

> **Important:** These are **not** the models used by the spawned coding agents. The runtimes (Claude Code, Codex, Gemini CLI) authenticate via their own sessions — for Claude Code, that's your Max/Pro subscription via `claude login`. No API key is passed to spawned agents, so they run at no additional cost beyond your subscription. See [Getting Started — Authentication](getting-started.md#authentication) for details.

```yaml
models:
  tech_lead:
    provider: anthropic
    model: claude-opus-4-20250514
    max_tokens: 16000
  senior:
    provider: anthropic
    model: claude-sonnet-4-20250514
    max_tokens: 8000
  intermediate:
    provider: anthropic
    model: claude-haiku-4-5-20251001
    max_tokens: 4000
  junior:
    provider: openai
    model: gpt-4o-mini
    max_tokens: 4000
  qa:
    provider: anthropic
    model: claude-sonnet-4-20250514
    max_tokens: 8000
  supervisor:
    provider: anthropic
    model: claude-sonnet-4-20250514
    max_tokens: 4000
```

| Role | Recommended Model | Why |
|------|-------------------|-----|
| Tech Lead | Opus | Needs deep reasoning for decomposition and dependency analysis |
| Senior | Sonnet | Balances quality and cost for reviews and complex stories |
| Intermediate | Haiku/Sonnet | Medium stories; Haiku for cost savings, Sonnet for quality |
| Junior | Haiku/GPT-4o-mini | Simple stories; cheapest models work well here |
| QA | Sonnet | Needs to understand code quality; Sonnet is the sweet spot |
| Supervisor | Sonnet | Drift detection needs good judgment but not maximum reasoning |

**Tuning tip:** Start with the defaults. If junior agents produce low-quality code that fails review repeatedly, upgrade them to Sonnet. If costs are a concern, move intermediate to Haiku.

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

### monitor

Controls the Watchdog that monitors running agents.

```yaml
monitor:
  poll_interval_ms: 10000         # Check agent output every 10 seconds
  stuck_threshold_s: 120          # Agent is "stuck" if output unchanged for 2 minutes
  context_freshness_tokens: 150000 # Warn when context window approaches limit
```

| Key | Default | Notes |
|-----|---------|-------|
| `poll_interval_ms` | `10000` | Lower values detect issues faster but increase overhead |
| `stuck_threshold_s` | `120` | Increase for slow models or complex stories |
| `context_freshness_tokens` | `150000` | Triggers context refresh warning |

**Tuning tip:** For fast models (Haiku, GPT-4o-mini), a `stuck_threshold_s` of 60-90s works well. For Opus on complex stories, 180-300s may be appropriate.

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
    model: claude-sonnet-4-20250514    # Sonnet instead of Opus
    max_tokens: 8000
  senior:
    provider: anthropic
    model: claude-sonnet-4-20250514
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
    model: claude-opus-4-20250514
    max_tokens: 16000
  senior:
    provider: anthropic
    model: claude-opus-4-20250514       # Opus for reviews too
    max_tokens: 16000
  intermediate:
    provider: anthropic
    model: claude-sonnet-4-20250514
    max_tokens: 8000
  junior:
    provider: anthropic
    model: claude-sonnet-4-20250514     # Sonnet even for simple stories
    max_tokens: 8000
  qa:
    provider: anthropic
    model: claude-sonnet-4-20250514
    max_tokens: 8000
  supervisor:
    provider: anthropic
    model: claude-opus-4-20250514
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
