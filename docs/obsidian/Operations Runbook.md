---
tags: [operations]
---

# Operations Runbook

Day-to-day operation of VXD. For the full command list see [[CLI Commands]];
for settings see [[Configuration]].

## Build & install
```bash
# ALWAYS build to ~/.local/bin (not ~/go/bin)
go build -o ~/.local/bin/vxd ./cmd/vxd
go test ./... -count=1
```
> **PATH shadowing**: if both `~/.local/bin` and `~/go/bin` are on PATH, the
> shell may run a stale binary. `vxd preflight`'s `CheckBinaryPath` warns about
> this. Fix: ensure `~/.local/bin` precedes `~/go/bin`, or `rm ~/go/bin/vxd`.

## Run a requirement
```bash
vxd init                       # one-time workspace setup
vxd req "add OAuth login"      # plans + auto-dispatches when review_mode=auto
vxd status                     # requirement + story status
vxd dashboard --web            # live browser dashboard (see [[Dashboard Authentication]])
```

## Recovery
```bash
vxd resume <req-id>            # resume a paused pipeline (crash-safe)
vxd rebuild                    # rebuild SQLite projection from events.jsonl
vxd preflight                  # 15 pre-flight checks before dispatch
```
`vxd rebuild` is the fix when the projection diverges from the log — see
[[Event Sourcing]].

## Autoresearch spend control
```bash
vxd autoresearch start <repo> --max-experiments 50   # hard cap on spend
vxd autoresearch status <repo>                       # wins/losses, budget
vxd autoresearch stop <repo>                          # drain and stop
```
A consecutive-failure circuit breaker (default 10) stops runaway loops even
without a cap.

## Common gotchas
- `unset ANTHROPIC_API_KEY` if using the Claude CLI subscription (otherwise it
  burns API credits).
- Set `GOOGLE_AI_API_KEY` if a Gemma execution role is configured.
- See [[Debugging Checklist]] for symptom-based troubleshooting.
