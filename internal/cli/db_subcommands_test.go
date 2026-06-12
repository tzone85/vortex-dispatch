package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// driveWithVxdYaml wires --config and --project flags onto cmd, points
// --config at a temp vxd.yaml with state_dir=t.TempDir(), and silences
// stdout. Returns the buffer in case the test wants to inspect output.
func driveWithVxdYaml(t *testing.T, cmd *cobra.Command, extraArgs ...string) *bytes.Buffer {
	t.Helper()
	stateDir := t.TempDir()
	cfgPath := seedVxdYaml(t, stateDir)
	cmd.Flags().String("config", "", "")
	cmd.Flags().String("project", "default", "")
	if err := cmd.Flags().Set("config", cfgPath); err != nil {
		t.Fatalf("set config: %v", err)
	}
	cmd.SetArgs(extraArgs)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	return &out
}

// Every db subcommand calls dbProviderFor at the top of its RunE.
// With devdb disabled (the default config), each command surfaces a
// "not configured" error. These tests drive the early branch — they
// don't claim to test the full happy path, but they confirm the
// command is wired up and the error message is informative.

func TestDBListCmd_NoDevDB(t *testing.T) {
	cmd := newDBListCmd()
	driveWithVxdYaml(t, cmd)
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Errorf("expected 'not configured' error, got: %v", err)
	}
}

func TestDBConnectCmd_NoDevDB(t *testing.T) {
	cmd := newDBConnectCmd()
	driveWithVxdYaml(t, cmd, "any-db")
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Errorf("expected 'not configured' error, got: %v", err)
	}
}

func TestDBSQLCmd_NoDevDB(t *testing.T) {
	cmd := newDBSQLCmd()
	driveWithVxdYaml(t, cmd, "any-db", "SELECT 1")
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Errorf("expected 'not configured' error, got: %v", err)
	}
}

func TestDBSchemaCmd_NoDevDB(t *testing.T) {
	cmd := newDBSchemaCmd()
	driveWithVxdYaml(t, cmd, "any-db")
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Errorf("expected 'not configured' error, got: %v", err)
	}
}

func TestDBDeleteCmd_NoDevDB(t *testing.T) {
	cmd := newDBDeleteCmd()
	driveWithVxdYaml(t, cmd, "any-db")
	if err := cmd.Flags().Set("confirm", "true"); err != nil {
		t.Fatalf("set confirm: %v", err)
	}
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Errorf("expected 'not configured' error, got: %v", err)
	}
}

func TestDBDeleteCmd_WithoutConfirm(t *testing.T) {
	cmd := newDBDeleteCmd()
	driveWithVxdYaml(t, cmd, "any-db")
	// Don't set --confirm — the early guard should fire before the
	// provider lookup.
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "confirm") {
		t.Errorf("expected --confirm error, got: %v", err)
	}
}

func TestDBGCCmd_NoOpWhenDevDBDisabled(t *testing.T) {
	// gc skips orphan recovery silently when devdb is disabled — the
	// command must NOT error in this case (it's the default state of a
	// fresh project).
	cmd := newDBGCCmd()
	driveWithVxdYaml(t, cmd)
	if err := cmd.Execute(); err != nil {
		t.Errorf("expected no-op success when devdb disabled, got: %v", err)
	}
}

func TestDBPingCmd_NoDevDB(t *testing.T) {
	cmd := newDBPingCmd()
	driveWithVxdYaml(t, cmd)
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Errorf("expected 'not configured' error, got: %v", err)
	}
}

func TestDBTemplateListCmd_NoDocker(t *testing.T) {
	cmd := newDBTemplateListCmd()
	driveWithVxdYaml(t, cmd)
	// dockerProviderFor returns "require devdb.provider == docker" when
	// the project is on a non-docker backend.
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "docker") {
		t.Errorf("expected 'docker' error, got: %v", err)
	}
}

func TestDBTemplateCreateCmd_NoDocker(t *testing.T) {
	cmd := newDBTemplateCreateCmd()
	// Need --from for cobra's required-flag check to pass.
	driveWithVxdYaml(t, cmd, "template-name")
	if err := cmd.Flags().Set("from", "/no/such/dump.sql"); err != nil {
		t.Fatalf("set from: %v", err)
	}
	err := cmd.Execute()
	// Either "docker" (provider mismatch) or "open dump" (file missing)
	// is acceptable; what we need is that the command wired up and
	// returned an actionable error rather than panicking.
	if err == nil {
		t.Fatal("expected error from template create")
	}
	if !strings.Contains(err.Error(), "docker") && !strings.Contains(err.Error(), "dump") {
		t.Errorf("expected actionable error mentioning 'docker' or 'dump', got: %v", err)
	}
}
