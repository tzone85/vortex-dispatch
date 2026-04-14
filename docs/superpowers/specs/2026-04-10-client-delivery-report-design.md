# Client Delivery Report — `vxd report`

## Overview

A CLI command that generates professional delivery reports for completed or in-progress requirements. Produces client-facing summaries (deliverables, timeline, effort, PR links) with optional internal technical detail (escalations, retries, agent performance, timing, estimate accuracy). Outputs markdown by default, self-contained HTML with `--html`.

## Command Interface

```
vxd report <req-id>                           # markdown to stdout
vxd report <req-id> --output report.md        # save to file
vxd report <req-id> --html                    # self-contained HTML to stdout
vxd report <req-id> --html --output report.html
vxd report <req-id> --internal                # adds technical detail sections
vxd report <req-id> --internal --html --output delivery.html
```

Works on requirements in any status — completed, in-progress, or paused. Status is displayed prominently.

## Report Sections

### Client Report (default)

**1. Header**
- Project name, requirement ID, date, status with emoji indicator
- Status values: ✅ Completed, 🔄 In Progress (N of M merged), ⏸ Paused

**2. Requirement**
- The original requirement text as submitted

**3. Deliverables**

| # | Story | Status | Complexity | PR |
|---|-------|--------|------------|-----|
| 1 | Setup OAuth middleware | Merged | 3 | [#42](url) |
| 2 | Google provider | In Progress | 3 | — |

**4. Timeline**
- Started: timestamp of first event
- Completed: timestamp of last merge (or "In progress")
- Elapsed: duration from start to completion or now

**5. Effort Summary**
- Total stories and complexity points
- Estimated hours (from billing config hours_per_point mapping)
- Rate and quote range

### Internal Additions (`--internal`)

Appended after client sections, separated by a divider.

**6. Escalation Log**
- Stories that escalated, from/to tier, reason from event payload

**7. Retry History**
- Retry count per story, error category (from smart retry), resolution

**8. Agent Performance**
- Agents that worked on this requirement, role, story count, average QA score

**9. Timing Breakdown**
- Per-story timing: planning, execution, review, QA, total
- Computed from event timestamps (STORY_CREATED → STORY_ASSIGNED → STORY_COMPLETED → STORY_REVIEW_PASSED → STORY_QA_PASSED)

**10. Estimate vs. Actual**
- Only shown if a saved `EventReqEstimated` exists matching this requirement
- Compares predicted stories/hours/cost to actual
- Shows estimate ID for traceability

## Architecture

### New Files

| File | Responsibility |
|------|---------------|
| `internal/engine/report.go` | `ReportBuilder` struct, `Build()` method — queries stores, assembles `ReportData` |
| `internal/engine/report_render.go` | `RenderMarkdown()` and `RenderHTML()` — pure functions: `ReportData` → string |
| `internal/engine/report_test.go` | Tests for builder and both renderers |
| `internal/cli/report.go` | Cobra command, flags (`--html`, `--output`, `--internal`), file output |

### Modified Files

| File | Change |
|------|--------|
| `internal/cli/root.go` | Register `newReportCmd()` |

### Data Structs

```go
type ReportData struct {
    Project       string
    ReqID         string
    Requirement   string
    Status        string
    StatusEmoji   string
    Date          string
    Stories       []StoryReport
    StartedAt     time.Time
    CompletedAt   time.Time
    Elapsed       time.Duration
    TotalPoints   int
    HoursLow      float64
    HoursHigh     float64
    Rate          float64
    Currency      string
    MergedCount   int
    TotalCount    int

    // Internal sections (populated only when --internal)
    Internal      bool
    Escalations   []EscalationEntry
    Retries       []RetryEntry
    AgentStats    []AgentStat
    StoryTimings  []StoryTiming
    EstimateMatch *EstimateComparison
}

type StoryReport struct {
    Index      int
    Title      string
    Status     string
    Complexity int
    PRUrl      string
    PRNumber   int
    Branch     string
}

type EscalationEntry struct {
    StoryTitle string
    FromTier   string
    ToTier     string
    Reason     string
}

type RetryEntry struct {
    StoryTitle    string
    RetryCount    int
    ErrorCategory string
    Resolution    string
}

type AgentStat struct {
    AgentID    string
    Role       string
    StoryCount int
    AvgQAScore float64
}

type StoryTiming struct {
    Title     string
    Planning  time.Duration
    Execution time.Duration
    Review    time.Duration
    QA        time.Duration
    Total     time.Duration
}

type EstimateComparison struct {
    EstimateID     string
    PredictedStories int
    ActualStories    int
    PredictedHoursLow  float64
    PredictedHoursHigh float64
    ActualHours        float64
    PredictedCostLow   float64
    PredictedCostHigh  float64
    ActualCost         float64
}
```

### Data Flow

```
CLI (report.go)
  → loadStores() — EventStore + ProjectionStore
  → resolveProject() — project name for header
  → ReportBuilder.Build(reqID, opts) — queries stores, builds ReportData
  │   ├── GetRequirement(reqID)
  │   ├── ListStories(filter by reqID)
  │   ├── Query events for timeline (first/last timestamps)
  │   ├── Calculate effort from BillingConfig
  │   ├── [if internal] Query escalation events
  │   ├── [if internal] Query retry/review-failed events
  │   ├── [if internal] Query agent scores
  │   ├── [if internal] Compute per-story timings from events
  │   └── [if internal] Find matching EventReqEstimated
  → RenderMarkdown(data) or RenderHTML(data)
  → [if --output] write to file, else print to stdout
```

### Reused Components

| Component | How Used |
|-----------|----------|
| `state.ProjectionStore` | Get requirement, list stories |
| `state.EventStore` | Query events for timing, escalations, retries |
| `config.BillingConfig` | Hours-per-point mapping for effort summary |
| `engine.CalculateCost` | Reuse cost math for effort section |

## HTML Output

Self-contained HTML file with inline `<style>` block. No JavaScript, no external dependencies.

**Styling:**
- White background, dark text, subtle borders
- Status badges with color (green=completed, blue=in-progress, orange=paused)
- PR links as clickable `<a>` tags
- Tables with alternating row shading
- Internal sections separated with gray divider
- Print-friendly layout

**Structure:**
```html
<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>Delivery Report: {title}</title>
  <style>/* all CSS inline */</style>
</head>
<body>
  <!-- same sections as markdown, rendered as HTML elements -->
</body>
</html>
```

## Edge Cases

- **Requirement not found** — return clear error: "requirement {id} not found"
- **No stories yet** — show header and requirement text, empty deliverables table, note "No stories planned yet"
- **No PRs created** — show "—" in PR column
- **No saved estimate** — skip "Estimate vs. Actual" section silently
- **Paused requirement** — show ⏸ status, include all data up to pause point

## Out of Scope

- PDF generation (requires external dependency)
- Email sending (use copy-paste or external tools)
- Automatic report generation on requirement completion
- Multi-requirement aggregate reports
