package preflight

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// CheckTmuxServer verifies the tmux server can actually create sessions —
// distinct from CheckTmux, which only proves the binary exists. A present
// binary with a broken server (stale socket, unwritable TMUX_TMPDIR, nested
// tmux misconfiguration) previously surfaced only when the executor's first
// agent spawn failed mid-dispatch (audit finding O-06). lookPath and run are
// injected for unit testing; pass exec.LookPath and RunTmux for the real host.
func CheckTmuxServer(lookPath func(string) (string, error), run func(args ...string) ([]byte, error)) Result {
	if _, err := lookPath("tmux"); err != nil {
		// CheckTmux (CRITICAL) owns the missing-binary failure; don't pile on.
		return Result{Name: "tmux_server", Severity: SeverityWarning, Passed: true,
			Message: "tmux binary not found — server probe skipped (see tmux check)"}
	}

	probe := fmt.Sprintf("vxd-preflight-%d", os.Getpid())
	out, err := run("new-session", "-d", "-s", probe, "true")
	// Best-effort cleanup: the probe command exits immediately so the session
	// is usually gone already; a "session not found" error here is expected.
	defer func() { _, _ = run("kill-session", "-t", probe) }()
	if err != nil {
		return Result{Name: "tmux_server", Severity: SeverityWarning, Passed: false,
			Message: fmt.Sprintf("tmux server cannot create sessions — agent spawns will fail: %s",
				strings.TrimSpace(string(out)))}
	}
	return Result{Name: "tmux_server", Severity: SeverityWarning, Passed: true,
		Message: "tmux server healthy (probe session created)"}
}

// RunTmux executes tmux with the given args, returning combined output for
// diagnostics. This is the production runner for CheckTmuxServer.
func RunTmux(args ...string) ([]byte, error) {
	return exec.Command("tmux", args...).CombinedOutput()
}
