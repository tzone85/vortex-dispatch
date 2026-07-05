package cli

import (
	"bytes"
	"strings"
	"testing"
)

// Execute is the production main-entry. We can't run it with real argv
// without polluting test state, but we CAN call it with --help to drive
// the cobra usage code path.
func TestExecute_HelpExits(t *testing.T) {
	prev := rootCmd
	defer func() { rootCmd = prev }()

	// Drive rootCmd with --help so cobra renders usage and returns nil.
	rootCmd.SetArgs([]string{"--help"})
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	if err := Execute(); err != nil {
		t.Errorf("Execute --help should not error, got: %v", err)
	}
}

func TestNewAutoresearchStopCmd_PrintsDrainMessage(t *testing.T) {
	cmd := newAutoresearchStopCmd()
	cmd.SetArgs([]string{"my-repo"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out.String(), "my-repo") || !strings.Contains(out.String(), "drain") {
		t.Errorf("expected 'my-repo' and 'drain' in stdout; got: %s", out.String())
	}
}

// TestNewAutoresearchEvolveCmd_RequiresConfig pins the wired evolve command's
// early-error path (the v1 stub used to print a placeholder; evolve now drives
// ProgramMDEvolver, so without a readable vxd.yaml it must fail cleanly and
// name the config rather than proceed to LLM/event-store setup).
func TestNewAutoresearchEvolveCmd_RequiresConfig(t *testing.T) {
	cmd := newAutoresearchEvolveCmd()
	cmd.SetArgs([]string{t.TempDir()})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when vxd.yaml is not readable")
	}
	if !strings.Contains(err.Error(), "config") {
		t.Errorf("error should name the config problem; got: %v", err)
	}
}

func TestNewAutoresearchStartCmd_DryRun(t *testing.T) {
	cmd := newAutoresearchStartCmd()
	if err := cmd.Flags().Set("dry-run", "true"); err != nil {
		t.Fatalf("set dry-run: %v", err)
	}
	cmd.SetArgs([]string{"my-repo"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	// dry-run path returns nil before exercising the live coordinator;
	// a real-config dependency is fine to bubble up since the test
	// directory has no vxd.yaml.
	err := cmd.Execute()
	if err != nil && !strings.Contains(err.Error(), "config") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNewAutoresearchHypothesesCmd_OnEmptyStore(t *testing.T) {
	dir := t.TempDir()
	withStateDir(t, dir)

	cmd := newAutoresearchHypothesesCmd()
	cmd.SetArgs([]string{"any-repo"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Errorf("execute: %v", err)
	}
}

func TestNewAutoresearchStatusCmd_OnEmptyStore(t *testing.T) {
	dir := t.TempDir()
	withStateDir(t, dir)

	cmd := newAutoresearchStatusCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Errorf("execute: %v", err)
	}
	// Empty store should still report "(all repos)" label.
	if !strings.Contains(out.String(), "(all repos)") {
		t.Errorf("expected label '(all repos)' in stdout; got: %s", out.String())
	}
}
