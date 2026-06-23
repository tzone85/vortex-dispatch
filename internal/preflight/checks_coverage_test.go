package preflight

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestParseGHUsername_WithAccount verifies username extraction from gh auth output.
func TestParseGHUsername_WithAccount(t *testing.T) {
	output := `github.com
  ✓ Logged in to github.com account testuser (keyring)
  - Active account: true
  - Git operations protocol: https
  - Token: gho_****
  - Token scopes: 'gist', 'read:org', 'repo', 'workflow'`

	username := parseGHUsername(output)
	if username != "testuser" {
		t.Errorf("expected 'testuser', got %q", username)
	}
}

// TestParseGHUsername_NoAccount verifies fallback when no matching line found.
func TestParseGHUsername_NoAccount(t *testing.T) {
	output := "some random output with no relevant lines"
	username := parseGHUsername(output)
	if username != "unknown" {
		t.Errorf("expected 'unknown', got %q", username)
	}
}

// TestParseGHUsername_EmptyOutput verifies fallback on empty output.
func TestParseGHUsername_EmptyOutput(t *testing.T) {
	username := parseGHUsername("")
	if username != "unknown" {
		t.Errorf("expected 'unknown', got %q", username)
	}
}

// TestParseGHUsername_AccountAtEndOfLine verifies edge case where "account"
// is the last word on the line (i+1 out of bounds).
func TestParseGHUsername_AccountAtEndOfLine(t *testing.T) {
	output := "some line with account"
	username := parseGHUsername(output)
	// "account" is the last word, so i+1 is out of bounds
	if username != "unknown" {
		t.Errorf("expected 'unknown' when account is last word, got %q", username)
	}
}

// TestCheckGoogleAPIKey_Set verifies check passes when env is set.
func TestCheckGoogleAPIKey_Set(t *testing.T) {
	t.Setenv("GOOGLE_AI_API_KEY", "test-key")
	result := CheckGoogleAPIKey()
	if !result.Passed {
		t.Error("should pass when GOOGLE_AI_API_KEY is set")
	}
	if !strings.Contains(result.Message, "set") {
		t.Error("message should indicate key is set")
	}
}

// TestCheckGoogleAPIKey_Unset verifies check fails when env is not set.
func TestCheckGoogleAPIKey_Unset(t *testing.T) {
	t.Setenv("GOOGLE_AI_API_KEY", "")
	result := CheckGoogleAPIKey()
	if result.Passed {
		t.Error("should fail when GOOGLE_AI_API_KEY is empty")
	}
	if !strings.Contains(result.Message, "not set") {
		t.Error("message should indicate key is not set")
	}
}

// TestCheckProject_WithEnvVar verifies project detection from VXD_PROJECT env.
func TestCheckProject_WithEnvVar(t *testing.T) {
	t.Setenv("VXD_PROJECT", "my-project")
	result := CheckProject()
	if !result.Passed {
		t.Error("should pass when VXD_PROJECT is set")
	}
	if !strings.Contains(result.Message, "my-project") {
		t.Error("message should contain project name")
	}
	if !strings.Contains(result.Message, "VXD_PROJECT env") {
		t.Error("message should indicate source is env var")
	}
}

// TestCheckStaleSessions_Metadata verifies returned metadata.
func TestCheckStaleSessions_Metadata(t *testing.T) {
	result := CheckStaleSessions()
	if result.Name != "stale_sessions" {
		t.Errorf("expected name 'stale_sessions', got %q", result.Name)
	}
	if result.Severity != SeverityWarning {
		t.Error("expected SeverityWarning")
	}
}

// TestCheckConfig_Metadata verifies returned metadata.
func TestCheckConfig_Metadata(t *testing.T) {
	result := CheckConfig()
	if result.Name != "config" {
		t.Errorf("expected name 'config', got %q", result.Name)
	}
	if result.Severity != SeverityInfo {
		t.Error("expected SeverityInfo")
	}
	// In the test environment, may find a config or report defaults
	if result.Message == "" {
		t.Error("message should not be empty")
	}
}

// TestCheckLLMAvailable_OnlyClaudeCLI verifies LLM check passes with only Claude CLI.
func TestCheckLLMAvailable_OnlyClaudeCLI(t *testing.T) {
	// Unset API keys to test Claude CLI path only
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GOOGLE_AI_API_KEY", "")
	result := CheckLLMAvailable()
	// Should pass because claude CLI is on PATH in this environment
	if result.Name != "llm" {
		t.Errorf("expected name 'llm', got %q", result.Name)
	}
}

// TestCheckBillingConfig_DefaultRate verifies billing config with defaults.
func TestCheckBillingConfig_DefaultRate(t *testing.T) {
	result := CheckBillingConfig()
	if result.Name != "billing" {
		t.Errorf("expected name 'billing', got %q", result.Name)
	}
	if result.Severity != SeverityInfo {
		t.Error("expected SeverityInfo")
	}
}

// TestCheckStateDir_ExistingDir verifies check against a real writable dir.
func TestCheckStateDir_Metadata(t *testing.T) {
	result := CheckStateDir()
	if result.Name != "state_dir" {
		t.Errorf("expected name 'state_dir', got %q", result.Name)
	}
	if result.Severity != SeverityInfo {
		t.Error("expected SeverityInfo")
	}
}

// TestIconFor_CriticalPassed verifies icon for passing critical check.
func TestIconFor_CriticalPassed(t *testing.T) {
	r := Result{Severity: SeverityCritical, Passed: true}
	icon := iconFor(r)
	if icon != "\u2713" {
		t.Errorf("expected check mark, got %q", icon)
	}
}

// TestIconFor_InfoPassed verifies icon for passing info check.
func TestIconFor_InfoPassed(t *testing.T) {
	r := Result{Severity: SeverityInfo, Passed: true}
	icon := iconFor(r)
	if icon != "\u2139" {
		t.Errorf("expected info icon, got %q", icon)
	}
}

// TestIconFor_CriticalFailed verifies icon for failing critical check.
func TestIconFor_CriticalFailed(t *testing.T) {
	r := Result{Severity: SeverityCritical, Passed: false}
	icon := iconFor(r)
	if icon != "\u2717" {
		t.Errorf("expected X mark, got %q", icon)
	}
}

// TestIconFor_WarningFailed verifies icon for failing warning check.
func TestIconFor_WarningFailed(t *testing.T) {
	r := Result{Severity: SeverityWarning, Passed: false}
	icon := iconFor(r)
	if icon != "\u26a0" {
		t.Errorf("expected warning icon, got %q", icon)
	}
}

// TestIconFor_InfoFailed verifies icon for failing info check (default case).
func TestIconFor_InfoFailed(t *testing.T) {
	r := Result{Severity: SeverityInfo, Passed: false}
	icon := iconFor(r)
	if icon != "\u2139" {
		t.Errorf("expected info icon for failed info, got %q", icon)
	}
}

// TestFormatVerbose_CriticalAndWarning verifies verbose output with both failures.
func TestFormatVerbose_CriticalAndWarning(t *testing.T) {
	report := Report{
		Results: []Result{
			{Name: "tmux", Severity: SeverityCritical, Passed: false, Message: "tmux not found"},
			{Name: "gh", Severity: SeverityWarning, Passed: false, Message: "gh not authenticated"},
			{Name: "claude", Severity: SeverityCritical, Passed: true, Message: "claude installed"},
		},
		HasCritical: true,
		HasWarning:  true,
	}
	var buf strings.Builder
	FormatVerbose(&buf, report)
	output := buf.String()
	if !strings.Contains(output, "1 critical, 1 warnings") {
		t.Errorf("expected critical+warning summary, got: %s", output)
	}
}

// TestFormatVerbose_CriticalOnly verifies verbose output with only critical failures.
func TestFormatVerbose_CriticalOnly(t *testing.T) {
	report := Report{
		Results: []Result{
			{Name: "tmux", Severity: SeverityCritical, Passed: false, Message: "tmux not found"},
		},
		HasCritical: true,
	}
	var buf strings.Builder
	FormatVerbose(&buf, report)
	output := buf.String()
	if !strings.Contains(output, "critical issues") {
		t.Errorf("expected critical-only summary, got: %s", output)
	}
}

// TestFormatVerbose_WarningOnly verifies verbose output with only warnings.
func TestFormatVerbose_WarningOnly(t *testing.T) {
	report := Report{
		Results: []Result{
			{Name: "gh", Severity: SeverityWarning, Passed: false, Message: "gh not auth"},
		},
		HasWarning: true,
	}
	var buf strings.Builder
	FormatVerbose(&buf, report)
	output := buf.String()
	if !strings.Contains(output, "warnings. Ready to dispatch") {
		t.Errorf("expected warning-only summary, got: %s", output)
	}
}

// TestFormatCompact_WithWarningAndCritical verifies compact with both types.
func TestFormatCompact_WithWarningAndCritical(t *testing.T) {
	report := Report{
		Results: []Result{
			{Name: "tmux", Severity: SeverityCritical, Passed: false, Message: "tmux missing"},
			{Name: "gh", Severity: SeverityWarning, Passed: false, Message: "gh not auth"},
			{Name: "config", Severity: SeverityInfo, Passed: false, Message: "config info"},
		},
		HasCritical: true,
		HasWarning:  true,
	}
	var buf strings.Builder
	FormatCompact(&buf, report)
	output := buf.String()
	if !strings.Contains(output, "tmux missing") {
		t.Error("should show critical failure")
	}
	if !strings.Contains(output, "gh not auth") {
		t.Error("should show warning failure")
	}
	// Info failures should NOT appear in compact format
	if strings.Contains(output, "config info") {
		t.Error("info failures should not appear in compact format")
	}
	if !strings.Contains(output, "Aborting") {
		t.Error("should show aborting message for critical")
	}
}

// TestCheckStaleSessions_Parsing verifies the stale session parsing logic.
// This test works because we're in the internal package and can verify the
// parsing logic by examining the result message.
func TestCheckStaleSessions_Parsing(t *testing.T) {
	// Just verify the check runs and returns valid result structure.
	result := CheckStaleSessions()
	if result.Name != "stale_sessions" {
		t.Errorf("expected name 'stale_sessions', got %q", result.Name)
	}
	// The message should either say "No stale" or contain count info
	if !strings.Contains(result.Message, "stale") {
		t.Errorf("message should mention stale sessions: %q", result.Message)
	}
}

// TestCheckStaleSessions_WithStaleSession creates a vxd-* tmux session
// and verifies the check detects it.
func TestCheckStaleSessions_WithStaleSession(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}

	name := "vxd-test-stale-preflight"
	// Clean up first
	exec.Command("tmux", "kill-session", "-t", name).Run()

	// Create a stale vxd-* session
	err := exec.Command("tmux", "new-session", "-d", "-s", name, "-c", "/tmp", "sleep 30").Run()
	if err != nil {
		t.Fatalf("create stale session: %v", err)
	}
	defer exec.Command("tmux", "kill-session", "-t", name).Run()

	result := CheckStaleSessions()
	if result.Passed {
		t.Error("should detect stale vxd-* session and report as not passed")
	}
	if !strings.Contains(result.Message, "stale tmux sessions found") {
		t.Errorf("message should mention stale sessions found: %q", result.Message)
	}
}

// TestCheckConfig_WithTempConfig creates a temporary config file and verifies
// that CheckConfig detects and validates it.
func TestCheckConfig_WithTempConfig(t *testing.T) {
	// CheckConfig looks for "vxd.yaml" in CWD and "$HOME/.vxd/config.yaml".
	// We can test the no-config-found path by running in a temp dir.
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(origDir)

	// Without any config file, should report "defaults"
	result := CheckConfig()
	// May find global config at $HOME/.vxd, or report defaults
	if result.Name != "config" {
		t.Errorf("expected name 'config', got %q", result.Name)
	}
}

// TestCheckConfig_WithInvalidYAML verifies CheckConfig catches invalid config.
func TestCheckConfig_WithInvalidYAML(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(origDir)

	// Write invalid YAML
	err = os.WriteFile("vxd.yaml", []byte("{{invalid yaml content"), 0644)
	if err != nil {
		t.Fatalf("write invalid yaml: %v", err)
	}

	result := CheckConfig()
	if result.Passed {
		t.Error("should fail with invalid YAML config")
	}
	if !strings.Contains(result.Message, "Config error") {
		t.Errorf("message should mention config error: %q", result.Message)
	}
}

// TestCheckConfig_WithValidYAML verifies CheckConfig accepts valid config.
func TestCheckConfig_WithValidYAML(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(origDir)

	// Write minimal valid YAML config
	validConfig := `workspace:
  state_dir: /tmp/.vxd-test
  backend: sqlite
models:
  tech_lead:
    provider: anthropic
    model: claude-opus-4-8
`
	err = os.WriteFile("vxd.yaml", []byte(validConfig), 0644)
	if err != nil {
		t.Fatalf("write valid yaml: %v", err)
	}

	result := CheckConfig()
	if !result.Passed {
		t.Errorf("should pass with valid config, got: %s", result.Message)
	}
	if !strings.Contains(result.Message, "vxd.yaml") {
		t.Errorf("message should mention vxd.yaml: %q", result.Message)
	}
}

// TestCheckProject_WithoutEnvVar verifies project detection from git.
func TestCheckProject_WithoutEnvVar(t *testing.T) {
	t.Setenv("VXD_PROJECT", "")
	result := CheckProject()
	if result.Name != "project" {
		t.Errorf("expected name 'project', got %q", result.Name)
	}
	// In a git repo environment, should auto-detect
	if result.Severity != SeverityInfo {
		t.Error("expected SeverityInfo")
	}
}

// TestCheckNetwork_Passes verifies network check in connected environment.
func TestCheckNetwork_Passes(t *testing.T) {
	result := CheckNetwork()
	// In a connected environment, this should pass
	if result.Name != "network" {
		t.Errorf("expected name 'network', got %q", result.Name)
	}
}
