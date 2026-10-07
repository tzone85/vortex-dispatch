#!/usr/bin/env bash
# =============================================================================
# install-automation.sh — wire up the automation suite from the audit.
#
# Idempotent. Run from anywhere. What it does:
#   1. Installs the shared git hooks (green gate, candidates C/D).
#   2. Rebuilds candidate F: retires the broken com.vxd.self-improve launchd
#      job and installs com.vxd.audit-loop (candidate A + G) in its place.
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
	# Render paths as plist values, not shell commands. This works with spaces,
	# quotes and XML characters in a user's home or repository directory.
	python3 - "$NEW_PLIST_SRC" "$NEW_PLIST_DST" "$HOME" "$REPO_DIR" <<'PY'
import os
import plistlib
import sys
import tempfile

source, destination, home, repo = sys.argv[1:]
with open(source, "rb") as stream:
    template = plistlib.load(stream)

def render(value):
    if isinstance(value, str):
        return value.replace("__VXD_HOME__", home).replace("__VXD_REPO__", repo)
    if isinstance(value, list):
        return [render(item) for item in value]
    if isinstance(value, dict):
        return {key: render(item) for key, item in value.items()}
    return value

with tempfile.NamedTemporaryFile(dir=os.path.dirname(destination), delete=False) as stream:
    temporary = stream.name
    plistlib.dump(render(template), stream)
os.replace(temporary, destination)
PY
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
	all)         command -v python3 >/dev/null; install_hooks; install_audit_loop; retire_self_improve ;;
	*) echo "usage: $0 [--hooks|--uninstall]" >&2; exit 2 ;;
esac
echo "done."
