package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// runMemory — various flag combinations
// ---------------------------------------------------------------------------

func TestRunMemory_NoWebFlag(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })

	cmd := newMemoryCmd()
	cmd.PersistentFlags().String("config", "nonexistent.yaml", "")
	cmd.PersistentFlags().String("project", "test", "")

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when --web not set")
	}
	if !strings.Contains(err.Error(), "--web") {
		t.Errorf("error should mention --web: %v", err)
	}
}

func TestRunMemory_WebFlag_NoAuditDir(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })

	cmd := newMemoryCmd()
	cmd.PersistentFlags().String("config", "nonexistent.yaml", "")
	cmd.PersistentFlags().String("project", "test", "")
	cmd.Flags().Set("web", "true")

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when audit directory doesn't exist")
	}
	if !strings.Contains(err.Error(), "audit directory not found") {
		t.Errorf("error should mention audit directory: %v", err)
	}
}

// TestRunMemory_WebFlag_WithAuditDir is skipped because it starts a blocking
// HTTP server. The non-web and missing-audit-dir paths are tested above, which
// exercises the key validation logic in runMemory.

func TestRunMemory_CustomPort(t *testing.T) {
	cmd := newMemoryCmd()
	if cmd.Flags().Lookup("port") == nil {
		t.Error("port flag not registered")
	}
	pf := cmd.Flags().Lookup("port")
	if pf.DefValue != "8078" {
		t.Errorf("port default = %q, want 8078", pf.DefValue)
	}
}
