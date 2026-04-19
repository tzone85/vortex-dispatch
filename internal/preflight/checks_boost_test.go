package preflight

import (
	"bytes"
	"strings"
	"testing"
)

// --- CheckBillingConfig coverage (currently 66.7%) ---

func TestCheckBillingConfig_DefaultRateIsPositive(t *testing.T) {
	r := CheckBillingConfig()
	if r.Severity != SeverityInfo {
		t.Errorf("expected info severity, got %d", r.Severity)
	}
	// Default config has $150/hr rate, so should pass
	if !r.Passed {
		t.Error("expected billing check to pass with default rate")
	}
	if !strings.Contains(r.Message, "150") {
		t.Errorf("expected rate in message, got: %s", r.Message)
	}
}

// --- CheckStateDir coverage (currently 77.8%) ---

func TestCheckStateDir_ExistingWritable(t *testing.T) {
	r := CheckStateDir()
	// Just verify the check runs without panic and returns valid structure
	if r.Name != "state_dir" {
		t.Errorf("expected name state_dir, got %s", r.Name)
	}
	if r.Severity != SeverityInfo {
		t.Errorf("expected info severity, got %d", r.Severity)
	}
}

// --- CheckGHCLI coverage (currently 71.4%) ---

func TestCheckGHCLI_ReturnsCorrectName(t *testing.T) {
	r := CheckGHCLI()
	if r.Name != "gh" {
		t.Errorf("expected name gh, got %s", r.Name)
	}
	if r.Severity != SeverityWarning {
		t.Errorf("expected warning severity, got %d", r.Severity)
	}
}

// --- CheckClaudeCLI coverage (currently 75%) ---

func TestCheckClaudeCLI_NameAndSeverity(t *testing.T) {
	r := CheckClaudeCLI()
	if r.Name != "claude" {
		t.Errorf("expected name claude, got %s", r.Name)
	}
	if r.Severity != SeverityCritical {
		t.Errorf("expected critical severity, got %d", r.Severity)
	}
}

// --- CheckGitRepo coverage (currently 75%) ---

func TestCheckGitRepo_NameAndSeverity(t *testing.T) {
	r := CheckGitRepo()
	if r.Name != "git" {
		t.Errorf("expected name git, got %s", r.Name)
	}
	if r.Severity != SeverityCritical {
		t.Errorf("expected critical severity, got %d", r.Severity)
	}
}

// --- FormatCompact coverage ---

func TestFormatCompact_AllPassing(t *testing.T) {
	report := Report{
		Results: []Result{
			{Name: "test1", Severity: SeverityCritical, Passed: true, Message: "ok"},
			{Name: "test2", Severity: SeverityWarning, Passed: true, Message: "ok"},
		},
	}
	var buf bytes.Buffer
	FormatCompact(&buf, report)
	out := buf.String()
	if strings.Contains(out, "Aborting") {
		t.Error("should not abort when all pass")
	}
}

func TestFormatCompact_OnlyInfoFailures(t *testing.T) {
	report := Report{
		Results: []Result{
			{Name: "test1", Severity: SeverityCritical, Passed: true, Message: "ok"},
			{Name: "test2", Severity: SeverityInfo, Passed: false, Message: "info fail"},
		},
	}
	var buf bytes.Buffer
	FormatCompact(&buf, report)
	out := buf.String()
	if strings.Contains(out, "Aborting") {
		t.Error("info failure should not cause abort")
	}
}

// --- FormatVerbose additional coverage ---

func TestFormatVerbose_InfoOnlyResults(t *testing.T) {
	report := Report{
		Results: []Result{
			{Name: "config", Severity: SeverityInfo, Passed: true, Message: "config ok"},
			{Name: "billing", Severity: SeverityInfo, Passed: false, Message: "no rate"},
		},
	}
	var buf bytes.Buffer
	FormatVerbose(&buf, report)
	out := buf.String()
	if !strings.Contains(out, "Pre-Flight") {
		t.Error("expected pre-flight header")
	}
}

// --- parseGHUsername edge cases ---

func TestParseGHUsername_MultipleAccountMentions(t *testing.T) {
	output := "Logged in to github.com as account myuser (oauth2)\naccount something"
	got := parseGHUsername(output)
	if got != "myuser" {
		t.Errorf("expected myuser, got %q", got)
	}
}

func TestParseGHUsername_AccountAtLineEnd(t *testing.T) {
	// When "account" is the last word on the line, next word is on next line
	output := "account\notherline"
	got := parseGHUsername(output)
	// Should return "unknown" since there's no word after "account" on same line
	if got != "unknown" {
		t.Errorf("expected unknown, got %q", got)
	}
}
