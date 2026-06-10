---
tags: [overview]
---

# What is VXD

**Vortex Dispatch (VXD)** is an AI agent orchestration system. You give it a
requirement in plain language; it produces merged pull requests.

Under the hood it:
1. Uses a **Tech Lead** LLM to decompose the requirement into stories with
   Fibonacci complexity estimates.
2. **Dispatches** each story to an agent sized to its complexity (junior /
   intermediate / senior), each in its own git worktree and tmux session.
3. **Monitors** agents, runs **code review** and **QA**, then **merges** the PR.
4. Recovers from failures through a 5-tier [[Escalation Chain]].

## Why the design is the way it is
- **Event sourcing** (see [[Event Sourcing]]) gives a full audit trail and replay.
- **Worktrees + tmux** isolate parallel agents and let them survive a monitor crash.
- **Adapter/Runner split** keeps command-building pure and testable while the
  execution backend (tmux, Docker, SSH) stays swappable — see [[Runtime and Adapters]].

## Two editions
| | VXD | NXD |
|---|---|---|
| LLM | Cloud (Anthropic + Google AI) | Ollama (offline-first) |
| Repo | `tzone85/vortex-dispatch` (private) | `tzone85/nexus-dispatch` (public) |
| Binary | `~/.local/bin/vxd` | `~/.local/bin/nxd` |

See [[Pipeline Flow]] for the end-to-end sequence and [[Architecture Overview]]
for the package layout.
