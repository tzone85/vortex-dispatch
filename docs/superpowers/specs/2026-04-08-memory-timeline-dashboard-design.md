# Memory Timeline Dashboard Design

**Date:** 2026-04-08
**Status:** Approved
**Command:** `vxd memory --web --port 8078`

## Overview

A standalone web dashboard for navigating VXD's institutional memory over time. Features a draggable timeline slider + calendar heatmap for date selection, with detail panels showing PRs, findings, commits, and MemPalace search results for the selected date. All data is local — audit logs, git history, and MemPalace semantic search.

## Architecture

**Command:** `vxd memory --web` (optional `--port`, defaults to 8078)

Standalone HTTP server + WebSocket, same vanilla HTML/CSS/JS pattern as `vxd dashboard --web`. Separate from the main dashboard — the main dashboard is for real-time pipeline monitoring, this is for reflective/historical exploration.

**Data sources (all local, no API calls):**
- `docs/self-improvement/changelog.jsonl` — audit entries with scores, dispositions, PR links
- `docs/self-improvement/runs/YYYY-MM-DD.json` — per-run summaries
- `git log` — commit history with timestamps
- MemPalace search API — `python3 -m mempalace search` via subprocess for on-demand queries

**No new dependencies.** Same tech as existing web dashboard: `net/http`, `gorilla/websocket` (already in go.mod), vanilla JS.

## UI Layout

### Top Bar
- Title: "VXD Memory Timeline"
- Search box (right-aligned): queries MemPalace, scoped to selected date range

### Timeline Slider
- Horizontal bar spanning full width
- Draggable handle: left = earliest data, right = today
- Date labels at left edge, handle position, and right edge
- Dragging updates all panels below in real-time

### Middle Row (two columns)

**Left: Calendar Heatmap**
- Monthly grid (like GitHub contribution graph)
- Each cell colored by activity level (PRs + findings + commits)
- Shades: no fill (0 events), light green (1-2), medium green (3-5), dark green (6+). Activity = PRs + findings_relevant + commits for that day.
- Click a cell → jumps slider to that day
- Month navigation arrows (← →)

**Right: Cumulative Charts**
- PRs over time (bar chart, last 30 days from selected date)
- Category breakdown (pie chart: security/performance/feature/dependency/docs)
- Findings trend (line chart, last 30 days)
- Charts rendered via QuickChart.io `<img>` URLs (same approach as email — no JS chart library)
- Charts update when slider moves (new img src URLs computed client-side)

### Bottom Section (day detail)

Four collapsible card sections for the selected date:

**1. PRs Created**
- Table: title (linked to GitHub), status (merged/open), category, lines changed
- Empty state: "No PRs on this date"

**2. Findings Analyzed**
- List: relevance star rating, title, disposition badge (implemented/proposed/rejected), category tag, source link
- Expandable: click to show full reasoning, scores (relevance/impact/risk), security review

**3. Commits**
- List: short SHA (linked to GitHub), commit message, timestamp
- Source: `git log --after="date 00:00" --before="date 23:59"`

**4. MemPalace Search**
- Input field: "Search memories from this date range..."
- Results: text excerpt, wing/room tags, similarity score (0-1), source file
- Scoped to content that existed as of the selected date

## Interactions

| Action | Effect |
|--------|--------|
| Drag slider | Updates selected date, refreshes all panels and charts |
| Click calendar cell | Jumps slider to that day |
| Click PR title | Opens GitHub PR in new tab |
| Click finding | Expands to show full reasoning, scores, source link |
| Click commit SHA | Opens GitHub commit in new tab |
| Type in search box | Queries MemPalace, results appear in search panel |
| Month arrows on calendar | Navigate to previous/next month |

## Data Flow

### On Page Load
1. WebSocket connects to server
2. Server reads all `runs/YYYY-MM-DD.json` → builds timeline index
3. Server reads `changelog.jsonl` → builds date-indexed findings map
4. Server runs `git log --format` → builds date-indexed commit map
5. Server sends: `{type: "init", timeline: [...], range: {min, max, today}}`
6. Client renders slider (min to today), calendar (current month), charts (cumulative)

### On Slider Drag
1. Client sends: `{type: "select_date", date: "2026-04-08"}`
2. Server responds: `{type: "day_detail", date, prs, findings, commits, run_summary}`
3. Client updates bottom panels + recomputes chart URLs for data up to selected date

### On MemPalace Search
1. Client sends: `{type: "search", query: "...", date: "2026-04-08"}`
2. Server runs `python3 -m mempalace search "query"` subprocess, parses JSON output
3. Server responds: `{type: "search_results", results: [...]}`
4. Client renders results in search panel

### No Polling
Data changes once per day (when `vxd-improve` runs). Server reads files on demand per WebSocket message.

## WebSocket Message Types

### Client → Server
```json
{"type": "select_date", "date": "2026-04-08"}
{"type": "search", "query": "why event sourcing", "date": "2026-04-08"}
```

### Server → Client
```json
{
  "type": "init",
  "timeline": [
    {"date": "2026-04-08", "prs": 2, "findings": 11, "commits": 5, "activity_level": 3}
  ],
  "range": {"min": "2026-04-08", "max": "2026-04-08", "today": "2026-04-08"}
}

{
  "type": "day_detail",
  "date": "2026-04-08",
  "prs": [{"title": "...", "url": "...", "status": "merged", "category": "...", "lines": 87}],
  "findings": [{"title": "...", "relevance": 9, "impact": 5, "risk": 2, "disposition": "proposed", "category": "security", "source_url": "...", "reasoning": "..."}],
  "commits": [{"sha": "685d46e", "message": "fix: increase max findings", "timestamp": "2026-04-08T16:38:00Z"}],
  "run_summary": {"sources_scraped": 11, "findings_total": 11, "prs_created": 0, "email_sent": true}
}

{
  "type": "search_results",
  "query": "why event sourcing",
  "results": [{"text": "...", "wing": "vortex_dispatch", "room": "internal", "source_file": "architecture.md", "similarity": 0.42}]
}
```

## Files Changed

| File | Action | Description |
|------|--------|-------------|
| `internal/memory/server.go` | **New** | HTTP + WebSocket server, static file serving |
| `internal/memory/data.go` | **New** | Types, data loaders (audit, git, timeline indexing) |
| `internal/memory/mempalace.go` | **New** | MemPalace subprocess wrapper for search |
| `internal/memory/server_test.go` | **New** | WebSocket message handling, HTTP endpoint tests |
| `internal/memory/data_test.go` | **New** | Timeline indexing, date filtering, chart data |
| `internal/memory/mempalace_test.go` | **New** | Search result parsing |
| `internal/memory/static/index.html` | **New** | Dashboard HTML structure |
| `internal/memory/static/styles.css` | **New** | Slider, calendar heatmap, card, chart styles |
| `internal/memory/static/app.js` | **New** | WebSocket client, slider, calendar, search, charts |
| `internal/cli/memory.go` | **New** | `vxd memory --web --port 8078` CLI command |
| `.github/workflows/ci.yml` | **Edit** | Verify `internal/memory` compiles |

## Constraints

- **Zero external dependencies** — no React, no chart libraries, no npm. Vanilla HTML/CSS/JS only.
- **Charts via QuickChart.io** — `<img>` URLs, same as email. Computed client-side from timeline data.
- **MemPalace optional** — if `python3 -m mempalace` is not available, search panel shows "MemPalace not installed" gracefully.
- **No server-side state** — server reads files on demand. No database, no cache.
- **Responsive** — works on laptop screens (min 1024px width).
