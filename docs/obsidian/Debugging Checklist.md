---
tags: [operations, troubleshooting]
---

# Debugging Checklist

Symptom → likely cause → fix. Mirrors the checklist in `CLAUDE.md`.

| Symptom | Likely cause | Action |
|---------|-------------|--------|
| Stories stuck in draft after escalation | SLA breach killing them on resume | Check SLA; reset `escalation_tier` in SQLite if needed. See [[Escalation Chain]]. |
| Review keeps rejecting valid work | `gitDiff()` merge-base wrong (repo uses `master` not `main`) | `cd <worktree> && git diff origin/<branch>...HEAD --stat` |
| Self-improve findings all aborted | `--max-turns`, env vars, or non-actionable findings | Check `internal/improve/implementer.go` |
| Email never sends (`email_sent: false`) | `RESEND_API_KEY` unset, or summary re-saved after email phase | Verify env + save order |
| Agent produced work but diff empty | Agent didn't commit | `cd <worktree> && git status`; `autoCommit()` runs post-execution |
| `CLAUDE.md` overwritten after merge | `stripVXDArtifactsFromBranch` ran too late | Verify it precedes `rebaseAndMerge`; `git log --oneline --diff-filter=M -- CLAUDE.md` |
| Code on GitHub but not local | `pullMainAfterMerge` failed (dirty tree / network) | `git pull --ff-only origin main` |
| Projection looks stale / wrong | Log↔projection divergence (crash, swallowed error) | `vxd rebuild` — see [[Event Sourcing]] |
| New feature/fix seems absent after rebuild | PATH shadowing (`~/go/bin/vxd`) | `which vxd` must be `~/.local/bin/vxd`; see [[Operations Runbook]] |

## Useful inspection
- Which state dir a running dashboard uses: `lsof -p <PID> | grep events.jsonl`
- Daemon log captured by `vxd req --background`: `vxd logs <req-id>`
- Event history: `vxd events`, escalations: `vxd escalations`

## Related
- [[Operations Runbook]] · [[CLI Commands]]
