# Autoresearch v1 Placeholders and v2 Levers

Tracked here so the next agent doesn't accidentally reinvent or skip.

## v1 placeholders (intentional simplifications)

### Fixed baseline (HIGH PRIORITY — scheduled)
File: internal/cli/autoresearch.go
Function: baselineFromConfig
Status: Returns 0; runner uses signedDelta(score, baseline=0)
Why: Re-measuring on origin/main HEAD between experiments needs a clean
worktree, TTL cache, and fail-soft semantics. Deferred to v2 to keep PR scope
manageable.
Replacement: scheduled remote agent on 2026-05-10T07:00:00Z UTC will open
PR with internal/autoresearch/baseline.go and a RemeasuringBaseline.
Routine ID: trig_01SpGAAg3GtvrXwoGWAf3bV9
Spec: docs/superpowers/specs/2026-05-02-autoresearch-harness-design.md
("Open questions" section)

### autoCommit message is generic
File: internal/autoresearch/driver.go
Detail: All auto-commits use "autoresearch: agent edits". Future improvement:
include the agent's last message or a one-line summary derived from the diff.
Not load-bearing — hash-dedup is on diff content, not message.

### MergePR is a stub for "auto" gate
File: internal/autoresearch/gate.go
DefaultGateOps.MergePR returns "not implemented" — auto gate currently goes
through routePR and only stops at the merge step. Production users default to
"winning" gate which doesn't hit this path. If "auto" is needed, parse PR URL
to number and call internal/git.MergePR(repoDir, prNumber).

## v2 levers (open questions in spec)

- Cross-repo Bayes prior transfer (priors learned on similar repos seed new ones)
- Multi-metric Pareto-front optimization
- Distributed coordinators (multi-host)
- Adaptive tie_epsilon based on observed metric variance

## Things explicitly OUT of scope
- LLM training (karpathy's train.py) — the harness is generic, not a port
- Replacing internal/improve/ — autoresearch is a sibling, not a successor
- GPU support — single-host CPU is the target

## How to know if a "v2 lever" is ready to land
1. Spec has a one-paragraph design entry
2. There's a wiring test for the activation path
3. Doc-coverage tests still pass (CLAUDE.md + README updated)
4. Existing tests still pass (no regressions in autoresearch package)
5. Binary rebuilds at ~/.local/bin/vxd

If any of those are missing, the lever is not ready.
