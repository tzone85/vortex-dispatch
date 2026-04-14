# Cost Estimation — `vxd estimate`

## Overview

A CLI command that produces client-facing quotes and internal cost projections from a natural language requirement. Uses the existing planner for accurate decomposition, with a heuristic fallback for quick ballparking.

## Command Interface

```
vxd estimate <requirement>            # live planner-based estimate
vxd estimate <requirement> --quick    # heuristic, no API call
vxd estimate <requirement> --rate N   # override hourly rate (USD)
vxd estimate <requirement> --json     # structured JSON output
vxd estimate <requirement> --save     # persist as EventReqEstimated
vxd estimate <requirement> --project X  # explicit project context
```

Flags can be combined: `vxd estimate "..." --quick --json --save`.

## Output

### Default (human-readable table)

```
Estimate: Add OAuth2 login with Google and GitHub providers
──────────────────────────────────────────────────────────

Stories  Complexity   Est. Hours    Client Quote ($150/hr)
───────  ──────────   ──────────    ──────────────────────
  4         13 pts     8 – 12h      $1,200 – $1,800

Story Breakdown:
  #1  Setup OAuth2 middleware       3 pts   2–3h   (junior)
  #2  Google provider integration   3 pts   2–3h   (junior)
  #3  GitHub provider integration   3 pts   2–3h   (junior)
  #4  Session management + tests    5 pts   4–6h   (intermediate)

LLM Cost:  $0.00  (Claude Max + Google Free)
Margin:    ~100%
```

### Quick mode (`--quick`)

```
Quick Estimate: Add OAuth2 login with Google and GitHub providers
─────────────────────────────────────────────────────────────────

Est. Stories: 3–5  |  Est. Hours: 6–15h  |  Quote: $900 – $2,250

⚠ Heuristic only — run without --quick for planner-based estimate
```

### JSON mode (`--json`)

```json
{
  "estimate_id": "est-20260410-a3b7c9d1",
  "requirement": "Add OAuth2 login with Google and GitHub providers",
  "is_quick": false,
  "project": "acme-corp-api",
  "stories": [
    {
      "title": "Setup OAuth2 middleware",
      "complexity": 3,
      "role": "junior",
      "hours_low": 2.0,
      "hours_high": 3.0,
      "cost_low": 300.0,
      "cost_high": 450.0
    }
  ],
  "summary": {
    "story_count": 4,
    "total_points": 13,
    "hours_low": 8.0,
    "hours_high": 12.0,
    "quote_low": 1200.0,
    "quote_high": 1800.0,
    "llm_cost": 0.0,
    "margin_percent": 100.0,
    "rate": 150.0,
    "currency": "USD"
  }
}
```

## Config Schema

New `billing` section added to Config struct, loaded via the existing config chain (repo `vxd.yaml` > global `~/.vxd/config.yaml` > defaults).

```yaml
billing:
  default_rate: 150
  currency: "USD"
  hours_per_point:
    1:  [0.5, 1.0]
    2:  [1.0, 2.0]
    3:  [2.0, 3.0]
    5:  [3.0, 5.0]
    8:  [5.0, 8.0]
    13: [8.0, 13.0]
  llm_costs:
    mode: "subscription"
```

### Config Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `billing.default_rate` | float64 | 150.0 | Hourly rate in billing currency |
| `billing.currency` | string | "USD" | Currency code for display |
| `billing.hours_per_point` | map[int][2]float64 | See above | Complexity point to [low, high] hours mapping |
| `billing.llm_costs.mode` | string | "subscription" | "subscription" (flat $0) or "per_token" |
| `billing.llm_costs.rates` | map[string]TokenRate | nil | Per-model token rates (only used when mode is "per_token") |

### TokenRate

```yaml
rates:
  claude-opus-4-20250514:
    input_per_1k: 0.015
    output_per_1k: 0.075
```

Only relevant when `mode: "per_token"`. When `mode: "subscription"`, LLM cost is always $0.00.

## Architecture

### Data Flow — Live Estimate

```
CLI (estimate.go)
  → Planner.Plan() — reuses existing Tech Lead decomposition
  → CostCalculator.Calculate(stories, config.Billing) — maps stories to hours/cost
  → Formatter.Format(estimate, outputMode) — table or JSON
  → [if --save] EventStore.Append(EventReqEstimated, payload)
```

### Data Flow — Quick Estimate

```
CLI (estimate.go)
  → QuickEstimate(requirement) — heuristic story/complexity guess
  → CostCalculator.Calculate(heuristicStories, config.Billing) — same cost logic
  → Formatter.Format(estimate, outputMode) — table or JSON with warning
  → [if --save] EventStore.Append(EventReqEstimated, payload)
```

### New Files

| File | Responsibility |
|------|---------------|
| `internal/cli/estimate.go` | Cobra command registration, flag parsing, output formatting |
| `internal/engine/estimator.go` | `Estimator` struct orchestrating plan-to-cost pipeline |
| `internal/engine/cost.go` | `CostCalculator` — pure function: stories + billing config → Estimate |
| `internal/engine/heuristic.go` | `QuickEstimate()` — text analysis for rough story/complexity guess |

### Key Structs

```go
// Estimate is the full result of a cost estimation
type Estimate struct {
    EstimateID  string          `json:"estimate_id"`
    Requirement string          `json:"requirement"`
    Project     string          `json:"project"`
    IsQuick     bool            `json:"is_quick"`
    Stories     []StoryEstimate `json:"stories"`
    Summary     EstimateSummary `json:"summary"`
}

// EstimateSummary holds the aggregated cost/hours data
type EstimateSummary struct {
    StoryCount    int     `json:"story_count"`
    TotalPoints   int     `json:"total_points"`
    HoursLow      float64 `json:"hours_low"`
    HoursHigh     float64 `json:"hours_high"`
    QuoteLow      float64 `json:"quote_low"`
    QuoteHigh     float64 `json:"quote_high"`
    LLMCost       float64 `json:"llm_cost"`
    MarginPercent float64 `json:"margin_percent"`
    Rate          float64 `json:"rate"`
    Currency      string  `json:"currency"`
}

// StoryEstimate is a planned story with cost projections
type StoryEstimate struct {
    Title      string  `json:"title"`
    Complexity int     `json:"complexity"`
    Role       string  `json:"role"`
    HoursLow   float64 `json:"hours_low"`
    HoursHigh  float64 `json:"hours_high"`
    CostLow    float64 `json:"cost_low"`
    CostHigh   float64 `json:"cost_high"`
}
```

### Reused Components

| Component | Location | How Used |
|-----------|----------|----------|
| `Planner.Plan()` | `internal/engine/planner.go` | Decompose requirement into stories (live mode) |
| `agent.RouteByComplexity()` | `internal/agent/roles.go` | Map complexity to agent role |
| `config.LoadConfigChain()` | `internal/config/loader.go` | Load billing config via chain |
| `state.EventStore.Append()` | `internal/state/store.go` | Persist estimate event (--save) |

### New Event Type

`EventReqEstimated` — added to `internal/state/events.go`.

Payload:

```json
{
  "estimate_id": "est-20260410-a3b7c9d1",
  "requirement": "Add OAuth2 login with Google and GitHub providers",
  "is_quick": false,
  "stories": 4,
  "total_points": 13,
  "hours_low": 8.0,
  "hours_high": 12.0,
  "quote_low": 1200.0,
  "quote_high": 1800.0,
  "llm_cost": 0.0,
  "rate": 150.0,
  "currency": "USD",
  "project": "acme-corp-api"
}
```

## Heuristic Engine (`--quick`)

Text analysis that produces a rough estimate without an LLM call.

### Signals

| Signal | Detection | Effect |
|--------|-----------|--------|
| Conjunctions | "and", "with", commas, bullet points | +1 story per conjunction |
| Complexity keywords | "auth", "migration", "real-time", "integration", "security", "payment" | +1-2 complexity points |
| Simplicity keywords | "simple", "basic", "add field", "rename", "update text" | -1 complexity points |
| Plural indicators | "providers", "endpoints", "pages", "services" | story count multiplier |
| Requirement length | Word count | Longer requirements = more stories |

### Algorithm

1. Base story count = max(1, conjunction_count + 1)
2. Adjust for plural indicators (multiply affected story segments)
3. Base complexity per story = 3 (median Fibonacci)
4. Adjust per story based on complexity/simplicity keywords
5. Clamp complexity to valid Fibonacci values: [1, 2, 3, 5, 8, 13]
6. Produce story range: [count × 0.7, count × 1.5] (rounded)
7. Feed into `CostCalculator` with synthetic `StoryEstimate` entries

The heuristic output always includes a warning that it is approximate.

## Persistence (`--save`)

- Generates estimate ID: `est-{YYYYMMDD}-{first8ofULID}`
- Emits `EventReqEstimated` via existing `EventStore.Append()`
- Estimate is independent of the requirement lifecycle — not linked to any `reqID`
- Data foundation for future `vxd accuracy` command (compare estimates vs. actuals)

## Billing Config Defaults

Default rate: **$150/hr USD**. This positions in the senior-to-architect range for remote AI/ML work.

Suggested per-context rates for reference (not enforced by system):
- Startups (seed/A): $100-140/hr
- Scale-ups/mid-market: $130-170/hr
- Enterprise: $150-200+/hr
- Bounties: fixed price (use `--rate` per estimate)

## Out of Scope

- Estimate vs. actual comparison (`vxd accuracy`) — future feature
- Auto-quoting from opportunity pipeline — future feature
- LLM token estimation for `per_token` mode — implement when switching off subscriptions
- Multi-currency support — USD only for now
