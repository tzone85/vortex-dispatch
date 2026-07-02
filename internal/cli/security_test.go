package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/config"
)

func TestResolveScanPath_ExplicitArg(t *testing.T) {
	dir := t.TempDir()
	got, err := resolveScanPath([]string{dir})
	if err != nil {
		t.Fatalf("resolveScanPath: %v", err)
	}
	if got != dir {
		t.Errorf("want %q, got %q", dir, got)
	}
}

func TestResolveScanPath_RelativeBecomesAbsolute(t *testing.T) {
	got, err := resolveScanPath([]string{"."})
	if err != nil {
		t.Fatalf("resolveScanPath: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("expected absolute path, got %q", got)
	}
}

func TestResolveScanPath_DefaultsToCwd(t *testing.T) {
	got, err := resolveScanPath(nil)
	if err != nil {
		t.Fatalf("resolveScanPath: %v", err)
	}
	cwd, _ := os.Getwd()
	if got != cwd {
		t.Errorf("want cwd %q, got %q", cwd, got)
	}
}

func TestSecurityKBPath_Configured(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Security.KBPath = "/explicit/kb.json"
	if got := securityKBPath(cfg); got != "/explicit/kb.json" {
		t.Errorf("configured kb_path must win, got %q", got)
	}
}

func TestSecurityKBPath_DefaultUnderStateDir(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Security.KBPath = ""
	cfg.Workspace.StateDir = "/tmp/vxd-state"
	want := filepath.Join("/tmp/vxd-state", "security", "knowledge.json")
	if got := securityKBPath(cfg); got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

func TestSecurityKBCmd_TextOutput(t *testing.T) {
	cmd := newSecurityKBCmd()
	out := driveWithVxdYaml(t, cmd)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("kb command: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "Security knowledge base v") {
		t.Errorf("missing header:\n%s", got)
	}
	// The baseline seeds the OWASP Top 10 — at least one A0x rule must render.
	if !strings.Contains(got, "A01") {
		t.Errorf("baseline OWASP rules missing:\n%s", got)
	}
}

func TestSecurityKBCmd_JSONOutput(t *testing.T) {
	cmd := newSecurityKBCmd()
	out := driveWithVxdYaml(t, cmd, "--json")
	if err := cmd.Execute(); err != nil {
		t.Fatalf("kb --json: %v", err)
	}
	var kb struct {
		Version int `json:"version"`
		Rules   []struct {
			ID string `json:"id"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(out.Bytes(), &kb); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out.String())
	}
	if len(kb.Rules) == 0 {
		t.Error("baseline knowledge base must not be empty")
	}
}

// TestSecurityScanCmd_NoScannersInstalled drives the full scan command with an
// empty PATH so every scanner is skipped (the graceful-degradation path): the
// scan must succeed, report zero findings, and list what it skipped rather
// than pretending it covered anything.
func TestSecurityScanCmd_NoScannersInstalled(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // empty dir: no scanner binaries resolvable

	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := newSecurityScanCmd()
	out := driveWithVxdYaml(t, cmd, "--json", repo)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("scan with no scanners must pass (0 findings < --min high): %v", err)
	}

	var report struct {
		Findings []any    `json:"findings"`
		Skipped  []string `json:"skipped"`
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out.String())
	}
	if len(report.Findings) != 0 {
		t.Errorf("no scanners ran — findings must be empty, got %d", len(report.Findings))
	}
	if len(report.Skipped) == 0 {
		t.Error("skipped scanners must be reported, not silently dropped")
	}
}

func TestSecurityScanCmd_MarkdownOutput(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := newSecurityScanCmd()
	out := driveWithVxdYaml(t, cmd, repo)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if out.Len() == 0 {
		t.Error("markdown report must not be empty")
	}
}
