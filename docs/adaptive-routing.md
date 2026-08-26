# Adaptive Routing (F3) — Design Note

## Problem

The dispatcher routed purely by static complexity thresholds
(`routing.junior_max_complexity` / `routing.intermediate_max_complexity`). But
the event log already records which execution tier actually succeeds at which
complexity **in this repo**. Ignoring that history burns escalations (junior
attempt → tier-1 senior retry) on story shapes this repo's history says junior
cannot do — and overpays for story shapes junior handles reliably.

## Design

Two pure functions in `internal/engine/adaptive.go`:

### `TierSuccessRates(events) map[int]map[int]TierOutcome`

Walks the event log in order and attributes every *resolved* attempt to a
`(dispatch-tier, complexity)` cell:

| Event | Attribution |
|-------|-------------|
| `STORY_CREATED` | supplies the story's complexity |
| `STORY_STARTED` (`role` field) | supplies the attempting tier (latest wins — retries overwrite) |
| `STORY_ESCALATED` | FAILURE for the current tier |
| `STORY_COMPLETED` | SUCCESS for the current tier |

Dispatch tiers (`RouteTierJunior=0`, `RouteTierIntermediate=1`,
`RouteTierSenior=2`) are deliberately distinct from the 5-level escalation-
chain numbers carried in event payloads (where junior and intermediate both
sit at tier 0): conflating the scales would misattribute senior attempts.

Attempts with no resolvable role or complexity are skipped, never guessed.

### `RecommendTier(complexity, rates, cfg)`

Refines the static default:

- **Demote** (checked first): the cheapest cheaper tier with ≥
  `adaptive_min_samples` resolved attempts and ≥ 80% success at this
  complexity takes the story. When both signals fire, the cheaper tier's
  strong record wins — biggest cost saving.
- **Promote**: the default tier with ≥ min samples and < 40% success routes
  up ONE tier — bounded, never above senior.
- Otherwise: the static complexity-threshold default.

## Wiring & auditability

Opt-in via `routing.adaptive: true` (default false; `TestDefaultConfig_AdaptiveOff`
pins it). When enabled, `Dispatcher.routeStoryWithDecision` computes rates
from the event store and applies `RecommendTier`. Overrides are logged in the
existing dispatcher style:

```
[dispatcher] adaptive: routed s-004 (cx 3) junior→intermediate, junior success 2/7
```

and carried on `Assignment.AdaptiveDecision`, which the executor copies onto
the `STORY_STARTED` payload as `adaptive_decision`. No new event types are
introduced, so the projection switch and its exhaustiveness test are untouched.

## Precedence & safety

- An existing escalation on the story (tier-1 senior retry path) **always
  wins**; adaptive routing never demotes an escalated story back down
  (`TestDispatcher_AdaptiveRouting_EscalationOverrideWins`).
- Fewer than `adaptive_min_samples` (default 5) resolved attempts at a cell →
  static routing stands.
- History read failure degrades to static routing with a log line.
- Disabled flag short-circuits before any event-store read (zero overhead).

## Tests

- `TestTierSuccessRates_FromEventHistory` — attribution rules, table-driven.
- `TestRecommendTier_PromoteDemoteBounds` — thresholds, bounds, disable switch,
  custom min-samples, demote-over-promote precedence (10 cases).
- `TestDispatcher_AdaptiveRoutingWired` (+ disabled / escalation-precedence
  variants) — proves the dispatcher ACTIVATES the recommendation end-to-end.
- `TestDefaultConfig_AdaptiveOff` — opt-in default pinned.
