# Memory Timeline Dashboard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a standalone web dashboard (`vxd memory --web`) for navigating VXD's institutional memory over time with a draggable timeline slider, calendar heatmap, and MemPalace semantic search.

**Architecture:** HTTP + WebSocket server following the exact pattern of VXD's existing `internal/web/` package. Embedded static files via `go:embed`. Data loaded from audit JSONL + run summaries + git log + MemPalace subprocess. Vanilla HTML/CSS/JS frontend with QuickChart.io for graphs.

**Tech Stack:** Go 1.23+, `nhooyr.io/websocket` (already in go.mod), `embed.FS`, vanilla HTML/CSS/JS, QuickChart.io

**Spec:** `docs/superpowers/specs/2026-04-08-memory-timeline-dashboard-design.md`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/memory/data.go` | Types (TimelineEntry, DayDetail, etc.) + data loaders (audit, git, timeline indexing) |
| `internal/memory/data_test.go` | Timeline building, date filtering, chart data computation |
| `internal/memory/mempalace.go` | MemPalace subprocess wrapper for search |
| `internal/memory/mempalace_test.go` | Search result parsing |
| `internal/memory/server.go` | HTTP + WebSocket server, message routing |
| `internal/memory/server_test.go` | WebSocket message handling |
| `internal/memory/static/index.html` | Dashboard HTML |
| `internal/memory/static/styles.css` | Slider, calendar, cards, charts |
| `internal/memory/static/app.js` | WebSocket client, UI interactions |
| `internal/cli/memory.go` | `vxd memory --web --port 8078` command |

---

## Task 1-6 Summary

6 tasks covering: data types + loaders, MemPalace wrapper, WebSocket server, frontend (HTML/CSS/JS), CLI command, and final verification. See the full plan in the spec for detailed code.

Note: The frontend uses innerHTML for rendering server-controlled audit data. All data originates from VXD's own audit files and git log — no external user input is rendered. The server sanitizes MemPalace search results before sending.
