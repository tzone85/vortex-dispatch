package preflight_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/preflight"
)

func TestFormatVerbose_AllPassing(t *testing.T) {
	report := preflight.Report{
		Results: []preflight.Result{
			{Name: "tmux", Severity: preflight.SeverityCritical, Passed: true, Message: "tmux installed (3.4)"},
			{Name: "gh", Severity: preflight.SeverityWarning, Passed: true, Message: "gh CLI authenticated (tzone85)"},
			{Name: "config", Severity: preflight.SeverityInfo, Passed: true, Message: "Config: vxd.yaml (repo)"},
		},
	}

	var buf bytes.Buffer
	preflight.FormatVerbose(&buf, report)
	out := buf.String()

	if !strings.Contains(out, "VXD Pre-Flight Check") {
		t.Fatal("expected header")
	}
	if !strings.Contains(out, "\u2713") {
		t.Fatal("expected check mark for passing check")
	}
	if !strings.Contains(out, "All checks passed") {
		t.Fatal("expected all-pass summary")
	}
}

func TestFormatVerbose_WithFailures(t *testing.T) {
	report := preflight.Report{
		Results: []preflight.Result{
			{Name: "tmux", Severity: preflight.SeverityCritical, Passed: false, Message: "tmux not found"},
			{Name: "gh", Severity: preflight.SeverityWarning, Passed: false, Message: "gh not authenticated"},
		},
		HasCritical: true,
		HasWarning:  true,
	}

	var buf bytes.Buffer
	preflight.FormatVerbose(&buf, report)
	out := buf.String()

	if !strings.Contains(out, "\u2717") {
		t.Fatal("expected X mark for critical failure")
	}
	if !strings.Contains(out, "\u26a0") {
		t.Fatal("expected warning symbol")
	}
}

func TestFormatCompact_CriticalOnly(t *testing.T) {
	report := preflight.Report{
		Results: []preflight.Result{
			{Name: "tmux", Severity: preflight.SeverityCritical, Passed: false, Message: "tmux not found"},
			{Name: "claude", Severity: preflight.SeverityCritical, Passed: true, Message: "claude OK"},
		},
		HasCritical: true,
	}

	var buf bytes.Buffer
	preflight.FormatCompact(&buf, report)
	out := buf.String()

	if !strings.Contains(out, "tmux not found") {
		t.Fatal("expected critical failure message")
	}
	if strings.Contains(out, "claude OK") {
		t.Fatal("compact should not show passing checks")
	}
}

func TestFormatCompact_NothingWhenAllPass(t *testing.T) {
	report := preflight.Report{
		Results: []preflight.Result{
			{Name: "tmux", Severity: preflight.SeverityCritical, Passed: true, Message: "tmux OK"},
		},
	}

	var buf bytes.Buffer
	preflight.FormatCompact(&buf, report)
	out := buf.String()

	if out != "" {
		t.Fatalf("compact should print nothing when all pass, got: %q", out)
	}
}
