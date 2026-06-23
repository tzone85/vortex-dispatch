package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/tzone85/vortex-dispatch/internal/autoresearch"
)

// withStateDir installs a fresh override for defaultStateDir for the
// duration of the test. Avoids touching $HOME or VXD_STATE_DIR.
func withStateDir(t *testing.T, dir string) {
	t.Helper()
	prev := stateDirOverride
	stateDirOverride = dir
	t.Cleanup(func() { stateDirOverride = prev })
}

// newCmdWithConfigFlag returns a bare cobra.Command with the
// --config flag wired so loadConfigForAutoresearch can read it.
func newCmdWithConfigFlag() *cobra.Command {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("config", "", "")
	return cmd
}

func TestLoadConfigForAutoresearch_MissingFile(t *testing.T) {
	cmd := newCmdWithConfigFlag()
	if err := cmd.Flags().Set("config", "/no/such/vxd.yaml"); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	if _, err := loadConfigForAutoresearch(cmd); err == nil {
		t.Error("expected error for missing config file")
	}
}

func TestLoadConfigForAutoresearch_ParsesValidFile(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "vxd.yaml")
	yaml := `workspace:
  state_dir: /tmp/state
  backend: sqlite
models:
  tech_lead:
    provider: anthropic
    model: claude-opus-4-8
  senior:
    provider: anthropic
    model: claude-sonnet-4-6
  junior:
    provider: anthropic
    model: claude-haiku-4-5-20251001
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write cfg: %v", err)
	}
	cmd := newCmdWithConfigFlag()
	if err := cmd.Flags().Set("config", cfgPath); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	cfg, err := loadConfigForAutoresearch(cmd)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Workspace.StateDir != "/tmp/state" {
		t.Errorf("config not parsed; got StateDir=%q", cfg.Workspace.StateDir)
	}
}

func TestLoadConfigForAutoresearch_InvalidYAML(t *testing.T) {
	// Force a parse error — YAML that has a leading tab triggers the
	// "found character that cannot start any token" diagnostic.
	cfgPath := filepath.Join(t.TempDir(), "vxd.yaml")
	if err := os.WriteFile(cfgPath, []byte("workspace:\n\t\tstate_dir: bad"), 0o600); err != nil {
		t.Fatalf("write cfg: %v", err)
	}
	cmd := newCmdWithConfigFlag()
	if err := cmd.Flags().Set("config", cfgPath); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	if _, err := loadConfigForAutoresearch(cmd); err == nil {
		t.Error("expected parse error for invalid YAML")
	}
}

func TestOpenEventStore_CreatesFileStore(t *testing.T) {
	dir := t.TempDir()
	withStateDir(t, dir)

	store, closer, err := openEventStore(nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer closer()

	if store == nil {
		t.Fatal("expected non-nil event store")
	}
	// File should have been created.
	if _, err := os.Stat(filepath.Join(dir, "events.jsonl")); err != nil {
		t.Errorf("events.jsonl not created: %v", err)
	}
}

func TestOpenEventStore_FailsForUnwritableParent(t *testing.T) {
	// Point at a path whose parent is a regular file — FileStore.NewFileStore
	// will fail trying to create the events.jsonl beneath it.
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	withStateDir(t, filepath.Join(blocker, "sub"))

	_, _, err := openEventStore(nil)
	if err == nil {
		t.Error("expected error opening event store under regular file")
	}
}

func TestCountWinsLosses_EmptyBank(t *testing.T) {
	// HypothesisBank with an empty event store returns zero/zero.
	dir := t.TempDir()
	withStateDir(t, dir)

	store, closer, err := openEventStore(nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer closer()

	bank := autoresearch.NewHypothesisBank(store)
	wins, losses := countWinsLosses(bank, "any-repo")
	if wins != 0 || losses != 0 {
		t.Errorf("got wins=%d losses=%d, want 0/0 on empty bank", wins, losses)
	}
}
