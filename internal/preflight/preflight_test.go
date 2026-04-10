package preflight_test

import (
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/preflight"
)

func passingCheck(name string, sev preflight.Severity) preflight.Check {
	return func() preflight.Result {
		return preflight.Result{Name: name, Severity: sev, Passed: true, Message: name + " OK"}
	}
}

func failingCheck(name string, sev preflight.Severity) preflight.Check {
	return func() preflight.Result {
		return preflight.Result{Name: name, Severity: sev, Passed: false, Message: name + " failed"}
	}
}

func TestRunAll_AllPass(t *testing.T) {
	report := preflight.RunAll([]preflight.Check{
		passingCheck("a", preflight.SeverityCritical),
		passingCheck("b", preflight.SeverityWarning),
	})
	if len(report.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(report.Results))
	}
	if report.HasCritical {
		t.Fatal("expected no critical failures")
	}
	if report.HasWarning {
		t.Fatal("expected no warnings")
	}
}

func TestRunAll_CriticalFailure(t *testing.T) {
	report := preflight.RunAll([]preflight.Check{
		failingCheck("tmux", preflight.SeverityCritical),
		passingCheck("network", preflight.SeverityWarning),
	})
	if !report.HasCritical {
		t.Fatal("expected critical failure")
	}
	if report.HasWarning {
		t.Fatal("expected no warnings (only critical)")
	}
}

func TestRunAll_WarningOnly(t *testing.T) {
	report := preflight.RunAll([]preflight.Check{
		passingCheck("tmux", preflight.SeverityCritical),
		failingCheck("gh", preflight.SeverityWarning),
	})
	if report.HasCritical {
		t.Fatal("expected no critical failures")
	}
	if !report.HasWarning {
		t.Fatal("expected warning")
	}
}

func TestRunAll_Empty(t *testing.T) {
	report := preflight.RunAll(nil)
	if len(report.Results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(report.Results))
	}
	if report.HasCritical || report.HasWarning {
		t.Fatal("empty run should have no failures")
	}
}

func TestRunAll_InfoFailureDoesNotSetFlags(t *testing.T) {
	report := preflight.RunAll([]preflight.Check{
		failingCheck("config", preflight.SeverityInfo),
	})
	if report.HasCritical {
		t.Fatal("info failure should not set HasCritical")
	}
	if report.HasWarning {
		t.Fatal("info failure should not set HasWarning")
	}
}

func TestRunAll_MixedSeverities(t *testing.T) {
	report := preflight.RunAll([]preflight.Check{
		failingCheck("tmux", preflight.SeverityCritical),
		failingCheck("gh", preflight.SeverityWarning),
		failingCheck("config", preflight.SeverityInfo),
		passingCheck("claude", preflight.SeverityCritical),
	})
	if !report.HasCritical {
		t.Fatal("expected critical")
	}
	if !report.HasWarning {
		t.Fatal("expected warning")
	}
	if len(report.Results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(report.Results))
	}
}
