#!/usr/bin/env bash
# =============================================================================
# install-automation.sh — wire up the automation suite from the audit.
#
# Idempotent. Run from anywhere. What it does:
#   1. Installs the shared git hooks (green gate, candidates C/D).
#   2. Rebuilds candidate F: retires the broken com.vxd.self-improve launchd
#      job and installs com.vxd.audit-loop (candidate A + G) in its place.
#
# It does NOT touch candidate H (Sanlam day-job) — deliberately left manual:
# it fails condition 3 (prod fintech / POPIA — can't afford wasted runs).
#
# Usage:
#   tools/install-automation.sh           # install everything
#   tools/install-automation.sh --hooks   # just the git hooks
#   tools/install-automation.sh --uninstall
# =============================================================================
set -euo pipefail

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LA_DIR="${HOME}/Library/LaunchAgents"
OLD_LABEL="com.vxd.self-improve"
NEW_LABEL="com.vxd.audit-loop"
NEW_PLIST_SRC="${REPO_DIR}/tools/launchd/${NEW_LABEL}.plist"
NEW_PLIST_DST="${LA_DIR}/${NEW_LABEL}.plist"

install_hooks() {
	echo "== git hooks (C/D) =="
	git -C "$REPO_DIR" config core.hooksPath .githooks
	echo "   core.hooksPath -> .githooks (pre-push green gate active)"
}

retire_self_improve() {
	echo "== retiring ${OLD_LABEL} (rebuild of candidate F) =="
	local old="${LA_DIR}/${OLD_LABEL}.plist"
	if [ -f "$old" ]; then
		launchctl unload "$old" 2>/dev/null || true
		mv "$old" "${old}.retired-$(date +%Y%m%d)"
		echo "   unloaded + backed up old news-scraper job"
	else
		echo "   (no ${OLD_LABEL} found — already retired)"
	fi
}

install_audit_loop() {
	echo "== installing ${NEW_LABEL} (A + G, weekly Mon 06:00) =="
	mkdir -p "${HOME}/.vxd/audit-loop" "$LA_DIR"
	cp "$NEW_PLIST_SRC" "$NEW_PLIST_DST"
	launchctl unload "$NEW_PLIST_DST" 2>/dev/null || true
	launchctl load "$NEW_PLIST_DST"
	echo "   loaded. Verify:  launchctl list | grep ${NEW_LABEL}"
	echo "   Dry-run now:     ${REPO_DIR}/tools/audit-loop.sh --dry"
}

uninstall() {
	echo "== uninstalling audit-loop =="
	[ -f "$NEW_PLIST_DST" ] && launchctl unload "$NEW_PLIST_DST" 2>/dev/null || true
	rm -f "$NEW_PLIST_DST"
	git -C "$REPO_DIR" config --unset core.hooksPath 2>/dev/null || true
	echo "   removed launchd job + reset hooksPath. (self-improve backup left intact)"
}

case "${1:-all}" in
	--hooks)     install_hooks ;;
	--uninstall) uninstall ;;
	all)         install_hooks; retire_self_improve; install_audit_loop ;;
	*) echo "usage: $0 [--hooks|--uninstall]" >&2; exit 2 ;;
esac
echo "done."
