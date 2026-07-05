package preflight

import (
	"errors"
	"strings"
	"testing"
)

func TestCheckTmuxServer_ProbeSucceeds(t *testing.T) {
	var calls [][]string
	run := func(args ...string) ([]byte, error) {
		calls = append(calls, args)
		return nil, nil
	}
	lookPath := func(string) (string, error) { return "/usr/bin/tmux", nil }

	r := CheckTmuxServer(lookPath, run)

	if r.Name != "tmux_server" {
		t.Errorf("Name = %q, want tmux_server", r.Name)
	}
	if !r.Passed {
		t.Errorf("Passed = false, want true (message: %s)", r.Message)
	}
	if len(calls) == 0 || calls[0][0] != "new-session" {
		t.Fatalf("expected a new-session probe, got calls: %v", calls)
	}
	// The probe session must be cleaned up (best-effort kill-session).
	last := calls[len(calls)-1]
	if last[0] != "kill-session" {
		t.Errorf("expected a kill-session cleanup as the final call, got %v", last)
	}
}

func TestCheckTmuxServer_ProbeFails(t *testing.T) {
	run := func(args ...string) ([]byte, error) {
		if args[0] == "new-session" {
			return []byte("error connecting to /tmp/tmux-501/default (Permission denied)"), errors.New("exit status 1")
		}
		return nil, nil
	}
	lookPath := func(string) (string, error) { return "/usr/bin/tmux", nil }

	r := CheckTmuxServer(lookPath, run)

	if r.Passed {
		t.Errorf("Passed = true, want false")
	}
	if r.Severity != SeverityWarning {
		t.Errorf("Severity = %v, want SeverityWarning", r.Severity)
	}
	if !strings.Contains(r.Message, "Permission denied") {
		t.Errorf("Message %q should carry the tmux error output for diagnosis", r.Message)
	}
}

// TestCheckTmuxServer_BinaryMissingSkips guards against double-reporting: the
// CRITICAL CheckTmux already fails when the binary is absent, so the server
// probe must step aside (pass with a skip note) rather than pile on.
func TestCheckTmuxServer_BinaryMissingSkips(t *testing.T) {
	lookPath := func(string) (string, error) { return "", errors.New("not found") }
	run := func(args ...string) ([]byte, error) {
		t.Fatal("probe must not run when tmux binary is missing")
		return nil, nil
	}

	r := CheckTmuxServer(lookPath, run)

	if !r.Passed {
		t.Errorf("Passed = false, want true (skip, not failure — CheckTmux owns the missing-binary failure)")
	}
	if !strings.Contains(r.Message, "skipped") {
		t.Errorf("Message %q should say the probe was skipped", r.Message)
	}
}
