#!/usr/bin/env bash
# =============================================================================
# audit-loop.sh — candidate A from the automation audit (highest-ranked loop).
#
# Runs the security/wiring/silent-failure audit that dominates this repo's week
# (24 security + 10 audit + 9 wiring commits over 16 weeks), then opens a PR.
#
# Why this is a LOOP and not a hook (the 4-condition test):
#   1. repeats           — yes, weekly+; the bulk of fix-commits
#   2. rule decides done — yes, via the STOP RULE below (dry rounds + green gate)
#   3. afford waste      — yes: PR-ONLY. A junk finding costs one closed PR.
#   4. has data + tools  — yes: full repo, build/test/lint, git, gh, sub-agents
#
# HARD GUARDRAIL: this loop NEVER merges. It only ever opens a PR for human
# review. That is what keeps condition 3 cheap and the loop safe to schedule.
#
# Usage:
#   tools/audit-loop.sh            # full run: audit -> fix -> open PR
#   tools/audit-loop.sh --dry      # report only; no code changes, no PR
#   tools/audit-loop.sh --force    # run even if main hasn't changed since last
# =============================================================================
set -euo pipefail

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
STATE_DIR="${HOME}/.vxd/audit-loop"
LOG_FILE="${STATE_DIR}/audit-loop.log"
LOCK_DIR="${STATE_DIR}/audit-loop.lock.d"
LAST_SHA_FILE="${STATE_DIR}/last-audited-sha"
BASE_BRANCH="main"
MAX_ROUNDS=3          # stop after this many hunt rounds even if not dry
DRY=0
FORCE=0

for arg in "$@"; do
	case "$arg" in
		--dry)   DRY=1 ;;
		--force) FORCE=1 ;;
		*) echo "unknown arg: $arg" >&2; exit 2 ;;
	esac
done

mkdir -p "$STATE_DIR"

log() { printf '%s  %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*" | tee -a "$LOG_FILE"; }

# Single-flight: never let two audit runs race the same tree. mkdir is atomic
# on every POSIX filesystem (macOS has no flock). Stale lock from a crashed run
# older than 6h is reclaimed.
if [ -d "$LOCK_DIR" ] && [ -n "$(find "$LOCK_DIR" -maxdepth 0 -mmin +360 2>/dev/null)" ]; then
	rmdir "$LOCK_DIR" 2>/dev/null || true
fi
if ! mkdir "$LOCK_DIR" 2>/dev/null; then
	log "another audit-loop run holds the lock; exiting."
	exit 0
fi
trap 'rmdir "$LOCK_DIR" 2>/dev/null || true' EXIT

cd "$REPO_DIR"

# Refuse to audit a dirty tree — we must branch from a clean main.
if [ -n "$(git status --porcelain)" ]; then
	log "ABORT: working tree is dirty. Commit/stash before auditing."
	exit 1
fi

git fetch --quiet origin "$BASE_BRANCH"
git checkout --quiet "$BASE_BRANCH"
git merge --ff-only --quiet "origin/${BASE_BRANCH}" || true
HEAD_SHA="$(git rev-parse HEAD)"

# Skip if main hasn't moved since the last audit (nothing new to find).
if [ "$FORCE" -eq 0 ] && [ -f "$LAST_SHA_FILE" ] && [ "$(cat "$LAST_SHA_FILE")" = "$HEAD_SHA" ]; then
	log "main unchanged since last audit ($HEAD_SHA); nothing to do. Use --force to override."
	exit 0
fi

# Don't audit a red tree — that's a build problem, not an audit finding.
log "running green gate (make verify) before audit ..."
if ! make verify >>"$LOG_FILE" 2>&1; then
	log "ABORT: green gate is RED on $BASE_BRANCH. Fix the build before auditing."
	exit 1
fi
log "green gate passed on $HEAD_SHA."

# Candidate G: weekly CVE scan, folded into this same scheduled run. Non-fatal —
# a known CVE is a finding for the audit to fix, not a reason to skip the audit.
VULN_REPORT=""
log "running govulncheck (G) ..."
if vuln_out="$(make vuln 2>&1)"; then
	log "govulncheck: no known vulnerabilities."
else
	VULN_REPORT="$vuln_out"
	log "govulncheck found advisories (will be fed to the audit):"
	printf '%s\n' "$vuln_out" >>"$LOG_FILE"
fi

# ---------------------------------------------------------------------------
# The audit prompt. Encodes the STOP RULE and the PR-ONLY guardrail so the
# agent's own loop terminates on a checkable condition (condition 2).
# ---------------------------------------------------------------------------
read -r -d '' AUDIT_PROMPT <<PROMPT || true
You are running the VXD weekly audit loop on a clean ${BASE_BRANCH} at ${HEAD_SHA}.

GOAL: find and fix real loopholes — concurrency bugs (locks held across I/O),
silent failures / swallowed errors, event-sourcing wiring gaps (events emitted
but not handled in sqlite.go Project()), and security issues — then open ONE PR.

METHOD (loop-until-dry):
  - Fan out parallel hunters by category. Adversarially verify EVERY candidate
    finding before fixing it — default to "not a real bug" when uncertain.
  - Fix only verified findings. Add or update a test for each fix.
  - Repeat for up to ${MAX_ROUNDS} rounds. STOP early when a round finds nothing
    new (two consecutive dry passes = done).

GREEN GATE (must pass before opening the PR):
  - 'make verify' must succeed: build + go vet + golangci-lint (0 issues) +
    go test ./... + doc-coverage. If you cannot get it green, open the PR as a
    DRAFT and say so in the body.

HARD GUARDRAILS — do not violate:
  - Branch from ${BASE_BRANCH}: fix/audit-loop-<short-date-or-topic>.
  - Open a PR with 'gh pr create'. NEVER merge. NEVER push to ${BASE_BRANCH}.
  - If you find ZERO real issues after the dry rounds, do NOT open a PR. Print
    "AUDIT CLEAN — no PR opened" and stop.
  - Do not touch files outside this repo. Do not exfiltrate secrets.

GOVULNCHECK RESULTS (treat any entry below as a finding to fix — bump the dep
or guard the call path; empty means no known CVEs):
${VULN_REPORT:-none}

Report a short summary of findings (or "clean") at the end.
PROMPT

if [ "$DRY" -eq 1 ]; then
	log "--dry: would run audit with the prompt below. No changes made."
	printf '%s\n' "$AUDIT_PROMPT" | tee -a "$LOG_FILE"
	# Deliberately do NOT record HEAD as audited — a preview must not suppress
	# the next real scheduled run on an unchanged main.
	exit 0
fi

# Use the Claude Max subscription (not metered API credits) and avoid nested
# session errors — mirrors the runtime adapter's documented env handling.
unset ANTHROPIC_API_KEY CLAUDECODE || true

log "invoking claude for the audit pass (PR-only) ..."
# NOTE: per global policy we do NOT pass --dangerously-skip-permissions.
# acceptEdits auto-approves file edits for this trusted, well-defined plan;
# the scoped --allowedTools lets it build/test and open a PR but not much else.
set +e
claude -p "$AUDIT_PROMPT" \
	--permission-mode acceptEdits \
	--allowedTools "Bash,Read,Edit,Write,Grep,Glob,Task" \
	--max-turns 120 \
	>>"$LOG_FILE" 2>&1
rc=$?
set -e

if [ "$rc" -ne 0 ]; then
	log "claude audit run exited non-zero ($rc); see log. Leaving last-sha unchanged so it retries."
	exit "$rc"
fi

# Record the audited SHA so the next scheduled run skips an unchanged main.
echo "$HEAD_SHA" >"$LAST_SHA_FILE"
log "audit pass complete for $HEAD_SHA. Review any open PR before merging."
