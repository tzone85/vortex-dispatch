package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedVxdYaml writes a minimal config to t.TempDir() that points state at
// stateDir and disables all live integrations. Returns the config file
// path so tests can pass it via --config.
func seedVxdYaml(t *testing.T, stateDir string) string {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "vxd.yaml")
	yaml := "workspace:\n" +
		"  state_dir: " + stateDir + "\n" +
		"  backend: sqlite\n" +
		"models:\n" +
		"  tech_lead:\n" +
		"    provider: anthropic\n" +
		"    model: claude-opus-4-20250514\n" +
		"  senior:\n" +
		"    provider: anthropic\n" +
		"    model: claude-sonnet-4-20250514\n" +
		"  junior:\n" +
		"    provider: anthropic\n" +
		"    model: claude-haiku-4-5-20251001\n"
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write cfg: %v", err)
	}
	return cfgPath
}

func TestRunBackup_CreatesArchive(t *testing.T) {
	stateDir := t.TempDir()
	cfgPath := seedVxdYaml(t, stateDir)
	outDir := t.TempDir()

	cmd := newBackupCmd()
	cmd.Flags().String("config", "", "")
	cmd.Flags().String("project", "default", "")
	if err := cmd.Flags().Set("config", cfgPath); err != nil {
		t.Fatalf("set config: %v", err)
	}
	if err := cmd.Flags().Set("output", outDir); err != nil {
		t.Fatalf("set output: %v", err)
	}
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	entries, _ := os.ReadDir(outDir)
	found := false
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tar.gz") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected .tar.gz archive in %s, got: %+v", outDir, entries)
	}
	if !strings.Contains(out.String(), "Backup created") {
		t.Errorf("expected 'Backup created' in stdout, got: %s", out.String())
	}
}

func TestRunLogs_MissingLogFile(t *testing.T) {
	stateDir := t.TempDir()
	cfgPath := seedVxdYaml(t, stateDir)

	cmd := newLogsCmd()
	cmd.Flags().String("config", "", "")
	cmd.Flags().String("project", "default", "")
	if err := cmd.Flags().Set("config", cfgPath); err != nil {
		t.Fatalf("set config: %v", err)
	}
	cmd.SetArgs([]string{"REQ-DOES-NOT-EXIST"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	if err == nil {
		t.Error("expected error for missing log file")
	}
	if !strings.Contains(err.Error(), "no log file") {
		t.Errorf("expected 'no log file' in error, got: %v", err)
	}
}

func TestRunLogs_StreamsLogContents(t *testing.T) {
	stateDir := t.TempDir()
	cfgPath := seedVxdYaml(t, stateDir)

	// Walk the loadStores flow: project dir is <stateDir>/projects/default.
	projectDir := filepath.Join(stateDir, "projects", "default")
	if err := os.MkdirAll(filepath.Join(projectDir, "logs"), 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	logPath := filepath.Join(projectDir, "logs", "req-REQ-X.log")
	body := "monitor: story 1 dispatched\nmonitor: story 1 merged\n"
	if err := os.WriteFile(logPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write log: %v", err)
	}

	cmd := newLogsCmd()
	cmd.Flags().String("config", "", "")
	cmd.Flags().String("project", "default", "")
	if err := cmd.Flags().Set("config", cfgPath); err != nil {
		t.Fatalf("set config: %v", err)
	}
	cmd.SetArgs([]string{"REQ-X"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out.String(), "story 1 merged") {
		t.Errorf("log not streamed; got: %s", out.String())
	}
}
