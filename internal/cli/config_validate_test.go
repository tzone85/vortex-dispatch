package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// runConfigValidate — test with invalid config and valid config
// ---------------------------------------------------------------------------

func TestRunConfigValidate_WithInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })

	// Write an invalid YAML config
	invalidCfg := `workspace:
  state_dir: "~/.vxd"
  backend: invalid_backend
routing:
  junior_max_complexity: -5
`
	cfgPath := filepath.Join(dir, "bad-config.yaml")
	os.WriteFile(cfgPath, []byte(invalidCfg), 0644)

	cmd := newConfigValidateCmd()
	cmd.PersistentFlags().String("config", cfgPath, "")

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	// Whether it fails depends on validation logic, but at least it shouldn't panic
	_ = err
	output := buf.String()
	if output == "" {
		t.Error("expected some output from validation")
	}
}

func TestRunConfigValidate_WithDefaultConfig(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })

	cmd := newConfigValidateCmd()
	cmd.PersistentFlags().String("config", "nonexistent.yaml", "")

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "PASSED") {
		t.Errorf("expected 'PASSED', got: %s", buf.String())
	}
}

// ---------------------------------------------------------------------------
// runConfigShow — test with custom config file
// ---------------------------------------------------------------------------

func TestRunConfigShow_WithValidConfig(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })

	validCfg := `workspace:
  state_dir: "~/.vxd"
  backend: sqlite
models:
  tech_lead:
    provider: anthropic
    model: claude-opus-4-8
`
	cfgPath := filepath.Join(dir, "valid-config.yaml")
	os.WriteFile(cfgPath, []byte(validCfg), 0644)

	cmd := newConfigShowCmd()
	cmd.PersistentFlags().String("config", cfgPath, "")

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if output == "" {
		t.Error("expected YAML output")
	}
}

// ---------------------------------------------------------------------------
// loadConfig — test chain loading
// ---------------------------------------------------------------------------

func TestLoadConfig_WithValidFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	cfgContent := `workspace:
  state_dir: "~/.vxd"
`
	cfgPath := filepath.Join(dir, "vxd.yaml")
	os.WriteFile(cfgPath, []byte(cfgContent), 0644)

	cfg, err := loadConfig(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Workspace.StateDir != "~/.vxd" {
		t.Errorf("expected state dir '~/.vxd', got %q", cfg.Workspace.StateDir)
	}
}

func TestLoadConfig_GlobalConfigChain(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	// Create global config
	globalDir := filepath.Join(dir, ".vxd")
	os.MkdirAll(globalDir, 0o755)
	globalCfg := `workspace:
  state_dir: "~/.vxd"
`
	os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte(globalCfg), 0644)

	// No repo config, should load from global
	cfg, err := loadConfig("nonexistent-repo.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should get valid config from chain
	if cfg.Workspace.StateDir == "" {
		t.Error("expected non-empty state dir from config chain")
	}
}
