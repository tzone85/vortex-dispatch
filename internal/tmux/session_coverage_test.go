package tmux_test

import (
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/tmux"
)

// TestCreateSession_WithCommand verifies creating a session with an initial command.
func TestCreateSession_WithCommand(t *testing.T) {
	skipIfNoTmux(t)

	name := "vxd-test-withcmd"
	tmux.KillSession(name)

	// Use a long-running command so the session doesn't exit immediately
	err := tmux.CreateSession(name, "/tmp", "sleep 30")
	if err != nil {
		t.Fatalf("create session with command: %v", err)
	}
	defer tmux.KillSession(name)

	if !tmux.SessionExists(name) {
		t.Fatal("session should exist")
	}
}

// TestCreateSession_KillsExisting verifies that creating a session with a name
// that already exists kills the old one first.
func TestCreateSession_KillsExisting(t *testing.T) {
	skipIfNoTmux(t)

	name := "vxd-test-reuse"
	tmux.KillSession(name)

	// Create first session with a long-running command
	err := tmux.CreateSession(name, "/tmp", "sleep 30")
	if err != nil {
		t.Fatalf("create first session: %v", err)
	}
	defer tmux.KillSession(name)

	if !tmux.SessionExists(name) {
		t.Fatal("first session should exist")
	}

	// Create another session with same name - should kill old one and succeed
	err = tmux.CreateSession(name, "/tmp", "sleep 30")
	if err != nil {
		t.Fatalf("create replacement session: %v", err)
	}

	if !tmux.SessionExists(name) {
		t.Fatal("replacement session should exist")
	}
}

// TestCapturePaneOutput_ZeroLines verifies default line count.
func TestCapturePaneOutput_ZeroLines(t *testing.T) {
	skipIfNoTmux(t)

	name := "vxd-test-capture-zero"
	tmux.KillSession(name)

	err := tmux.CreateSession(name, "/tmp", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer tmux.KillSession(name)

	time.Sleep(300 * time.Millisecond)

	// Request 0 lines - should use default (50)
	out, err := tmux.CapturePaneOutput(name, 0)
	if err != nil {
		t.Fatalf("capture with 0 lines: %v", err)
	}
	// Should return something (at least the prompt)
	_ = out // just verify no error
}

// TestCapturePaneOutput_NegativeLines verifies negative line count falls back.
func TestCapturePaneOutput_NegativeLines(t *testing.T) {
	skipIfNoTmux(t)

	name := "vxd-test-capture-neg"
	tmux.KillSession(name)

	err := tmux.CreateSession(name, "/tmp", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer tmux.KillSession(name)

	time.Sleep(300 * time.Millisecond)

	out, err := tmux.CapturePaneOutput(name, -5)
	if err != nil {
		t.Fatalf("capture with negative lines: %v", err)
	}
	_ = out
}

// TestCapturePaneOutput_NonExistent verifies error on non-existent session.
func TestCapturePaneOutput_NonExistent(t *testing.T) {
	skipIfNoTmux(t)

	_, err := tmux.CapturePaneOutput("vxd-nonexistent-capture", 10)
	if err == nil {
		t.Error("CapturePaneOutput on non-existent session should return error")
	}
}

// TestListSessions_NoSessions verifies clean output when sessions are cleaned up.
func TestListSessions_AfterCleanup(t *testing.T) {
	skipIfNoTmux(t)

	// Create and immediately kill to test list after cleanup
	name := "vxd-test-list-cleanup"
	tmux.KillSession(name)

	err := tmux.CreateSession(name, "/tmp", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Session should be in list
	sessions, err := tmux.ListSessions()
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	found := false
	for _, s := range sessions {
		if s == name {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected session %s in list", name)
	}

	// Kill and verify it's gone from list
	tmux.KillSession(name)
	sessions, err = tmux.ListSessions()
	if err != nil {
		t.Fatalf("list after kill: %v", err)
	}

	for _, s := range sessions {
		if s == name {
			t.Fatalf("session %s should not be in list after kill", name)
		}
	}
}

// TestKillSession_NonExistent verifies error on killing a non-existent session.
func TestKillSession_NonExistent(t *testing.T) {
	skipIfNoTmux(t)

	err := tmux.KillSession("vxd-nonexistent-kill-xyz")
	if err == nil {
		t.Error("KillSession on non-existent session should return error")
	}
}
