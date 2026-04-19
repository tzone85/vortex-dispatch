package preflight

import (
	"bytes"
	"strings"
	"testing"
)

// --- Additional format coverage ---

func TestFormatVerbose_AllPassingPrintsReady(t *testing.T) {
	report := Report{
		Results: []Result{
			{Name: "t1", Severity: SeverityCritical, Passed: true, Message: "ok"},
			{Name: "t2", Severity: SeverityWarning, Passed: true, Message: "ok"},
			{Name: "t3", Severity: SeverityInfo, Passed: true, Message: "ok"},
		},
	}
	var buf bytes.Buffer
	FormatVerbose(&buf, report)
	out := buf.String()
	if !strings.Contains(out, "Ready to dispatch") {
		t.Error("expected ready to dispatch message")
	}
}

func TestFormatVerbose_CriticalFailure(t *testing.T) {
	report := Report{
		Results: []Result{
			{Name: "t1", Severity: SeverityCritical, Passed: false, Message: "tmux missing"},
		},
		HasCritical: true,
	}
	var buf bytes.Buffer
	FormatVerbose(&buf, report)
	out := buf.String()
	if !strings.Contains(out, "critical") {
		t.Error("expected critical message")
	}
}

func TestFormatVerbose_WarningOnlyNonCritical(t *testing.T) {
	report := Report{
		Results: []Result{
			{Name: "t1", Severity: SeverityCritical, Passed: true, Message: "ok"},
			{Name: "t2", Severity: SeverityWarning, Passed: false, Message: "gh not auth"},
		},
		HasWarning: true,
	}
	var buf bytes.Buffer
	FormatVerbose(&buf, report)
	out := buf.String()
	if !strings.Contains(out, "warning") {
		t.Error("expected warning message")
	}
	if !strings.Contains(out, "non-critical") {
		t.Error("expected non-critical qualifier")
	}
}

func TestFormatCompact_CriticalAbort(t *testing.T) {
	report := Report{
		Results: []Result{
			{Name: "t1", Severity: SeverityCritical, Passed: false, Message: "tmux missing"},
			{Name: "t2", Severity: SeverityWarning, Passed: false, Message: "gh not auth"},
		},
		HasCritical: true,
		HasWarning:  true,
	}
	var buf bytes.Buffer
	FormatCompact(&buf, report)
	out := buf.String()
	if !strings.Contains(out, "Aborting") {
		t.Error("expected abort message")
	}
}

func TestFormatCompact_WarningNoAbort(t *testing.T) {
	report := Report{
		Results: []Result{
			{Name: "t1", Severity: SeverityWarning, Passed: false, Message: "gh not auth"},
		},
		HasWarning: true,
	}
	var buf bytes.Buffer
	FormatCompact(&buf, report)
	out := buf.String()
	if strings.Contains(out, "Aborting") {
		t.Error("warnings should not abort")
	}
}

// --- RunAll coverage ---

func TestRunAll_MultipleCriticalAndWarning(t *testing.T) {
	checks := []Check{
		func() Result {
			return Result{Name: "c1", Severity: SeverityCritical, Passed: false, Message: "fail1"}
		},
		func() Result {
			return Result{Name: "c2", Severity: SeverityCritical, Passed: false, Message: "fail2"}
		},
		func() Result {
			return Result{Name: "w1", Severity: SeverityWarning, Passed: false, Message: "warn1"}
		},
	}
	report := RunAll(checks)
	if !report.HasCritical {
		t.Error("expected HasCritical")
	}
	if !report.HasWarning {
		t.Error("expected HasWarning")
	}
	if len(report.Results) != 3 {
		t.Errorf("expected 3 results, got %d", len(report.Results))
	}
}

func TestRunAll_InfoFail(t *testing.T) {
	checks := []Check{
		func() Result {
			return Result{Name: "i1", Severity: SeverityInfo, Passed: false, Message: "info fail"}
		},
	}
	report := RunAll(checks)
	if report.HasCritical {
		t.Error("info fail should not set HasCritical")
	}
	if report.HasWarning {
		t.Error("info fail should not set HasWarning")
	}
}

// --- DispatchChecks and AllChecks ---

func TestDispatchChecks_Count(t *testing.T) {
	checks := DispatchChecks()
	if len(checks) < 8 {
		t.Errorf("expected at least 8 dispatch checks, got %d", len(checks))
	}
}

func TestAllChecks_Count(t *testing.T) {
	all := AllChecks()
	dispatch := DispatchChecks()
	if len(all) <= len(dispatch) {
		t.Error("AllChecks should include more than DispatchChecks")
	}
}

// --- iconFor ---

func TestIconFor_WarningPassed(t *testing.T) {
	r := Result{Severity: SeverityWarning, Passed: true}
	icon := iconFor(r)
	if icon == "" {
		t.Error("expected non-empty icon")
	}
}
