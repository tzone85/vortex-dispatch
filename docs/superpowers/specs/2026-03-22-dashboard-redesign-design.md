# Dashboard Redesign for VXD

**Date:** 2026-03-22
**Status:** Draft
**Author:** Design collaboration

## Problem Statement

The current TUI dashboard uses a tab-based layout that only shows one panel at a time (pipeline, agents, activity, or escalations). Users must switch tabs to see the full picture, losing context. There's no web interface, which intimidates non-terminal users. The dashboard is read-only — common actions (pause, retry, kill agent) require switching to another terminal.

## Design Goals

1. **Single-pane TUI** — all critical information visible simultaneously, no tab switching
2. **Web dashboard** — browser-based alternative with the same layout plus full interactive controls
3. **Real-time updates** — WebSocket pushes state changes immediately, not just on poll intervals
4. **Full control panel** — pause/resume, retry, reassign, escalate, kill, and edit from the web UI
5. **Zero external dependencies for frontend** — vanilla HTML/CSS/JS, no build step

## TUI Redesign

### Layout

Replace the tab-based 4-panel layout with a stacked single-screen showing all sections:

```
┌─────────────────────────────────────────────────────────────────┐
│ VXD DASHBOARD v1.0                          [Refresh: 2s] [Web]│
├─ Agents ────────────────────────────────────────────────────────┤
│ ROLE        MODEL              STATUS   STORY          SESSION  │
│ SENIOR      claude-sonnet-4    WORKING  01KM-s-003     vxd-s003 │
│ JUNIOR      gpt-4o-mini        IDLE     —              —        │
│ MANAGER     claude-sonnet-4    WORKING  01KM-s-001     —(API)   │
├─ Pipeline ──────────────────────────────────────────────────────┤
│ Planned: 4  In Prog: 2  Review: 1  QA: 0  PR: 1  Merged: 3    │
│ ████████████████████░░░░░░░░░  30% complete                    │
├─ Stories ───────────────────────────────────────────────────────┤
│ ID              STATUS          COMPLEXITY  TIER  TITLE         │
│ 01KM-s-001      in_progress|T2  [C3]        2     Scaffold...  │
│ 01KM-s-002      merged          [C2]        0     Configure... │
│ 01KM-s-003      review          [C3]        0     Define...    │
│ 01KM-s-001-a    planned         [C2]        0     Part A...    │
├─ Activity ──────────────────────────────────────────────────────┤
│ 03:30:49 STORY_ESCALATED     monitor  01KM-s-001  tier 0→2     │
│ 03:30:47 STORY_REVIEW_FAILED reviewer 01KM-s-001               │
│ 03:30:45 STORY_MERGED        merger   01KM-s-002               │
│ 03:30:40 AGENT_SPAWNED       —        01KM-s-003  vxd-s003     │
├─ Escalations [1 pending] ──────────────────────────────────────┤
│ 01KM-s-001  monitor  Tier 0→2  pending  max retries exhausted  │
└─────────────────────────────────────────────────────────────────┘
```

### Section Sizing

- **Agents**: Fixed height — rows equal to active agent count + 1 header row
- **Pipeline**: Fixed 2 rows — summary counts + progress bar
- **Stories**: Variable — expands to fill available space (scrollable)
- **Activity**: Fixed 5-8 rows depending on terminal height — always visible, newest first
- **Escalations**: Collapsed to 1 row when empty ("No escalations"), expands when pending

### Keyboard Controls

| Key | Action |
|-----|--------|
| `q` / `Ctrl+C` | Quit |
| `j` / `k` / arrows | Scroll stories table |
| `w` | Open web dashboard in browser |

### Changes from Current TUI

- Remove tab bar and tab navigation
- Remove `internal/dashboard/pipeline.go` kanban column layout — replace with summary bar
- Rewrite `internal/dashboard/app.go` to render all sections in a single `View()`
- Adjust `activity.go` to render a fixed number of rows (no full-panel mode)
- Adjust `escalations.go` to collapse when empty

## Web Dashboard

### Server Architecture

```
vxd dashboard --web [--port 8787]
  │
  ├── HTTP Server (localhost:8787)
  │   ├── GET /              → index.html (embedded)
  │   ├── GET /styles.css    → styles.css (embedded)
  │   ├── GET /app.js        → app.js (embedded)
  │   └── WebSocket /ws      → bidirectional connection
  │
  └── WebSocket Hub
      ├── Broadcasts state snapshot every 2s
      ├── Pushes immediate event on state change
      └── Receives commands from browser
```

### Startup

```bash
vxd dashboard          # launches TUI (redesigned single-pane)
vxd dashboard --web    # launches web server, prints URL, opens browser
vxd dashboard --web --port 9090  # custom port
```

TUI and web are alternative views, not concurrent. A user can run both in separate terminals if desired.

### WebSocket Protocol

**Server → Client: State broadcast (every 2s)**

```json
{
  "type": "state",
  "data": {
    "agents": [
      {"id": "senior-01KM-1", "role": "senior", "model": "claude-sonnet-4", "status": "active", "story_id": "01KM-s-003", "session_name": "vxd-s003"}
    ],
    "stories": [
      {"id": "01KM-s-001", "title": "Scaffold...", "status": "in_progress", "complexity": 3, "escalation_tier": 2, "agent_id": "senior-01KM-1"}
    ],
    "pipeline": {"planned": 4, "in_progress": 2, "review": 1, "qa": 0, "failed": 0, "pr_submitted": 1, "merged": 3},
    "events": [
      {"type": "STORY_ESCALATED", "timestamp": "2026-03-22T03:30:49Z", "agent_id": "monitor", "story_id": "01KM-s-001"}
    ],
    "escalations": [
      {"id": "esc-1", "story_id": "01KM-s-001", "from_agent": "monitor", "from_tier": 0, "to_tier": 2, "status": "pending", "reason": "max retries"}
    ],
    "requirements": [
      {"id": "01KM...", "title": "Create angular CRUD API", "status": "planned"}
    ]
  }
}
```

**Server → Client: Single event push (immediate)**

```json
{
  "type": "event",
  "data": {"type": "STORY_MERGED", "timestamp": "...", "story_id": "01KM-s-002", "agent_id": "merger"}
}
```

**Client → Server: Command**

```json
{
  "type": "command",
  "action": "pause_requirement",
  "payload": {"req_id": "01KM..."}
}
```

**Server → Client: Command result**

```json
{
  "type": "command_result",
  "action": "pause_requirement",
  "success": true,
  "message": "Requirement paused"
}
```

### Web Layout

Same 5 sections as TUI, rendered as HTML with browser-native interactivity:

**Agents panel:**
- Status badges with color (green active, red stuck, gray idle/terminated)
- Session name as "copy tmux attach command" button
- Kill button (X) per agent row

**Pipeline bar:**
- Clickable segments that filter the stories table below
- Animated progress bar

**Stories table:**
- Sortable columns (click header to sort)
- Per-row action buttons: retry, reassign (dropdown), escalate
- Inline edit button for title/description/acceptance criteria
- Status and tier badges with color coding

**Activity log:**
- Scrollable, auto-scrolls to bottom on new events
- Filter buttons by event type category (REQ, STORY, AGENT, ESCALATION)

**Escalations:**
- Expandable rows showing full diagnostic context
- Resolve button (for manual resolution)

### Styling

Dark theme matching the TUI color palette:
- Background: `#1A1A2E`
- Primary: Cyan `#00CCCC`
- Success: Green `#00CC66`
- Warning: Orange `#FF9933`
- Error: Red `#FF4444`
- Text: White `#FFFFFF`
- Muted: Gray `#888888`

Monospace font for tables and logs. Clean, minimal UI — functional dashboard, not a design showcase.

## Commands & Safety

| Command | Server Action | Confirmation Required |
|---------|--------------|----------------------|
| `pause_requirement` | Emit `EventReqPaused` with `{id: req_id}` | No |
| `resume_requirement` | Emit `EventReqResumed` with `{id: req_id}` | No |
| `retry_story` | Emit `EventStoryEscalated` with `{from_tier: current, to_tier: 0}` to force-reset tier, then emit `EventStoryReviewFailed` to reset to draft. This gives the story a fresh start at tier 0. | No |
| `reassign_story` | Emit `EventStoryEscalated` with `{from_tier: current, to_tier: target_tier}` then emit `EventStoryReviewFailed` to reset to draft. Payload must include `target_tier` (valid: 0-3). | Yes — browser confirm dialog |
| `escalate_story` | Query current tier via event count, emit `EventStoryEscalated` with `{from_tier: current, to_tier: current+1}` then emit `EventStoryReviewFailed` to reset to draft. Forces advancement by one tier. | No |
| `kill_agent` | Resolve session name from projection store by agent ID (never from client payload). Run `exec.Command("tmux", "kill-session", "-t", sessionName)` using arg-array form (no shell). Agent ID in payload must match `^[a-zA-Z0-9_-]+$`. | Yes — browser confirm dialog |
| `edit_story` | Emit `EventStoryRewritten` with changed fields in `changes` map. Projection handler updates `title`, `description`, `acceptance_criteria`, `complexity` and resets `escalation_tier` to 0 and `status` to `draft`. Note: `owned_files` editing requires extending `projectStoryRewritten` in `sqlite.go` to marshal and write the `owned_files` JSON column — deferred to v2. Any edit (even title-only) fully resets the story to draft at tier 0. | Yes — browser confirm dialog |

**Distinction between retry vs escalate:** `retry_story` forces a reset to tier 0 (start over). `escalate_story` forces advancement by one tier (skip to the next level). They are intentionally different actions.

**Dashboard-sourced escalation records:** Both `retry_story` and `reassign_story` emit `EventStoryEscalated`, which creates escalation table records. To distinguish dashboard-initiated escalations from organic ones, the event payload includes `"source": "dashboard"`. The escalations panel can use this to show a `[manual]` badge next to dashboard-triggered entries.

**TUI is read-only:** All mutation commands are web-dashboard only. The TUI remains a monitoring-only view — this is by design, not an omission.

**Command prerequisite:** Commands emit events into the store. Effects (agent spawning, story dispatch) only take place when `vxd resume` is actively running. The web UI should show a warning banner if no active requirement is running.

**Auth:** None for v1. Server binds only to `localhost`. Banner on page: "Connected to localhost:{port} — local access only."

### Input Validation

All command payloads are validated server-side before execution:
- `req_id` and `story_id`: must be non-empty strings, looked up in projection store before acting
- `agent_id`: must match `^[a-zA-Z0-9_-]+$`
- `target_tier`: must be integer 0-3
- `edit_story` fields: only `title`, `description`, `acceptance_criteria`, `complexity` are accepted in v1 (`owned_files` deferred to v2); other fields are silently dropped

## File Structure

### New Files

```
internal/web/
  server.go          — HTTP server setup, route registration, go:embed directive
  ws.go              — WebSocket hub: client tracking, broadcast loop, command dispatch
  handlers.go        — Command handlers (pause, resume, retry, reassign, kill, edit)
  data.go            — State snapshot builder (queries stores, builds JSON payload)
  static/
    index.html       — Single page with all 5 sections
    styles.css       — Dark theme, responsive layout
    app.js           — WebSocket client, DOM rendering, command sending, confirmations
```

### Modified Files

```
internal/cli/dashboard.go      — Add --web and --port flags, launch web server
internal/dashboard/app.go      — Rewrite for single-pane dense layout (remove tabs)
internal/dashboard/pipeline.go — Replace kanban columns with summary bar + progress
internal/dashboard/activity.go — Fixed height for always-visible mode
internal/dashboard/escalations.go — Collapsible single-line when empty
```

### Dependencies

- `nhooyr.io/websocket` — WebSocket library (stdlib-compatible, no CGo)
- All static files embedded via `//go:embed static/*`
- No frontend build step — vanilla HTML/CSS/JS

## Data Flow

Both TUI and web dashboard share the same underlying stores:

```
EventStore (events.jsonl)  ──┐
                             ├──→ TUI (Bubbletea, 2s poll)
ProjectionStore (SQLite)   ──┤
                             └──→ Web Server
                                   ├── State snapshot (2s broadcast via WebSocket)
                                   ├── Event push (immediate on new events)
                                   └── Command execution (writes to stores)
```

The web server's `data.go` queries the same way the TUI's `fetchData()` does. No new data layer needed.

### Event Push Strategy

The WebSocket hub detects new events during the 2-second broadcast loop by comparing the last known event count. Each broadcast:
1. Query `eventStore.Count(EventFilter{})` — if count > last known count
2. Query new events: `eventStore.List(EventFilter{After: lastEventTimestamp})`
3. Send individual `{"type": "event", ...}` messages for each new event
4. Then send the full state snapshot

This is pragmatic polling (consistent with the TUI model), not true file-watching. The 2-second granularity is sufficient for a monitoring dashboard.

### Pipeline Status Buckets

Both TUI and web use the same canonical pipeline stages:

| Stage | Statuses Included |
|-------|-------------------|
| Planned | draft, estimated, planned, assigned |
| In Prog | in_progress |
| Review | review |
| QA | qa, qa_started |
| PR | pr_submitted |
| Merged | merged |
| Split | split (shown separately, not counted in progress %) |

Progress percentage = `(merged + pr_submitted) / total_non_split * 100`

### SQLite Concurrent Access

The web server opens SQLite in WAL mode (`PRAGMA journal_mode=WAL`) to allow concurrent reads while `vxd resume` holds write locks. Command handlers that write events must handle `SQLITE_BUSY` with retry backoff (3 attempts, 100ms/200ms/500ms delays).

### Graceful Shutdown

On `SIGINT`/`SIGTERM`:
1. Close all WebSocket connections (send close frame)
2. Call `server.Shutdown(ctx)` with 3-second grace period
3. Close event and projection stores
4. Print "Dashboard server stopped" and exit 0

Use `signal.NotifyContext` (same pattern as `resume.go`).

### Port Handling

On `net.Listen` failure, print: `"Error: port {port} is already in use. Try: vxd dashboard --web --port {port+1}"` and exit 1.

### WebSocket Reconnection

`app.js` must implement exponential backoff reconnection:
- On disconnect: show "Disconnected — reconnecting..." banner (red)
- Retry at 1s, 2s, 4s, 8s, max 30s intervals
- On reconnect: hide banner, request full state snapshot

### Empty States

- **No agents:** Show "No agents running" in agents section (both TUI and web)
- **No stories:** Show "No stories — run 'vxd plan' to create a requirement" in stories section
- **No events:** Show "Waiting for activity..." in activity log
- **No escalations:** TUI collapses to "No escalations". Web shows empty section with muted text.

### TUI `w` Key Behavior

The `w` key attempts to open `http://localhost:8787` in the default browser. If the web server isn't running, the browser will show a connection error — the TUI does not start a web server. To use both, run `vxd dashboard --web` in one terminal and `vxd dashboard` in another.

### `--all` Flag

The `--all` flag applies to both TUI and web modes. It controls `ReqFilter` passed to `data.go`'s query functions. Without `--all`, only the current repo's requirements are shown (excluding archived).

### Multiple Browser Tabs

Commands are accepted from any connected tab. Command handlers must be idempotent where possible:
- `kill_agent`: Check if session exists before killing (tmux returns exit 1 if not — suppress that error)
- `pause/resume`: Check current status before emitting events (skip if already in target state)
- `retry/reassign/escalate`: These emit events unconditionally — duplicates are harmless since the escalation machine re-reads state from events

### Error Display in Web UI

Command results with `success: false` display as a toast notification (red banner at top, auto-dismiss after 5 seconds). Successful commands show a brief green toast.

## Out of Scope

- Authentication / multi-user access (localhost only for v1)
- Persistent web server (daemon mode) — it runs while the terminal is open
- Mobile-responsive layout — desktop browser only
- Dark/light theme toggle — dark only
- Concurrent TUI + web from same command — they're separate invocations
- SUPERVISOR event filter button in web activity log (events still visible, just no dedicated filter toggle)
