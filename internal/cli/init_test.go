package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRunInit_CreatesConfigWithoutExampleFile(t *testing.T) {
	// Run init from a temp directory where no vxd.config.example.yaml exists.
	// This is the exact scenario described in issue #6.
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir(orig) })

	// Override HOME so init creates ~/.vxd inside the temp dir
	t.Setenv("HOME", dir)

	cmd := newInitCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Verify vxd.yaml was created
	cfgPath := filepath.Join(dir, "vxd.yaml")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("vxd.yaml not created: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("vxd.yaml is empty")
	}

	// Verify output message
	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("Created vxd.yaml")) {
		t.Fatalf("expected 'Created vxd.yaml' in output, got: %s", output)
	}
	// Should NOT contain the old warning about example config
	if bytes.Contains([]byte(output), []byte("Warning")) {
		t.Fatalf("unexpected warning in output: %s", output)
	}
}

func TestRunInit_SkipsExistingConfig(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir(orig) })

	t.Setenv("HOME", dir)

	// Pre-create a vxd.yaml with custom content
	customCfg := []byte("version: \"2.0\"\n")
	os.WriteFile("vxd.yaml", customCfg, 0644)

	cmd := newInitCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Verify existing config was NOT overwritten
	data, _ := os.ReadFile("vxd.yaml")
	if string(data) != string(customCfg) {
		t.Fatalf("existing vxd.yaml was overwritten: got %s", string(data))
	}

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("already exists")) {
		t.Fatalf("expected 'already exists' in output, got: %s", output)
	}
}
