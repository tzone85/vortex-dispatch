# Autoresearch Harness — Architecture Decisions

**Source session:** 2026-05-02 → 2026-05-04
**Spec:** docs/superpowers/specs/2026-05-02-autoresearch-harness-design.md
**Code:** internal/autoresearch/, internal/cli/autoresearch.go

## What it is
Generic, sustainable, self-improving experiment harness modeled on
karpathy/autoresearch. Runs the loop hypothesize → edit → measure →
keep/discard → iterate against any tracked repo with a pluggable metric.

## Locked decisions and why

| Question | Choice | Why |
|---|---|---|
| Interpretation | C — generic harness for any tracked repo | Beats literal port (would need GPU) and beats absorbing into self-improvement engine (different loop semantics) |
| Metric | D — numeric primary + LLM tiebreak on near-tie | Pure-numeric is too brittle for code quality; pure-LLM is too noisy and expensive. Hybrid keeps the discipline and lets LLM resolve ties. |
| Concurrency | B — parallel via VXD worktree+tmux | Throughput > strict comparability. Karpathy's serial discipline was abandoned by choice; the tradeoff is wall-clock drift. |
| Sustainability | D — layered: memory + Bayesian + program.md auto-evolution | Each layer is independently useful; stacking gives true self-improvement without runaway risk. Last layer is human-gated. |
| Edit scope | D — allowlist hard floor + LLM tripwire | Allowlist is cheap and deterministic. Tripwire catches the classic Goodhart case (deleted tests, shortened benchmarks). |
| Acceptance gate | D — per-repo configurable, default winning-branch | Matches autoresearch overnight-batch pattern + existing weekly-digest infra. |
| Time budget | D — scheduled batch default, continuous opt-in | Reuses existing 6am cron rhythm. No new scheduler. |
| Code location | Option 3 — new package, shared infra | Loop logic genuinely distinct from improve/; reusing runtime, llm, git, state avoids 3k LOC duplication. |

## Two principles baked into error handling
1. Fail-closed on judging — tripwire/tiebreaker errors yield SUSPICIOUS, never OK. False-discard costs one re-experiment; Goodhart-merge costs forever.
2. Distinguish infra from agent failures in Bayes updates. IsAgentCausedFailure(reason, infraCaused) is the single decision point. Don't poison priors with worktree/tmux errors.

## Components and their files
- coordinator.go — wave dispatcher with parallel goroutines + drain on Stop
- runner.go — one experiment lifecycle; emits the canonical event sequence
- hypothesis.go — pure projection over event log; dedupes by diff content-hash
- sampler.go — Beta priors per (repo, class) + Thompson sampling via Marsaglia gamma draws
- metric.go — 4 parsers (regex, json_path, last_float, exit_code_inverse) + LLM tiebreaker
- tripwire.go — single-line VERDICT|REASON LLM judge; fail-closed on any parse ambiguity
- gate.go — auto / winning / pr routing with per-repo mutex
- evolver.go — weekly program.md rewrite; HARDCODED human review (never auto-merges)
- driver.go — LiveAgentDriver wrapping internal/runtime; auto-commits leftover edits
- worktree_ops.go — DefaultWorktreeOps wrapping internal/git
- workspace_writer.go — ephemeral worktree → write → commit → cleanup, used by evolver

## Integration points
- 11 new event types in internal/state/events.go, all explicitly handled in sqlite.go Project()
- AutoresearchConfig in internal/config/config.go with full validation
- 5 cobra subcommands in internal/cli/autoresearch.go
- git.FastForwardLocal added for the winning-branch gate

## Current placeholders / v2 levers
- baselineFromConfig in internal/cli/autoresearch.go returns fixed 0; v2 routine scheduled 2026-05-10 to replace with RemeasuringBaseline (re-measure metric on origin/main HEAD with TTL cache)
- Cross-repo Bayes prior transfer
- Multi-metric optimization (Pareto front)
- Distributed coordinators
