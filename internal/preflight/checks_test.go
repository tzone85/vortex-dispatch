package preflight_test

import (
	"testing"

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

func TestDispatchChecks_Returns8(t *testing.T) {
	checks := preflight.DispatchChecks()
	if len(checks) != 8 {
		t.Fatalf("expected 8 dispatch checks, got %d", len(checks))
	}
}

func TestAllChecks_Returns13(t *testing.T) {
	checks := preflight.AllChecks()
	if len(checks) != 13 {
		t.Fatalf("expected 13 total checks, got %d", len(checks))
	}
}
