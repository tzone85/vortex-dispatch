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
