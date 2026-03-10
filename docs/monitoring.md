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

## TUI Dashboard

The live terminal dashboard gives a real-time view of the entire pipeline.

### Launching

```bash
vxd dashboard
```

### Panels

```
┌─ Pipeline ──────────────────┐┌─ Agents ───────────────────────┐
│ Shows all requirements and  ││ Lists all agents with their    │
│ their stories with current  ││ current role, assigned story,  │
│ status indicators.          ││ and session status.            │
│                             ││                                │
│ REQ-01: planned             ││ junior-01  STORY-001  working  │
│  STORY-001  in_progress     ││ junior-02  STORY-002  working  │
│  STORY-002  review          ││ senior-01  (idle)     idle     │
│  STORY-003  blocked         ││                                │
└─────────────────────────────┘└────────────────────────────────┘
┌─ Activity ──────────────────┐┌─ Escalations ──────────────────┐
│ Live feed of recent events  ││ All escalation events with     │
│ from the event store.       ││ story, agent, reason, and      │
│                             ││ current resolution status.     │
│ 14:05 STORY_REVIEW_PASSED  ││                                │
│ 14:04 STORY_COMPLETED      ││ (none)                         │
│ 14:02 AGENT_SPAWNED        ││                                │
└─────────────────────────────┘└────────────────────────────────┘
```

### Refresh Rate

The dashboard polls stores every 2 seconds. This is not currently configurable (hardcoded in `dashboard/app.go`).

### Controls

| Key | Action |
|-----|--------|
| `q` / `Ctrl+C` | Quit dashboard |
| Arrow keys | Navigate between panels |

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
