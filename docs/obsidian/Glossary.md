---
tags: [reference]
---

# Glossary

Terms used across the VXD vault.

- **Requirement** — a plain-language feature request submitted with `vxd req`.
  Decomposed into stories.
- **Story** — the unit of work an agent implements. Has a complexity (Fibonacci),
  a validated ID, acceptance criteria, and `depends_on` edges.
- **Tier** — a rung on the [[Escalation Chain]] (0 retry → 4 human pause).
- **Wave** — a batch of ready (dependency-satisfied) stories dispatched together.
- **Worktree** — an isolated git working directory per story (shared objects).
- **Projection** — the SQLite-materialized view of the [[Event Sourcing|event log]].
- **Adapter / Runner** — pure command-builder vs. I/O executor. See
  [[Runtime and Adapters]].
- **Gate** — a human or policy checkpoint (`review_mode`, autoresearch `gate`).
- **devdb** — an ephemeral per-story Postgres database (Docker or Ghost provider).
- **Tripwire** — an LLM judge in autoresearch that flags suspicious experiment
  diffs (e.g. weakened tests).
- **Circuit breaker** — the consecutive-failure stop on the autoresearch loop.
- **VXD vs NXD** — cloud-LLM (private) vs Ollama-based offline (public) editions.
  See [[What is VXD]].

## Related
- [[VXD MOC]] — the index of all notes.
