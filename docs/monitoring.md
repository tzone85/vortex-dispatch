# Monitoring and Intervention

VXD includes three monitoring systems that keep the pipeline running without human intervention: the **Watchdog**, the **Supervisor**, and the **TUI Dashboard**. This guide explains how each works and when you need to step in.

## Watchdog

The Watchdog monitors individual agent sessions in real time. It runs continuously while agents are executing.

### How It Works

Every `poll_interval_ms` (default: 10 seconds), the Watchdog:
1. Captures the last N lines of output from each tmux session
2. Computes a SHA-256 fingerprint of the output
3. Compares the fingerprint to the previous check

### Automatic Actions

| Condition | Detection | Action | Event |
|-----------|-----------|--------|-------|
| Permission prompt | `permission_pattern` regex matches | Sends "Y" to session | (none) |
| Plan mode | `plan_mode_pattern` regex matches | Sends Escape key | (none) |
| Agent stuck | Fingerprint unchanged for `stuck_threshold_s` | Emits stuck event | `AGENT_STUCK` |
| Agent done | `idle_pattern` regex matches | Marks story complete | `STORY_COMPLETED` |

### Configuration

```yaml
monitor:
  poll_interval_ms: 10000       # Check frequency
  stuck_threshold_s: 120        # Seconds of unchanged output = stuck
  context_freshness_tokens: 150000  # Token limit warning
```

### Detection Patterns

Each runtime defines regex patterns for status detection:

```yaml
runtimes:
  claude-code:
    detection:
      idle_pattern: "^\\$\\s*$"         # Shell prompt = done
      permission_pattern: "\\[Y/n\\]"   # Permission request
      plan_mode_pattern: "Plan mode"    # Claude entered plan mode
```

These patterns are compiled at startup. If an agent enters an unexpected state, adjust the patterns to match your runtime's output format.

### Tuning for Different Models

| Model Speed | Recommended `stuck_threshold_s` |
|-------------|-------------------------------|
| Fast (Haiku, GPT-4o-mini) | 60-90s |
| Medium (Sonnet) | 120-180s |
| Slow (Opus, complex stories) | 180-300s |

## Supervisor

The Supervisor provides periodic high-level oversight across all stories in a requirement.

### How It Works

The Supervisor is an LLM-powered agent (Sonnet by default) that reviews:
- The original requirement
- Current status of all stories
- Progress so far

It produces a structured assessment:

```
{
  "on_track": true/false,
  "concerns": ["list of concerns"],
  "reprioritize": ["story IDs to reprioritize"]
}
```

### Events

| Outcome | Event |
|---------|-------|
| Everything on track | `SUPERVISOR_CHECK` |
| Drift detected | `SUPERVISOR_DRIFT_DETECTED` |
| Reprioritization needed | `SUPERVISOR_REPRIORITIZE` |

### When Drift Is Detected

If the Supervisor determines stories are drifting from the original requirement, VXD:
1. Emits `SUPERVISOR_DRIFT_DETECTED` with details
2. Logs concerns for visibility
3. May reprioritize remaining stories

You can view supervisor findings via:

```bash
vxd events --type SUPERVISOR_DRIFT_DETECTED
```

## Escalations

When an agent repeatedly fails (stuck, review rejected, QA failures), VXD creates an escalation.

### Escalation Triggers

| Trigger | Threshold | Action |
|---------|-----------|--------|
| Execution failures | `max_retries_before_escalation` (default: 2) | Escalate to next tier |
| QA failures | `max_qa_failures_before_escalation` (default: 3) | Escalate to next tier |
| Agent stuck | After stuck detection + retry | Escalate to next tier |

### Escalation Path

```
Junior ──► Intermediate ──► Senior ──► Manual intervention
```

If a Senior agent fails repeatedly, the escalation is marked as unresolved and requires human attention.

### Viewing Escalations

```bash
# List all escalations
vxd escalations

# Example output:
#   ESC-001  STORY-003  junior-01  "Agent stuck after 2 retries"  unresolved
#   ESC-002  STORY-005  inter-02   "QA failed 3 times"            resolved
```

Escalations also appear in the Dashboard's Escalation panel.

## Dashboard

VXD ships two dashboard modes: a terminal UI (TUI) and a browser-based web dashboard. Both show the same five sections and refresh every 2 seconds.

### TUI Dashboard

```bash
vxd dashboard
```

The TUI is a single-pane layout — all five sections are visible simultaneously without switching tabs.

```
┌─ Agents ────────────────────────────────────────────────────────┐
│ junior-01  STORY-001  working    senior-01  (idle)  idle        │
│ junior-02  STORY-002  working                                    │
└─────────────────────────────────────────────────────────────────┘
Pipeline: REQ-01HXYZ  ████████████░░░░░░░░  8/13 stories  in_progress
┌─ Stories ───────────────────────────────────────────────────────┐
│ STORY-001  [2]  Add /healthz route           in_progress        │
│ STORY-002  [3]  Implement uptime tracking    in_progress        │
│ STORY-003  [2]  Add integration tests        blocked            │
│ ...                                                             │
└─────────────────────────────────────────────────────────────────┘
┌─ Activity ──────────────────────────────────────────────────────┐
│ 14:05  STORY_REVIEW_PASSED   STORY-001                         │
│ 14:04  STORY_COMPLETED       STORY-002                         │
│ 14:02  AGENT_SPAWNED         junior-01                         │
└─────────────────────────────────────────────────────────────────┘
┌─ Escalations ───────────────────────────────────────────────────┐
│ (none)                                                          │
└─────────────────────────────────────────────────────────────────┘
```

#### TUI Controls

| Key | Action |
|-----|--------|
| `j` / `k` | Scroll the stories table down / up |
| `w` | Open the web dashboard in your browser |
| `q` / `Ctrl+C` | Quit |

### Web Dashboard

```bash
vxd dashboard --web
vxd dashboard --web --port 9090   # custom port (default: 8787)
```

Opens a browser-based dashboard at `http://localhost:8787`. Binds to localhost only — no external access.

#### Sections

The web dashboard mirrors the TUI: agents, a pipeline summary bar with progress, a stories table, an activity log, and an escalations panel (collapsible).

#### Available Commands

From the web dashboard you can issue commands directly to the running pipeline:

| Command | Description |
|---------|-------------|
| Pause | Pause requirement intake — no new stories are dispatched |
| Resume | Resume a paused requirement |
| Retry | Retry a failed or stuck story with the same agent |
| Reassign | Reassign a story to a different agent tier |
| Escalate | Manually escalate a story to the next agent tier |
| Kill agent | Terminate an agent's tmux session |
| Edit story | Update a story's description before re-dispatch |

Destructive actions (kill, reassign, edit) show a confirmation dialog before executing.

Command results appear as toast notifications.

#### WebSocket Protocol

The web dashboard connects to VXD over WebSocket on the same port. Three message types are used:

- **State broadcast** — full dashboard snapshot sent every 2 seconds to all connected clients
- **Event push** — individual events pushed immediately when they are emitted (e.g., `STORY_COMPLETED`)
- **Command / result** — client sends a JSON command object; server replies with a result message

The client reconnects automatically on disconnect with exponential backoff.

#### Implementation Notes

- Vanilla HTML, CSS, and JavaScript — no external dependencies or build step
- Dark theme
- No authentication in v1 — restrict network access at the OS level if needed
- Empty states are shown for each section when there is no data yet

## CLI Monitoring Commands

For quick checks without the full dashboard:

```bash
# Current status of all requirements and stories
vxd status

# Status of a specific requirement
vxd status --req REQ-01HXYZ

# List all agents, optionally filtered
vxd agents
vxd agents --status working
vxd agents --status stuck

# Recent events (newest first)
vxd events --limit 20

# Events of a specific type
vxd events --type AGENT_STUCK

# Events for a specific story
vxd events --story STORY-001

# All escalations
vxd escalations
```

## When to Intervene Manually

VXD is designed to run autonomously, but some situations require human attention:

| Signal | What to do |
|--------|-----------|
| Senior escalation (unresolved) | Review the story requirements — they may be ambiguous or infeasible |
| Repeated QA failures across stories | Check if lint/build/test commands are correct in config |
| Supervisor drift detected | Review the original requirement and story decomposition |
| Agent stuck with high `stuck_threshold_s` | Check if the runtime CLI is responsive (`tmux attach -t <session>`) |
| No progress after `vxd resume` | Verify API keys are set and runtime CLIs are installed |
