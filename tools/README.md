# VXD automation suite

Built from a workspace + history audit that scored every recurring task against
the 4-condition automation test:

1. **Repeats** — often enough to be worth automating?
2. **Rule decides done** — is there a checkable success criterion, not a judgment call?
3. **Afford wasted runs** — are failures cheap and reversible?
4. **Has data + tools** — can the agent reach the inputs and capabilities it needs?

A task is only wired as an autonomous **loop** when all four are YES. Tasks that
pass but need no judgment become **hooks**. Tasks that fail a condition are left
manual — automating them would be a liability, not a help.

| Verdict | Task | Mechanism | Why |
|---|---|---|---|
| **Loop** | A — security/wiring/silent-failure audit → fix → PR | `audit-loop.sh` + weekly launchd | Dominates the week; all 4 pass (condition 2 via the stop rule) |
| **Loop** | B — VXD→NXD core-fix sync | `nxd-sync.sh` | Cleanest rule-decidability of any candidate |
| **Hook** | C/D — doc-coverage + green gate | `.githooks/pre-push` → `make verify` | Pure rule, exit code — no judgment, so a hook not a loop |
| **Schedule** | G — govulncheck CVE scan | folded into `audit-loop.sh` | Episodic; rides A's weekly cadence |
| **Rebuilt** | F — self-improvement research | retired → replaced by A | Failed #2 (subjective "actionable") + #4 (scraped news, not code signal): 0 findings in a month |
| **Manual** | H — Sanlam day-job (PPE/Jira/docs) | *intentionally none* | Fails #3: prod fintech + POPIA — can't afford wasted runs |

## Files

| Path | Role |
|---|---|
| `audit-loop.sh` | Candidate A. `--dry` (report only), `--force` (ignore unchanged-main skip). **PR-ONLY — never merges.** |
| `nxd-sync.sh` | Candidate B. `--weeks N`, `--leak-check` (fails if NXD references VXD/vortex). |
| `launchd/com.vxd.audit-loop.plist` | Weekly Mon 06:00 schedule for A+G (the rebuilt F slot). |
| `install-automation.sh` | Wires hooks + retires self-improve + loads audit-loop. `--hooks`, `--uninstall`. |
| `../Makefile` | `make verify` (green gate), `make vuln` (G), `make doc-coverage`, `make hooks`. |

## Install

```bash
tools/install-automation.sh        # hooks + rebuild F (retire self-improve, load audit-loop)
tools/audit-loop.sh --dry          # preview the next audit run safely
```

## Guardrails

- **Loop A never merges.** It opens a PR for human review — that is what keeps
  condition 3 (afford waste) cheap: a bad finding costs one closed PR.
- The loop **skips a clean, unchanged `main`** and **refuses to audit a red or
  dirty tree**, so it never opens noise PRs.
- Per global policy, the loop does **not** use `--dangerously-skip-permissions`;
  it runs Claude in `acceptEdits` mode with a scoped tool allowlist.
