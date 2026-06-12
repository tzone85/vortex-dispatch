package tmux_test

import (
	"strings"
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/tmux"
)

// waitForPaneContains polls CapturePaneOutput until the pane text contains
// substr, or the timeout expires. Returns the most recent output for
// failure messages. Replaces fixed time.Sleep() before capture — those
// sleeps were the source of the long-standing TestSendKeysRaw flake under
// CI load (the shell hadn't rendered the keystrokes before the test
// captured the pane).
func waitForPaneContains(t *testing.T, name, substr string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		out, err := tmux.CapturePaneOutput(name, 10)
		if err == nil {
			last = out
			if strings.Contains(out, substr) {
				return out
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return last
}

// waitForPaneReady polls until the pane shows ANY prompt-like text,
// confirming the session has started. Used in lieu of a 500ms warmup
// sleep — slow CI shells need more time than 500ms, fast laptops need
// less, polling adapts.
func waitForPaneReady(t *testing.T, name string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, err := tmux.CapturePaneOutput(name, 5)
		if err == nil && strings.TrimSpace(out) != "" {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestSendKeysRaw verifies that SendKeysRaw sends text without appending Enter.
func TestSendKeysRaw(t *testing.T) {
	skipIfNoTmux(t)

	name := "vxd-test-sendraw"
	tmux.KillSession(name)

	if err := tmux.CreateSession(name, "/tmp", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer tmux.KillSession(name)

	waitForPaneReady(t, name, 3*time.Second)

	// Send partial text without Enter.
	if err := tmux.SendKeysRaw(name, "partial-text"); err != nil {
		t.Fatalf("send keys raw: %v", err)
	}

	out := waitForPaneContains(t, name, "partial-text", 3*time.Second)
	if !strings.Contains(out, "partial-text") {
		t.Errorf("expected 'partial-text' in output, got: %s", out)
	}
}

// TestSendKeysRaw_ThenEnter verifies raw keys followed by manual Enter produces output.
func TestSendKeysRaw_ThenEnter(t *testing.T) {
	skipIfNoTmux(t)

	name := "vxd-test-sendraw-enter"
	tmux.KillSession(name)

	if err := tmux.CreateSession(name, "/tmp", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer tmux.KillSession(name)

	waitForPaneReady(t, name, 3*time.Second)

	if err := tmux.SendKeysRaw(name, "echo raw-executed"); err != nil {
		t.Fatalf("send keys raw: %v", err)
	}
	if err := tmux.SendKeysRaw(name, "Enter"); err != nil {
		t.Fatalf("send enter: %v", err)
	}

	// The echo command produces "raw-executed" on its own line — wait
	// for it. 5s budget covers slow shells under CI load.
	out := waitForPaneContains(t, name, "raw-executed", 5*time.Second)
	if !strings.Contains(out, "raw-executed") {
		t.Errorf("expected 'raw-executed' in output, got: %s", out)
	}
}
