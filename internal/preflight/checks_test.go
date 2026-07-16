package preflight_test

import (
	"os"
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/preflight"
)

func TestCheckTmux_ReturnsResult(t *testing.T) {
	result := preflight.CheckTmux()
	if result.Name != "tmux" {
		t.Fatalf("expected name 'tmux', got %s", result.Name)
	}
	if result.Severity != preflight.SeverityCritical {
		t.Fatal("expected SeverityCritical")
	}
	if result.Message == "" {
		t.Fatal("expected non-empty message")
	}
}

func TestCheckClaudeCLI_ReturnsResult(t *testing.T) {
	result := preflight.CheckClaudeCLI()
	if result.Name != "claude" {
		t.Fatalf("expected name 'claude', got %s", result.Name)
	}
	if result.Severity != preflight.SeverityCritical {
		t.Fatal("expected SeverityCritical")
	}
	if result.Message == "" {
		t.Fatal("expected non-empty message")
	}
}

func TestCheckGitRepo_ReturnsResult(t *testing.T) {
	result := preflight.CheckGitRepo()
	if result.Name != "git" {
		t.Fatalf("expected name 'git', got %s", result.Name)
	}
	if result.Severity != preflight.SeverityCritical {
		t.Fatal("expected SeverityCritical")
	}
	if result.Message == "" {
		t.Fatal("expected non-empty message")
	}
}

func TestCheckLLMAvailable_ReturnsResult(t *testing.T) {
	result := preflight.CheckLLMAvailable()
	if result.Name != "llm" {
		t.Fatalf("expected name 'llm', got %s", result.Name)
	}
	if result.Severity != preflight.SeverityCritical {
		t.Fatal("expected SeverityCritical")
	}
	if result.Message == "" {
		t.Fatal("expected non-empty message")
	}
}

func TestCheckGHCLI_ReturnsResult(t *testing.T) {
	result := preflight.CheckGHCLI()
	if result.Name != "gh" {
		t.Fatalf("expected name 'gh', got %s", result.Name)
	}
	if result.Severity != preflight.SeverityWarning {
		t.Fatal("expected SeverityWarning")
	}
	if result.Message == "" {
		t.Fatal("expected non-empty message")
	}
}

func TestCheckNetwork_ReturnsResult(t *testing.T) {
	result := preflight.CheckNetwork()
	if result.Name != "network" {
		t.Fatalf("expected name 'network', got %s", result.Name)
	}
	if result.Severity != preflight.SeverityWarning {
		t.Fatal("expected SeverityWarning")
	}
	if result.Message == "" {
		t.Fatal("expected non-empty message")
	}
}

func TestCheckStaleSessions_ReturnsResult(t *testing.T) {
	result := preflight.CheckStaleSessions()
	if result.Name != "stale_sessions" {
		t.Fatalf("expected name 'stale_sessions', got %s", result.Name)
	}
	if result.Severity != preflight.SeverityWarning {
		t.Fatal("expected SeverityWarning")
	}
	if result.Message == "" {
		t.Fatal("expected non-empty message")
	}
}

func TestCheckGoogleAPIKey_ReturnsResult(t *testing.T) {
	result := preflight.CheckGoogleAPIKey()
	if result.Name != "google_api_key" {
		t.Fatalf("expected name 'google_api_key', got %s", result.Name)
	}
	if result.Severity != preflight.SeverityWarning {
		t.Fatal("expected SeverityWarning")
	}
	if result.Message == "" {
		t.Fatal("expected non-empty message")
	}
}

func TestCheckConfig_ReturnsResult(t *testing.T) {
	result := preflight.CheckConfig()
	if result.Name != "config" {
		t.Fatalf("expected name 'config', got %s", result.Name)
	}
	if result.Severity != preflight.SeverityInfo {
		t.Fatal("expected SeverityInfo")
	}
	if result.Message == "" {
		t.Fatal("expected non-empty message")
	}
}

func TestCheckProject_ReturnsResult(t *testing.T) {
	result := preflight.CheckProject()
	if result.Name != "project" {
		t.Fatalf("expected name 'project', got %s", result.Name)
	}
	if result.Severity != preflight.SeverityInfo {
		t.Fatal("expected SeverityInfo")
	}
	if result.Message == "" {
		t.Fatal("expected non-empty message")
	}
}

func TestCheckStateDir_ReturnsResult(t *testing.T) {
	result := preflight.CheckStateDir()
	if result.Name != "state_dir" {
		t.Fatalf("expected name 'state_dir', got %s", result.Name)
	}
	if result.Severity != preflight.SeverityInfo {
		t.Fatal("expected SeverityInfo")
	}
	if result.Message == "" {
		t.Fatal("expected non-empty message")
	}
}

func TestCheckBillingConfig_ReturnsResult(t *testing.T) {
	result := preflight.CheckBillingConfig()
	if result.Name != "billing" {
		t.Fatalf("expected name 'billing', got %s", result.Name)
	}
	if result.Severity != preflight.SeverityInfo {
		t.Fatal("expected SeverityInfo")
	}
	if result.Message == "" {
		t.Fatal("expected non-empty message")
	}
}

func TestCheckOllama_ReturnsCorrectNameAndSeverity(t *testing.T) {
	result := preflight.CheckOllama()
	if result.Name != "ollama" {
		t.Fatalf("expected name 'ollama', got %s", result.Name)
	}
	if result.Severity != preflight.SeverityInfo {
		t.Fatal("expected SeverityInfo")
	}
	if result.Message == "" {
		t.Fatal("expected non-empty message")
	}
}

func TestCheckOllama_PassesWhenNotInstalled(t *testing.T) {
	result := preflight.CheckOllama()
	// Ollama is optional for VXD, so the check should always pass
	// regardless of whether Ollama is installed or not.
	if !result.Passed {
		t.Fatalf("expected Passed=true (Ollama is optional), got false: %s", result.Message)
	}
}

// The check-set counts are documented in README.md, CLAUDE.md, and AGENTS.md —
// when either count changes, update all three docs plus
// test/audit_structural_test.go::TestAudit_PreflightCheckCounts.
func TestDispatchChecks_Count(t *testing.T) {
	checks := preflight.DispatchChecks()
	if len(checks) != 11 {
		t.Fatalf("expected 11 dispatch checks, got %d", len(checks))
	}
}

func TestAllChecks_Count(t *testing.T) {
	checks := preflight.AllChecks()
	if len(checks) != 19 {
		t.Fatalf("expected 19 total checks, got %d", len(checks))
	}
}

func TestCheckBinaryPath_WarnWhenOutsideLocalBin(t *testing.T) {
	// Simulate binary being at ~/go/bin/vxd (common shadow location)
	fakePath := "/Users/testuser/go/bin/vxd"
	result := preflight.CheckBinaryPath(fakePath)

	if result.Name != "binary_path" {
		t.Fatalf("expected name 'binary_path', got %q", result.Name)
	}
	if result.Severity != preflight.SeverityWarning {
		t.Fatal("expected SeverityWarning")
	}
	if result.Passed {
		t.Fatal("expected Passed=false when binary is outside ~/.local/bin/")
	}
	if !strings.Contains(result.Message, fakePath) {
		t.Fatalf("expected message to contain fake path %q, got: %s", fakePath, result.Message)
	}
	if !strings.Contains(result.Message, "rm "+fakePath) {
		t.Fatalf("expected message to contain remediation 'rm %s', got: %s", fakePath, result.Message)
	}
}

func TestCheckBinaryPath_PassWhenInLocalBin(t *testing.T) {
	home := os.Getenv("HOME")
	fakePath := home + "/.local/bin/vxd"
	result := preflight.CheckBinaryPath(fakePath)

	if !result.Passed {
		t.Fatalf("expected Passed=true when binary is in ~/.local/bin/, got: %s", result.Message)
	}
}

func TestCheckDevDBProviderReachable_Null(t *testing.T) {
	cfg := config.Config{}
	cfg.DevDB.Provider = "null"
	r := preflight.CheckDevDBProviderReachable(cfg)
	if !r.Passed || r.Severity != preflight.SeverityInfo {
		t.Errorf("null provider should pass with INFO: %+v", r)
	}
}

func TestCheckDevDBProviderReachable_Empty(t *testing.T) {
	cfg := config.Config{}
	// Provider is "" (zero value)
	r := preflight.CheckDevDBProviderReachable(cfg)
	if !r.Passed || r.Severity != preflight.SeverityInfo {
		t.Errorf("empty provider should pass with INFO: %+v", r)
	}
}

func TestCheckDevDBProviderReachable_Unknown(t *testing.T) {
	cfg := config.Config{}
	cfg.DevDB.Provider = "potato"
	r := preflight.CheckDevDBProviderReachable(cfg)
	if r.Passed || r.Severity != preflight.SeverityCritical {
		t.Errorf("unknown provider should be CRITICAL fail: %+v", r)
	}
}

func TestCheckDevDBProviderReachable_GhostNoAPIKey(t *testing.T) {
	// Without an API key in the environment the ghost check should fail
	// CRITICAL (key resolution error), not block as "SP2 pending".
	cfg := config.Config{}
	cfg.DevDB.Provider = "ghost"
	cfg.DevDB.Ghost.APIKeyEnv = "GHOST_API_KEY_PREFLIGHT_TEST_UNSET_XYZ"
	r := preflight.CheckDevDBProviderReachable(cfg)
	if r.Passed || r.Severity != preflight.SeverityCritical {
		t.Errorf("ghost provider with no API key should be CRITICAL+!Passed: %+v", r)
	}
}

func TestCheckDevDBTemplateExists_NullProvider(t *testing.T) {
	cfg := config.Config{}
	cfg.DevDB.Provider = "null"
	cfg.DevDB.Template = "x"
	r := preflight.CheckDevDBTemplateExists(cfg)
	if !r.Passed || r.Severity != preflight.SeverityInfo {
		t.Errorf("null provider template-check should pass with INFO: %+v", r)
	}
}

func TestCheckDevDBTemplateExists_EmptyTemplate(t *testing.T) {
	cfg := config.Config{}
	cfg.DevDB.Provider = "docker"
	// Template empty
	r := preflight.CheckDevDBTemplateExists(cfg)
	if !r.Passed || r.Severity != preflight.SeverityInfo {
		t.Errorf("empty template should pass with INFO: %+v", r)
	}
}

func TestCheckDevDBTemplateExists_GhostProviderSkipped(t *testing.T) {
	cfg := config.Config{}
	cfg.DevDB.Provider = "ghost"
	cfg.DevDB.Template = "mytemplate"
	r := preflight.CheckDevDBTemplateExists(cfg)
	if !r.Passed || r.Severity != preflight.SeverityInfo {
		t.Errorf("ghost provider template-check should be skipped with INFO: %+v", r)
	}
}

func TestDispatchChecksWithConfig_AddsTwoDevDBChecks(t *testing.T) {
	cfg := config.Config{}
	cfg.DevDB.Provider = "null"
	base := len(preflight.DispatchChecks())
	full := len(preflight.DispatchChecksWithConfig(cfg))
	if full != base+2 {
		t.Errorf("DispatchChecksWithConfig should add 2 checks, got %d vs base %d", full, base)
	}
}

func TestAllChecksWithConfig_AddsTwoDevDBChecks(t *testing.T) {
	cfg := config.Config{}
	cfg.DevDB.Provider = "null"
	base := len(preflight.AllChecks())
	full := len(preflight.AllChecksWithConfig(cfg))
	if full != base+2 {
		t.Errorf("AllChecksWithConfig should add 2 checks, got %d vs base %d", full, base)
	}
}

// TestPreflight_QAModelInertCheck pins the qa_model preflight check: a
// non-default models.qa binding fails at WARNING tier with the inert-binding
// message, the default binding passes.
func TestPreflight_QAModelInertCheck(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Models.QA = config.ModelConfig{Provider: "codex", Model: "gpt-5.5"}

	r := preflight.CheckQAModelInertWith(cfg)
	if r.Name != "qa_model" {
		t.Fatalf("name = %q, want qa_model", r.Name)
	}
	if r.Severity != preflight.SeverityWarning {
		t.Errorf("severity = %v, want warning", r.Severity)
	}
	if r.Passed {
		t.Error("non-default models.qa binding must fail the check")
	}
	if !strings.Contains(r.Message, "inert") || !strings.Contains(r.Message, "models.reviewer") {
		t.Errorf("message should explain inertness and point to models.reviewer: %q", r.Message)
	}

	ok := preflight.CheckQAModelInertWith(config.DefaultConfig())
	if !ok.Passed {
		t.Errorf("default config must pass, got: %s", ok.Message)
	}
}
