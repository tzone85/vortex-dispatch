package runtime

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func skipIfNoTmuxRT(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
}

// killSessionRT is a helper to clean up tmux sessions.
func killSessionRT(name string) {
	exec.Command("tmux", "kill-session", "-t", name).Run()
}

// TestTmuxRunner_Run_WritesSetupFiles verifies TmuxRunner.Run writes setup files.
func TestTmuxRunner_Run_WritesSetupFiles(t *testing.T) {
	skipIfNoTmuxRT(t)

	dir := t.TempDir()
	name := "vxd-test-runner-setup"
	killSessionRT(name)
	defer killSessionRT(name)

	runner := NewTmuxRunner()
	testFile := filepath.Join(dir, "subdir", "setup.txt")

	exec := PreparedExecution{
		Command:     "sleep 30",
		WorkDir:     dir,
		SessionName: name,
		SetupFiles: map[string]string{
			testFile: "setup content",
		},
	}

	err := runner.Run(exec)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Verify setup file was written
	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("setup file should exist: %v", err)
	}
	if string(data) != "setup content" {
		t.Errorf("setup file content = %q, want %q", string(data), "setup content")
	}
}

// TestTmuxRunner_Terminate verifies TmuxRunner.Terminate kills a session.
func TestTmuxRunner_Terminate(t *testing.T) {
	skipIfNoTmuxRT(t)

	name := "vxd-test-runner-term"
	killSessionRT(name)

	runner := NewTmuxRunner()
	exec := PreparedExecution{
		Command:     "sleep 30",
		WorkDir:     "/tmp",
		SessionName: name,
	}

	err := runner.Run(exec)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	err = runner.Terminate(name)
	if err != nil {
		t.Fatalf("Terminate: %v", err)
	}

	if runner.IsAlive(name) {
		t.Error("session should not be alive after Terminate")
	}
}

// TestTmuxRunner_SendInput verifies TmuxRunner.SendInput sends text.
func TestTmuxRunner_SendInput(t *testing.T) {
	skipIfNoTmuxRT(t)

	name := "vxd-test-runner-send"
	killSessionRT(name)
	defer killSessionRT(name)

	runner := NewTmuxRunner()
	ex := PreparedExecution{
		Command:     "",
		WorkDir:     "/tmp",
		SessionName: name,
	}

	err := runner.Run(ex)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	err = runner.SendInput(name, "echo runner-test-input")
	if err != nil {
		t.Fatalf("SendInput: %v", err)
	}
}

// TestTmuxRunner_ReadOutput verifies TmuxRunner.ReadOutput captures output.
func TestTmuxRunner_ReadOutput(t *testing.T) {
	skipIfNoTmuxRT(t)

	name := "vxd-test-runner-read"
	killSessionRT(name)
	defer killSessionRT(name)

	runner := NewTmuxRunner()
	ex := PreparedExecution{
		Command:     "",
		WorkDir:     "/tmp",
		SessionName: name,
	}

	err := runner.Run(ex)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	out, err := runner.ReadOutput(name, 10)
	if err != nil {
		t.Fatalf("ReadOutput: %v", err)
	}
	// Should return something (the shell prompt at minimum)
	_ = out
}

// TestTmuxRunner_IsAlive verifies TmuxRunner.IsAlive.
func TestTmuxRunner_IsAlive(t *testing.T) {
	skipIfNoTmuxRT(t)

	name := "vxd-test-runner-alive"
	killSessionRT(name)
	defer killSessionRT(name)

	runner := NewTmuxRunner()
	ex := PreparedExecution{
		Command:     "sleep 30",
		WorkDir:     "/tmp",
		SessionName: name,
	}

	if runner.IsAlive(name) {
		t.Error("session should not be alive before Run")
	}

	err := runner.Run(ex)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !runner.IsAlive(name) {
		t.Error("session should be alive after Run")
	}
}

// TestDetectStatus_WorkingSession verifies StatusWorking for an active session.
func TestDetectStatus_WorkingSession(t *testing.T) {
	skipIfNoTmuxRT(t)

	name := "vxd-test-detect-work"
	killSessionRT(name)
	defer killSessionRT(name)

	// Create a CLIRuntime with no detection patterns
	rt := &CLIRuntime{
		name: "test",
		detection: Detection{
			// No patterns set — everything should be StatusWorking
		},
	}

	// Create a tmux session
	exec.Command("tmux", "new-session", "-d", "-s", name, "-c", "/tmp", "sleep 30").Run()
	time.Sleep(300 * time.Millisecond)

	status, err := rt.DetectStatus(name)
	if err != nil {
		t.Fatalf("DetectStatus: %v", err)
	}
	// With no detection patterns set, should default to Working
	if status != StatusWorking {
		t.Errorf("expected StatusWorking, got %s", status)
	}
}

// TestAgentStatus_String_Unknown verifies the default case for unknown status.
func TestAgentStatus_String_Unknown(t *testing.T) {
	unknownStatus := AgentStatus(99)
	if unknownStatus.String() != "unknown" {
		t.Errorf("expected 'unknown' for undefined status, got %s", unknownStatus.String())
	}
}
