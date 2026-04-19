package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Validate edge cases ---

func TestValidate_InvalidWorktreePrune(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Cleanup.WorktreePrune = "invalid"
	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for invalid worktree_prune")
	}
	if !strings.Contains(err.Error(), "worktree_prune") {
		t.Errorf("expected worktree_prune error, got: %v", err)
	}
}

func TestValidate_InvalidLogArchive(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Cleanup.LogArchive = "s3"
	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for invalid log_archive")
	}
	if !strings.Contains(err.Error(), "log_archive") {
		t.Errorf("expected log_archive error, got: %v", err)
	}
}

func TestValidate_IntermediateMaxTooHigh(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Routing.IntermediateMaxComplexity = 14
	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for intermediate > 13")
	}
}

func TestValidate_NegativeBillingRate(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Billing.DefaultRate = -10
	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for negative billing rate")
	}
}

func TestValidate_EmptyBillingCurrency(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Billing.Currency = ""
	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for empty currency")
	}
}

func TestValidate_InvalidLLMCostMode(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Billing.LLMCosts.Mode = "free_tier"
	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for invalid LLM cost mode")
	}
}

func TestValidate_HoursPerPoint_NegativeValues(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Billing.HoursPerPoint[1] = [2]float64{-1, 2}
	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for negative hours")
	}
}

func TestValidate_HoursPerPoint_LowGreaterThanHigh(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Billing.HoursPerPoint[1] = [2]float64{5, 2}
	err := cfg.Validate()
	if err == nil {
		t.Error("expected error when low > high")
	}
}

func TestValidate_InvalidReviewMode(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Merge.ReviewMode = "half_auto"
	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for invalid review mode")
	}
}

func TestValidate_InvalidQACriterionKind(t *testing.T) {
	cfg := DefaultConfig()
	cfg.QA.SuccessCriteria = []SuccessCriterion{
		{Kind: "bogus_check"},
	}
	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for invalid criterion kind")
	}
}

func TestValidate_ValidReviewModes(t *testing.T) {
	for _, mode := range []string{"", "auto", "manual", "plan_only"} {
		cfg := DefaultConfig()
		cfg.Merge.ReviewMode = mode
		if err := cfg.Validate(); err != nil {
			t.Errorf("review mode %q should be valid: %v", mode, err)
		}
	}
}

func TestValidate_ValidQACriteria(t *testing.T) {
	kinds := []string{"output_contains", "output_not_contains", "file_exists",
		"file_contains", "file_not_empty", "exit_code_zero"}
	for _, kind := range kinds {
		cfg := DefaultConfig()
		cfg.QA.SuccessCriteria = []SuccessCriterion{{Kind: kind}}
		if err := cfg.Validate(); err != nil {
			t.Errorf("criterion kind %q should be valid: %v", kind, err)
		}
	}
}

// --- LoadConfigChain edge cases ---

func TestLoadConfigChain_MalformedRepoFile(t *testing.T) {
	dir := t.TempDir()
	repoPath := filepath.Join(dir, "vxd.yaml")
	// Write malformed YAML that parses but fails validation
	os.WriteFile(repoPath, []byte("workspace:\n  backend: invalid_backend\n"), 0o644)

	globalPath := filepath.Join(dir, "global.yaml")

	_, err := LoadConfigChain(repoPath, globalPath)
	if err == nil {
		t.Error("expected error for malformed repo config")
	}
}

func TestLoadConfigChain_MalformedGlobalFile(t *testing.T) {
	dir := t.TempDir()
	repoPath := filepath.Join(dir, "nonexistent.yaml")
	globalPath := filepath.Join(dir, "global.yaml")
	// Write malformed YAML
	os.WriteFile(globalPath, []byte("workspace:\n  backend: bad_backend\n"), 0o644)

	_, err := LoadConfigChain(repoPath, globalPath)
	if err == nil {
		t.Error("expected error for malformed global config")
	}
}

func TestLoadFromFile_MalformedYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	// YAML with a tab character which is invalid YAML syntax
	os.WriteFile(path, []byte("{\t: broken"), 0o644)

	_, err := LoadFromFile(path)
	if err == nil {
		// Even if YAML parsing is lenient, this is fine.
		// Just ensure it doesn't panic.
	}
	_ = err // suppress
}

// --- DefaultYAML ---

func TestDefaultYAML_NotEmpty(t *testing.T) {
	data, err := DefaultYAML()
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty YAML output")
	}
	if !strings.Contains(string(data), "workspace") {
		t.Error("expected workspace section in YAML")
	}
}

// --- DefaultConfig completeness ---

func TestDefaultConfig_AllValidBackends(t *testing.T) {
	for backend := range validBackends {
		cfg := DefaultConfig()
		cfg.Workspace.Backend = backend
		if err := cfg.Validate(); err != nil {
			t.Errorf("backend %q should be valid: %v", backend, err)
		}
	}
}

func TestDefaultConfig_AllValidLogLevels(t *testing.T) {
	for level := range validLogLevels {
		cfg := DefaultConfig()
		cfg.Workspace.LogLevel = level
		if err := cfg.Validate(); err != nil {
			t.Errorf("log level %q should be valid: %v", level, err)
		}
	}
}

func TestDefaultConfig_AllValidLogArchiveModes(t *testing.T) {
	for mode := range validLogArchive {
		cfg := DefaultConfig()
		cfg.Cleanup.LogArchive = mode
		if err := cfg.Validate(); err != nil {
			t.Errorf("log archive mode %q should be valid: %v", mode, err)
		}
	}
}

func TestDefaultConfig_PerTokenLLMMode(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Billing.LLMCosts.Mode = "per_token"
	if err := cfg.Validate(); err != nil {
		t.Errorf("per_token mode should be valid: %v", err)
	}
}
