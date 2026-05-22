package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// runReq — dry-run mode (no LLM calls needed)
// ---------------------------------------------------------------------------

func TestRunReq_DryRun(t *testing.T) {
	dir, _ := setupTestEnv(t)

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newReqCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.PersistentFlags().Bool("skip-preflight", true, "")
	cmd.Flags().Set("dry-run", "true")
	cmd.Flags().Set("no-dispatch", "true") // plan-only; resume is tested separately
	cmd.SetArgs([]string{"Add a health check endpoint"})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "DRY RUN") {
		t.Errorf("expected 'DRY RUN' in output, got: %s", output)
	}
	if !strings.Contains(output, "Requirement ID:") {
		t.Errorf("expected 'Requirement ID:' in output, got: %s", output)
	}
}

func TestRunReq_DryRun_WithFile(t *testing.T) {
	dir, _ := setupTestEnv(t)

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	// Create a requirement file
	reqFile := filepath.Join(dir, "requirement.md")
	os.WriteFile(reqFile, []byte("Build a REST API with CRUD operations"), 0644)

	cmd := newReqCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.PersistentFlags().Bool("skip-preflight", true, "")
	cmd.Flags().Set("dry-run", "true")
	cmd.Flags().Set("no-dispatch", "true") // plan-only; resume is tested separately
	cmd.Flags().Set("file", reqFile)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "DRY RUN") {
		t.Errorf("expected 'DRY RUN' in output, got: %s", output)
	}
}

func TestRunReq_DryRun_WithGodmode(t *testing.T) {
	dir, _ := setupTestEnv(t)

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newReqCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.PersistentFlags().Bool("skip-preflight", true, "")
	cmd.Flags().Set("dry-run", "true")
	cmd.Flags().Set("no-dispatch", "true") // plan-only; resume is tested separately
	cmd.Flags().Set("godmode", "true")
	cmd.SetArgs([]string{"Add OAuth login"})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunReq_NoArgNoFile(t *testing.T) {
	dir, _ := setupTestEnv(t)

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newReqCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.PersistentFlags().Bool("skip-preflight", true, "")

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when no arg and no file provided")
	}
}

// TestRunReq_NoDispatch_SkipsAutoDispatch verifies that --no-dispatch stops
// after planning and prints guidance instead of chaining into runResume.
func TestRunReq_NoDispatch_SkipsAutoDispatch(t *testing.T) {
	dir, _ := setupTestEnv(t)

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newReqCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.PersistentFlags().Bool("skip-preflight", true, "")
	cmd.Flags().Set("dry-run", "true")
	cmd.Flags().Set("no-dispatch", "true")
	cmd.SetArgs([]string{"Add a login page"})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	// Should print guidance to run resume, not attempt dispatch.
	if !strings.Contains(output, "vxd resume") {
		t.Errorf("expected 'vxd resume' in --no-dispatch output, got: %s", output)
	}
	// Must NOT contain "starting dispatch" — that indicates auto-dispatch fired.
	if strings.Contains(output, "starting dispatch") {
		t.Errorf("unexpected dispatch message in --no-dispatch output: %s", output)
	}
}

// TestForkReqDaemon_CommandLineInvocation verifies that forkReqDaemon produces
// the expected Cmd without actually forking a process.
func TestForkReqDaemon_CommandLineInvocation(t *testing.T) {
	const self = "/usr/local/bin/vxd"
	const reqID = "01JV7REQID0000000000001"
	const logPath = "/tmp/test.log"

	cmd := forkReqDaemon(self, reqID, logPath, []string{"--godmode"})
	if cmd == nil {
		t.Fatal("forkReqDaemon returned nil")
	}

	args := cmd.Args
	if len(args) < 3 {
		t.Fatalf("expected at least 3 args, got %v", args)
	}
	if args[0] != self {
		t.Errorf("args[0] = %q, want %q", args[0], self)
	}
	if args[1] != "resume" {
		t.Errorf("args[1] = %q, want 'resume'", args[1])
	}
	if args[2] != reqID {
		t.Errorf("args[2] = %q, want %q", args[2], reqID)
	}

	found := false
	for _, a := range args {
		if a == "--godmode" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("--godmode not forwarded to child args: %v", args)
	}

	// SysProcAttr must be set so the child detaches from the parent session.
	if cmd.SysProcAttr == nil {
		t.Error("SysProcAttr is nil — daemon won't detach from parent process group")
	}
}

// TestRunReq_ManualReviewMode_SkipsAutoDispatch verifies that a non-auto
// review_mode (manual or plan_only) causes runReq to stop after planning
// and print approval guidance instead of dispatching.
func TestRunReq_ManualReviewMode_SkipsAutoDispatch(t *testing.T) {
	dir, _ := setupTestEnv(t)

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	// Write a vxd.yaml with manual review mode.
	vxdYaml := filepath.Join(dir, "vxd.yaml")
	os.WriteFile(vxdYaml, []byte("merge:\n  review_mode: manual\n"), 0644)

	cmd := newReqCmd()
	cmd.PersistentFlags().String("config", vxdYaml, "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.PersistentFlags().Bool("skip-preflight", true, "")
	cmd.Flags().Set("dry-run", "true")
	cmd.SetArgs([]string{"Add OAuth support"})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	// Should print plan approval guidance.
	if !strings.Contains(output, "approve-plan") {
		t.Errorf("expected 'approve-plan' in manual review_mode output, got: %s", output)
	}
	// Must NOT say "starting dispatch".
	if strings.Contains(output, "starting dispatch") {
		t.Errorf("unexpected dispatch message in manual review_mode output: %s", output)
	}
}
