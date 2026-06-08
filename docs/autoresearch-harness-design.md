# Autoresearch Harness Design

**Date:** 2026-05-02
**Author:** Thando Mini (with Claude)
**Status:** Approved
**Inspired by:** [karpathy/autoresearch](https://github.com/karpathy/autoresearch)

## Summary

Generic, sustainable, self-improving experiment harness inside VXD. Runs autoresearch-style loops (`hypothesize → edit → measure → keep/discard → iterate`) on **any tracked repo**, with pluggable metrics. Builds on existing VXD infrastructure (worktree+tmux, event sourcing, LLM clients, Bayesian feedback).

The user does not edit Python files like a researcher; they configure `vxd.yaml` and seed `program.md`. Agents do the rest, overnight, in parallel, indefinitely.

## Goals

1. Take any repo and turn it into an autoresearch target by adding a few lines of YAML.
2. Optimize an arbitrary numeric metric (LLM tiebreak on near-ties) without touching forbidden paths and without "cheating" the metric.
3. Self-improve over time: remember every experiment, sample smartly, evolve the agent's instructions.
4. Stay safe: never auto-merge unjudged code; never poison priors with infra failures; tripwire the obvious metric-hacks.
5. Reuse VXD infra. No new tmux, no new event store, no new LLM clients.

## Non-goals

- LLM training (karpathy's `train.py`) — the harness is *generic*, not a port of nanochat.
- Replacing the existing self-improvement engine (`internal/improve/`) — autoresearch is a sibling, not a successor.
- Distributed compute. Single-host parallelism is enough for v1.
- Adaptive time budgets. Fixed-budget keeps experiments comparable (the whole point).

## Locked design decisions

| Question | Choice |
|---|---|
| Interpretation | C — generic harness for any tracked repo, pluggable metrics |
| Metric | D — numeric primary + LLM tiebreaker on near-tie |
| Concurrency | B — parallel via existing VXD worktree+tmux |
| Sustainability | D — layered: experiment memory + Bayesian sampler + program.md auto-evolution (staged, last is human-gated) |
| Edit scope | D — allowlist hard floor + LLM tripwire |
| Acceptance gate | D — per-repo configurable (auto / winning-branch / PR), default winning-branch |
| Time budget | D — scheduled batch by default, continuous opt-in |
| Code location | Option 3 — new `internal/autoresearch/` package, shared infra |

## Architecture

```
                                  ┌──────────────────────┐
                                  │ vxd.yaml             │
                                  │ autoresearch:        │
                                  │   enabled: true      │
                                  │   metric: {...}      │
                                  │   editable_paths: [] │
                                  │   gate: winning      │
                                  │   budget: 5m         │
                                  │   parallel: 4        │
                                  └──────────┬───────────┘
                                             │
                                ┌────────────▼───────────┐
                                │ internal/autoresearch/ │
                                │                        │
                                │  Coordinator           │
                                │  ├─ HypothesisBank     │  ◄──── event store
                                │  ├─ BayesSampler       │  ◄──── feedback.go primitives
                                │  ├─ ProgramMDEvolver   │  ◄──── weekly cron
                                │  └─ ExperimentRunner   │
                                │       ├─ TripwireJudge │  (LLM)
                                │       ├─ MetricHarness │  (shell + parse + LLM tiebreak)
                                │       └─ GateRouter    │  (auto / winning / PR)
                                └────────────┬───────────┘
                                             │ uses
       ┌─────────────────────────────────────┼─────────────────────────────────────┐
       ▼                                     ▼                                     ▼
internal/runtime/                     internal/llm/                        internal/state/
  (worktree+tmux+adapter)               (clients, fallback)                  (events.jsonl, sqlite)
```

## Components

### `Coordinator` (`coordinator.go`)
Owns the loop. Reads `vxd.yaml`, holds budget window, dispatches up to `parallel` experiments concurrently.

```go
type Coordinator struct {
    repo     string
    cfg      AutoresearchConfig
    bank     *HypothesisBank
    sampler  *BayesSampler
    runner   *ExperimentRunner
    eventer  state.Eventer
    parallel int
}
func (c *Coordinator) Run(ctx context.Context) error
```

### `HypothesisBank` (`hypothesis.go`)
Pure projection over event store. Returns top-K wins/losses for prompt seeding. No state of its own. Dedupes via diff content-hash so the same experiment doesn't recur.

```go
func (b *HypothesisBank) TopWins(repo string, k int) []Experiment
func (b *HypothesisBank) TopLosses(repo string, k int) []Experiment
func (b *HypothesisBank) SeenDiff(hash string) bool
```

### `BayesSampler` (`sampler.go`)
Wraps existing `internal/improve/feedback.go` Beta-prior primitives. Per-class priors (`refactor`, `perf`, `test`, `simplify`, `feature`, `other`). Thompson-samples next class. Updates posterior on every kept/discarded outcome.

```go
func (s *BayesSampler) Next(repo string) ExperimentClass
func (s *BayesSampler) Update(repo string, class ExperimentClass, kept bool)
```

### `ExperimentRunner` (`runner.go`)
Stateless. Receives a proposal, returns an outcome.

```go
func (r *ExperimentRunner) Run(ctx context.Context, p Proposal) (Outcome, error)
```

Sequence:
1. Create worktree on `autoresearch/exp-{ulid}` branch.
2. Render prompt: template + memory-seed (top wins/losses) + repo `program.md`.
3. Spawn agent in tmux via existing `runtime.Adapter`.
4. Wait until exit or budget timeout. Kill if over.
5. Diff vs `origin/{base}`. If touches non-allowlist path → `EXPERIMENT_TRIPWIRED { scope }`.
6. `MetricHarness.Measure(worktree)`. Emit `EXPERIMENT_MEASURED`.
7. If delta within ε → LLM tiebreak.
8. `TripwireJudge.Judge(diff, delta)`. Suspicious → `DISCARDED`.
9. If improvement and judge OK → `GateRouter.Route(outcome)`. Else cleanup worktree.
10. `BayesSampler.Update(repo, class, kept)`.

### `MetricHarness` (`metric.go`)
Executes user's shell command, parses stdout per declared parser, returns a numeric. Supports parsers:
- `regex` — first capture group, `strconv.ParseFloat`
- `json_path` — `gjson` lookup
- `last_float` — last float on stdout
- `exit_code_inverse` — 1/0 from exit (use for binary checks)

If two scores tie within `tie_epsilon`, calls LLM tiebreaker with rubric from `vxd.yaml`. Tiebreaker output is a signed nudge in [-1, +1]; final score = primary + ε·nudge.

```go
func (h *MetricHarness) Measure(worktree string) (Score, error)
```

### `TripwireJudge` (`tripwire.go`)
Single LLM call. Inputs: diff, metric delta, repo conventions. Returns `Verdict ∈ {OK, SUSPICIOUS, REJECTED}` + reason. Catches:
- Deleted/weakened tests
- Shortened benchmarks / lowered thresholds
- Stubbed-out functions
- Hardcoded "expected" outputs
- Disabled lints/CI

```go
func (j *TripwireJudge) Judge(diff string, delta float64, conv Conventions) (Verdict, error)
```

### `GateRouter` (`gate.go`)
Routes kept experiments per `vxd.yaml` `autoresearch.gate`:
- `auto` → squash-merge to base
- `winning` (default) → fast-forward onto `autoresearch/winning` branch
- `pr` → open PR via existing `internal/git` helpers

Gate writes are serialized through `sync.Mutex` keyed on repo to avoid concurrent fast-forward conflicts.

```go
func (g *GateRouter) Route(o Outcome) error
```

### `ProgramMDEvolver` (`evolver.go`)
Cron-triggered (Sunday 03:00 by default). Reads top-20 wins + top-20 losses, asks LLM to rewrite repo's `program.md`. Opens PR — **never auto-merges**, even under `gate: auto`. Hardcoded human review. PR body includes win/loss summary so the reviewer can sanity-check.

```go
func (e *ProgramMDEvolver) Evolve(ctx context.Context, repo string) (PRURL, error)
```

### CLI (`internal/cli/autoresearch.go`)

| Command | Purpose |
|---|---|
| `vxd autoresearch start <repo> [--budget 1h] [--continuous]` | Start coordinator for a repo |
| `vxd autoresearch stop <repo>` | Graceful stop after current experiments finish |
| `vxd autoresearch status [<repo>]` | Wins, losses, current Bayes posterior, budget remaining |
| `vxd autoresearch hypotheses <repo>` | Top wins/losses with diffs and metric deltas |
| `vxd autoresearch evolve <repo>` | Manual `program.md` rewrite trigger (still PR-gated) |

## Data flow

### Single experiment lifecycle (event sequence)

```
1. EXPERIMENT_PROPOSED  { id, repo, class, prompt_hash, parent_win_hashes[], parent_loss_hashes[] }
2. EXPERIMENT_RUNNING   { id, worktree, session, started_at }
3a. (scope violation)   EXPERIMENT_TRIPWIRED { id, reason: "scope", paths_violated[] }
3b. EXPERIMENT_MEASURED { id, score, baseline, delta, parser_output }
4. (near-tie)           EXPERIMENT_TIEBROKEN { id, nudge, rubric_hash }
5. EXPERIMENT_TRIPWIRED { id, verdict, reason }   // OK verdict still emits this with verdict=OK
6a. EXPERIMENT_KEPT     { id, score, gate_action, branch_or_pr }
6b. EXPERIMENT_DISCARDED { id, reason }
7. (failure tier)       EXPERIMENT_FAILED { id, reason, infra_caused: bool }
```

### Baseline tracking
- `baseline_score` per repo stored in projection table. Recomputed from latest `EXPERIMENT_KEPT` for that repo.
- Initial baseline: harness runs once on `main` HEAD before first experiment, emits `BASELINE_MEASURED`.

### Memory seed determinism
- `parent_*_hashes` in `EXPERIMENT_PROPOSED` records exactly which past experiments seeded the prompt → reproducible audit trail.

### Weekly evolver flow

```
cron sunday 03:00:
  for each enabled repo:
    wins   := bank.TopWins(repo, 20)
    losses := bank.TopLosses(repo, 20)
    new_md := LLM(rewrite_prompt, current=programMD, wins, losses)
    if diff(programMD, new_md) significant:
      open PR "autoresearch: program.md evolution {date}"
      emit PROGRAMMD_EVOLVED { repo, pr_url, kept_for_review: true }
```

## Error handling

Two principles:
1. **Fail-closed on judging** (tripwire, tiebreaker). Never merge unjudged code.
2. **Distinguish infra failures from agent failures** in Bayes updates. Don't poison priors with worktree errors.

Helper: `isAgentCausedFailure(reason) bool` is the single decision point for whether to update Bayes.

| Class | Trigger | Response |
|---|---|---|
| Worktree create fails | Disk, permissions, git lock | `EXPERIMENT_FAILED { infra }`, skip Bayes, back off 30s, retry |
| Tmux session never starts | tmux not on PATH, env conflict | Same. Preflight gates Coordinator startup |
| Agent budget timeout | Slow/stuck LLM | Kill tmux, `DISCARDED { timeout }`, count as Bayes loss (signals class too ambitious) |
| Agent exits, no diff | Agent bailed | `DISCARDED { no_diff }`, count as Bayes loss |
| Diff outside allowlist | Off-leash | `TRIPWIRED { scope }`, count as Bayes loss, retain diff for forensics |
| Metric command non-zero exit | Build broken | `DISCARDED { metric_failed }`. If parser detects "compile error" / agent broke build → Bayes loss. If repo-side infra fault (e.g. missing test fixture) → skip Bayes |
| Metric output unparseable | Parser miss | `DISCARDED { parse_error }`, full output in event, alert via existing email digest |
| Tripwire LLM call fails | Provider down | Retry once via existing fallback chain (Anthropic → Google). If still failing: fail-closed, verdict = `SUSPICIOUS`, discard |
| Tiebreaker LLM fails | Same | Fail-closed: keep older baseline, no change |
| Concurrent gate write | Two finishers same instant | Mutex per-repo on gate routing |
| Evolver malformed markdown | LLM hallucination | PR opens regardless (human-gated); CI lint catches malformed YAML frontmatter |
| Coordinator panic | Bug | Defer-recover at tick top, `COORDINATOR_PANIC { stack }`, sleep 60s, resume. Don't kill daemon |

## Observability

Every experiment event carries `repo_id`, `experiment_id`, `class`, `parent_*_hashes`. `vxd autoresearch status` materializes from event store.

Existing `vxd metrics` extended with autoresearch section:
- Kept rate per class
- Mean delta per class
- Tripwire rate
- Infra failure rate
- Time-to-first-improvement
- Cumulative improvement vs baseline

Existing email digest extended with autoresearch summary block (top wins this week, current posterior, evolver PRs awaiting review).

## Configuration

```yaml
autoresearch:
  enabled: true
  metric:
    command: "go test -bench=. -benchmem -run=^$ ./..."
    parser:
      kind: regex                          # regex | json_path | last_float | exit_code_inverse
      pattern: "BenchmarkFoo-\\d+\\s+\\d+\\s+(\\d+)\\s+ns/op"
      lower_is_better: true
    tie_epsilon: 0.02                      # within 2% → LLM tiebreak
    tiebreak_rubric: |
      Prefer the diff that is simpler, more readable,
      and more idiomatic Go.
  editable_paths:
    - "internal/**/*.go"
    - "cmd/**/*.go"
  forbidden_paths:                         # explicit denylist on top of allowlist
    - "**/*_test.go"
    - ".github/**"
    - "vxd.yaml"
  gate: winning                            # auto | winning | pr
  budget: 5m
  parallel: 4
  continuous: false                        # if true, run back-to-back; if false, scheduled batch only
  schedule:
    nightly:
      enabled: true
      window: "23:00-06:00"
    evolver:
      enabled: true
      cron: "0 3 * * 0"                    # Sunday 03:00
  tripwire:
    model: claude-sonnet-4-20250514        # cheaper-than-Opus default
    fail_closed: true                      # always — exposed for clarity, not flexibility
  bayes:
    classes: [refactor, perf, test, simplify, feature, other]
    prior_alpha: 1.0
    prior_beta: 1.0
```

Config is **per-repo**. Lives in target repo's `vxd.yaml` (autoresearch is opt-in). The controlling VXD instance reads it on coordinator start.

## Events (new)

```go
const (
    EventBaselineMeasured     EventType = "BASELINE_MEASURED"
    EventExperimentProposed   EventType = "EXPERIMENT_PROPOSED"
    EventExperimentRunning    EventType = "EXPERIMENT_RUNNING"
    EventExperimentMeasured   EventType = "EXPERIMENT_MEASURED"
    EventExperimentTiebroken  EventType = "EXPERIMENT_TIEBROKEN"
    EventExperimentTripwired  EventType = "EXPERIMENT_TRIPWIRED"
    EventExperimentKept       EventType = "EXPERIMENT_KEPT"
    EventExperimentDiscarded  EventType = "EXPERIMENT_DISCARDED"
    EventExperimentFailed     EventType = "EXPERIMENT_FAILED"
    EventCoordinatorPanic     EventType = "COORDINATOR_PANIC"
    EventProgrammdEvolved     EventType = "PROGRAMMD_EVOLVED"
)
```

All projected in `internal/state/sqlite.go Project()` switch. Wiring tests required per CLAUDE.md (`engine/wiring_test.go` style: feature is *activated*, not just implemented).

## Testing

### Unit tests (per file, table-driven)
- `hypothesis_test.go` — deterministic projection from synthetic event log; dedupe by hash; top-K ordering correctness.
- `sampler_test.go` — Bayes update math (Beta posterior); Thompson-sample bias respects priors; class-specific updates do not bleed.
- `metric_test.go` — each parser kind given golden stdout; lower_is_better flag flip; tiebreak nudge bounded in [-ε, +ε].
- `tripwire_test.go` — given diffs that delete tests / shorten benchmarks / stub functions, judge returns SUSPICIOUS. LLM call mocked.
- `gate_test.go` — `auto` calls merger, `winning` fast-forwards, `pr` opens PR. Mock git layer.
- `evolver_test.go` — given fixed wins/losses, prompts LLM with both, opens PR; never auto-merges even with `gate: auto`.
- `runner_test.go` — happy path + each error class; verifies correct event sequence emitted; correct Bayes update / skip.
- `coordinator_test.go` — parallel dispatch up to `parallel`, budget window honored, graceful stop drains in-flight experiments.

### Wiring tests (`engine/wiring_test.go`)
- `TestWiring_AutoresearchEvents_Projected` — every new event type present in `sqlite.go Project()` switch (mirrors existing wiring guard).
- `TestWiring_AutoresearchConfig_Loaded` — `vxd config validate` accepts a sample `autoresearch:` block.
- `TestWiring_AutoresearchCLI_Registered` — every subcommand reachable from root cobra command.
- `TestWiring_TripwireFailClosed` — when LLM client always errors, judge returns SUSPICIOUS (not OK), experiment discarded.
- `TestWiring_BayesSkippedOnInfra` — `EXPERIMENT_FAILED { infra_caused: true }` does not call `BayesSampler.Update`.

### Integration tests
- `coordinator_integration_test.go` — full loop on a fixture repo: synthetic worktree, mock LLM client, asserts kept/discarded ratios match injected metric deltas.

### Doc coverage tests
- Existing `engine/doc_coverage_test.go` checks CLAUDE.md and README.md; new commands and config fields must be added to both.

### Coverage target
- 80%+ on new package per project rule.

## Implementation phases

The work decomposes into 6 phases. Each phase is an atomic, testable, rollback-safe step. Wiring tests guard activation at every phase.

### Phase 1 — Events + projection + config schema
- Add 11 new event types to `internal/state/events.go`.
- Wire each into `sqlite.go Project()` switch with explicit handlers (no `default` swallows).
- Add `AutoresearchConfig` struct to `internal/config/config.go` + YAML tags + validation.
- Doc updates: CLAUDE.md events list + README config table.
- Wiring tests: `TestWiring_AutoresearchEvents_Projected`, `TestWiring_AutoresearchConfig_Loaded`.

### Phase 2 — HypothesisBank + BayesSampler
- New package `internal/autoresearch/`. Files: `types.go`, `hypothesis.go`, `sampler.go`, plus tests.
- Reuse `internal/improve/feedback.go` Beta primitives where possible.
- No I/O beyond event-log read (already abstracted).

### Phase 3 — MetricHarness + TripwireJudge
- `metric.go` with 4 parser kinds. Pure functions for parsing; thin shell layer for execution.
- `tripwire.go` LLM call with fail-closed default. Reuse `internal/llm/` clients + fallback.

### Phase 4 — ExperimentRunner + GateRouter
- `runner.go` orchestrates one experiment lifecycle; emits the canonical event sequence.
- `gate.go` routes per config; mutex per-repo for concurrent winning-branch fast-forwards.
- Reuse `internal/runtime/` for worktree+tmux+adapter.
- Reuse `internal/git/` for branch/PR ops.

### Phase 5 — Coordinator + ProgramMDEvolver + CLI
- `coordinator.go` parallel dispatch + budget window + graceful stop.
- `evolver.go` weekly cron handler; PR-only output.
- `internal/cli/autoresearch.go` 5 subcommands wired into cobra root.
- `vxd metrics` + email digest extensions (small, additive).

### Phase 6 — Integration + docs + commit
- Full integration test (fixture repo, mocked LLM, end-to-end loop).
- README + CLAUDE.md updates; doc coverage tests pass.
- All `go test ./...` green; `go build -o ~/.local/bin/vxd ./cmd/vxd` succeeds.
- Commit, push, merge to main.

## Risks & mitigations

| Risk | Likelihood | Mitigation |
|---|---|---|
| Goodhart / metric hacking | High | LLM tripwire + allowlist + fail-closed. Tiebreaker rubric biases toward simplicity. |
| Bayes prior collapse to one class | Medium | `prior_alpha=prior_beta=1.0` keeps all classes alive; classes never excluded, only down-weighted. |
| Worktree disk explosion | Medium | Discarded experiments cleaned immediately; existing `vxd gc` already prunes. |
| Provider quota burnout | Medium | Per-experiment LLM cost capped via existing `internal/llm/` token budgets; tripwire uses cheaper Sonnet by default. |
| Evolver writes unsafe `program.md` | Low | Hardcoded human review gate. Never auto-merged regardless of repo's gate setting. |
| Drift from main while autoresearch runs | Medium | Each experiment rebases on latest base before measure (existing pattern in `internal/git/`). |
| Tmux PATH / env conflicts | Medium | Existing preflight covers it. Preflight gates Coordinator startup. |

## Open questions (deferred to v2)

- Cross-repo learning (priors transferred between similar repos).
- Multi-metric optimization (Pareto front).
- Distributed coordinators (multi-host).
- Adaptive `tie_epsilon` based on observed metric variance.

These are explicitly out of scope for v1. Adding any one would balloon the design.

## Success criteria

- `vxd autoresearch start <repo>` runs N parallel experiments per night and produces measurably better baseline metric within 7 days on at least one tracked repo.
- Zero unjudged merges (every kept experiment passed tripwire).
- < 5% Bayes-poisoning rate (infra failures correctly skipped).
- `program.md` evolver PR opened weekly; never auto-merged.
- All wiring tests green; doc coverage tests green; `go test ./...` green.
