package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestImproveAuditCmd_Flags(t *testing.T) {
	cmd := newImproveAuditCmd()
	for _, f := range []string{"fix", "skip-tests", "json", "timeout", "repo"} {
		if cmd.Flags().Lookup(f) == nil {
			t.Errorf("missing --%s flag", f)
		}
	}
	var found bool
	for _, c := range newImproveCmd().Commands() {
		if c.Name() == "audit" {
			found = true
		}
	}
	if !found {
		t.Error("audit must be registered under improve")
	}
}

func TestImproveAudit_FixFormatsFile(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns go toolchain")
	}
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/audit\n\ngo 1.26\n")
	write("main.go", "package main\n\nfunc main() {\n\tx   :=   1\n\t_ = x\n}\n")

	cmd := newImproveAuditCmd()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"--repo", dir, "--skip-tests", "--fix"})
	// govulncheck may be absent; that is reported as skipped, not as a failure.
	if err := cmd.Execute(); err != nil {
		t.Fatalf("audit --fix: %v\n%s\n%s", err, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "Self-Audit") {
		t.Errorf("report missing:\n%s", out.String())
	}
	got, err := exec.Command("gofmt", "-l", dir).Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != "" {
		t.Errorf("file still unformatted: %s", got)
	}
}

func TestImproveAudit_RejectsNonDirectoryRepo(t *testing.T) {
	cmd := newImproveAuditCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--repo", filepath.Join(t.TempDir(), "missing"), "--skip-tests"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("want 'not a directory' error, got %v", err)
	}
}

func TestImproveAudit_AllChecksSkippedIsAnError(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns toolchain lookups")
	}
	// An empty PATH means no tool can run: every check is skipped, which must
	// not look like a clean audit.
	t.Setenv("PATH", "")
	cmd := newImproveAuditCmd()
	var errOut bytes.Buffer
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"--repo", t.TempDir(), "--skip-tests"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "no check could run") {
		t.Fatalf("want 'no check could run' error, got %v (stderr %q)", err, errOut.String())
	}
}
