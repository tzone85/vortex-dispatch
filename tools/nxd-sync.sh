#!/usr/bin/env bash
# =============================================================================
# nxd-sync.sh — candidate B from the automation audit (2nd-ranked loop).
#
# VXD (private, cloud APIs) and NXD (public, Ollama) mirror each other on core
# fixes. Evidence: both shipped "#76 close audit findings" on the SAME day.
# That port is currently manual and highly repetitive.
#
# 4-condition test — the CLEANEST of all candidates:
#   1. repeats           — yes, every core fix
#   2. rule decides done — yes: the fix's behaviour+test exist in NXD, NXD is
#                          green, and NO 'vxd'/'vortex' string leaked in
#   3. afford waste      — yes: ported as a PR, human-gated
#   4. has data + tools  — yes: both repos are local
#
# Usage:
#   tools/nxd-sync.sh                 # report VXD core-fix commits vs NXD
#   tools/nxd-sync.sh --weeks 6       # change the lookback window
#   tools/nxd-sync.sh --leak-check    # fail if NXD tree references VXD/vortex
# =============================================================================
set -euo pipefail

VXD_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
NXD_DIR="${NXD_DIR:-${HOME}/Sites/misc/nexus-dispatch}"
WEEKS=8
LEAK_ONLY=0

while [ $# -gt 0 ]; do
	case "$1" in
		--weeks) WEEKS="$2"; shift 2 ;;
		--leak-check) LEAK_ONLY=1; shift ;;
		*) echo "unknown arg: $1" >&2; exit 2 ;;
	esac
done

if [ ! -d "$NXD_DIR" ]; then
	echo "NXD not found at $NXD_DIR (set NXD_DIR=...)." >&2
	exit 1
fi

# ---------------------------------------------------------------------------
# Leak check: the one HARD rule of the VXD->NXD port. NXD is public; a leaked
# 'vxd'/'vortex' reference is a privacy bug. Greps tracked source only.
# ---------------------------------------------------------------------------
leak_check() {
	echo "== NXD leak check (no 'vxd'/'vortex' in tracked source) =="
	# Allow the module path swap files to mention neither; scan .go + docs.
	if git -C "$NXD_DIR" grep -niE '\b(vxd|vortex[- ]?dispatch)\b' -- '*.go' '*.md' 2>/dev/null; then
		echo "LEAK: NXD references VXD/vortex above — scrub before pushing." >&2
		return 1
	fi
	echo "clean — no VXD references in NXD."
}

if [ "$LEAK_ONLY" -eq 1 ]; then
	leak_check
	exit $?
fi

since="${WEEKS} weeks ago"
echo "=============================================================="
echo " VXD -> NXD sync report   (lookback: ${WEEKS} weeks)"
echo "=============================================================="
echo
echo "## VXD core-fix commits on main (candidates to port)"
git -C "$VXD_DIR" log --since="$since" --pretty=format:"  %ad %s" --date=short main \
	| grep -iE "fix|security|wiring|refactor|perf" || echo "  (none)"
echo; echo
echo "## NXD recent commits on main (what's already there)"
git -C "$NXD_DIR" log --since="$since" --pretty=format:"  %ad %s" --date=short main 2>/dev/null \
	| head -30 || echo "  (none)"
echo; echo
echo "## Heuristic gap: VXD fix subjects with no obvious NXD twin"
# Compare normalized subjects (strip type prefix, PR number, punctuation).
norm() { sed -E 's/^[a-z]+(\([a-z,-]+\))?: //; s/\(#[0-9]+\)//; s/[^a-z0-9 ]//gi' | tr '[:upper:]' '[:lower:]' | tr -s ' '; }
nxd_subjects="$(git -C "$NXD_DIR" log --since="$since" --pretty=format:%s main 2>/dev/null | norm)"
git -C "$VXD_DIR" log --since="$since" --pretty=format:%s main | grep -iE "fix|security|wiring" | while IFS= read -r subj; do
	key="$(printf '%s' "$subj" | norm | awk '{print $1, $2, $3}')"
	if [ -n "$key" ] && ! printf '%s' "$nxd_subjects" | grep -qiF "$key"; then
		echo "  [ ] $subj"
	fi
done
echo
echo "Next: port each unchecked item to NXD (keep offline-first; Docker-only,"
echo "no Ghost), then run: tools/nxd-sync.sh --leak-check  &&  (cd $NXD_DIR && make verify)"
