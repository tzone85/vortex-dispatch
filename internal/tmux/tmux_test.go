package tmux_test

import (
	"strings"
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/tmux"
)

func skipIfNoTmux(t *testing.T) {
	t.Helper()
	if !tmux.Available() {
		t.Skip("tmux not installed")
	}
}

func TestAvailable(t *testing.T) {
	// Just verify it doesn't panic.
	_ = tmux.Available()
}

func TestCreateAndKillSession(t *testing.T) {
	skipIfNoTmux(t)

	name := "vxd-test-session"
	// Cleanup in case of a previous failed test.
	tmux.KillSession(name)

	err := tmux.CreateSession(name, "/tmp", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer tmux.KillSession(name)

	if !tmux.SessionExists(name) {
		t.Fatal("session should exist")
	}

	err = tmux.KillSession(name)
	if err != nil {
		t.Fatalf("kill session: %v", err)
	}

	if tmux.SessionExists(name) {
		t.Fatal("session should not exist after kill")
	}
}

func TestListSessions(t *testing.T) {
	skipIfNoTmux(t)

	name := "vxd-test-list"
	tmux.KillSession(name)

	err := tmux.CreateSession(name, "/tmp", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer tmux.KillSession(name)

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
		t.Fatalf("expected session %s in list %v", name, sessions)
	}
}

func TestSendKeysAndCapture(t *testing.T) {
	skipIfNoTmux(t)

	name := "vxd-test-capture"
	tmux.KillSession(name)

	err := tmux.CreateSession(name, "/tmp", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer tmux.KillSession(name)

	waitForPaneReady(t, name, 3*time.Second)

	if err := tmux.SendKeys(name, "echo hello-vxd"); err != nil {
		t.Fatalf("send keys: %v", err)
	}

	// Poll until echo has run instead of guessing how long it takes.
	out := waitForPaneContains(t, name, "hello-vxd", 5*time.Second)
	if !strings.Contains(out, "hello-vxd") {
		t.Fatalf("expected 'hello-vxd' in output, got: %s", out)
	}
}

func TestSessionExists_NonExistent(t *testing.T) {
	skipIfNoTmux(t)

	if tmux.SessionExists("vxd-nonexistent-session-xyz") {
		t.Fatal("should not exist")
	}
}
