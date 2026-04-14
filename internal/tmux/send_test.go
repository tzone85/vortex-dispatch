package tmux_test

import (
	"strings"
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/tmux"
)

// TestSendKeysRaw verifies that SendKeysRaw sends text without appending Enter.
func TestSendKeysRaw(t *testing.T) {
	skipIfNoTmux(t)

	name := "vxd-test-sendraw"
	tmux.KillSession(name)

	err := tmux.CreateSession(name, "/tmp", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer tmux.KillSession(name)

	time.Sleep(500 * time.Millisecond)

	// Send partial text without Enter
	err = tmux.SendKeysRaw(name, "partial-text")
	if err != nil {
		t.Fatalf("send keys raw: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	out, err := tmux.CapturePaneOutput(name, 10)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}

	// The text should appear in the pane (as part of the prompt line)
	if !strings.Contains(out, "partial-text") {
		t.Errorf("expected 'partial-text' in output, got: %s", out)
	}
}

// TestSendKeysRaw_ThenEnter verifies raw keys followed by manual Enter produces output.
func TestSendKeysRaw_ThenEnter(t *testing.T) {
	skipIfNoTmux(t)

	name := "vxd-test-sendraw-enter"
	tmux.KillSession(name)

	err := tmux.CreateSession(name, "/tmp", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer tmux.KillSession(name)

	time.Sleep(500 * time.Millisecond)

	// Send command text without Enter
	err = tmux.SendKeysRaw(name, "echo raw-executed")
	if err != nil {
		t.Fatalf("send keys raw: %v", err)
	}

	// Now send Enter separately
	err = tmux.SendKeysRaw(name, "Enter")
	if err != nil {
		t.Fatalf("send enter: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	out, err := tmux.CapturePaneOutput(name, 10)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}

	if !strings.Contains(out, "raw-executed") {
		t.Errorf("expected 'raw-executed' in output, got: %s", out)
	}
}
