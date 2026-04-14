package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// runPreflight — verbose and JSON output modes
// ---------------------------------------------------------------------------

func TestRunPreflight_VerboseOutput(t *testing.T) {
	cmd := newPreflightCmd()

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	// runPreflight calls os.Exit(1) on critical failures, but we can't test that.
	// Instead, test via RunE directly.
	err := runPreflight(cmd, nil)
	_ = err

	output := buf.String()
	if output == "" {
		t.Error("expected preflight output, got empty")
	}
	// Output uses unicode symbols (checkmarks, warning signs, info) not PASS/FAIL text.
	// Just verify we got the header.
	if !strings.Contains(output, "Pre-Flight") {
		t.Errorf("expected 'Pre-Flight' header in output, got: %s", output[:min(len(output), 200)])
	}
}

func TestRunPreflight_JSONOutput(t *testing.T) {
	cmd := newPreflightCmd()
	cmd.Flags().Set("json", "true")

	// The JSON is printed to stdout via fmt.Println, not cmd.OutOrStdout().
	// Capture real stdout.
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	_ = runPreflight(cmd, nil)

	w.Close()
	os.Stdout = oldStdout

	var stdoutBuf bytes.Buffer
	stdoutBuf.ReadFrom(r)
	output := stdoutBuf.String()

	if output == "" {
		t.Error("expected JSON output on stdout, got empty")
	}
	if !strings.Contains(output, "{") {
		t.Errorf("expected JSON, got: %s", output[:min(len(output), 200)])
	}
	if !strings.Contains(output, "Results") {
		t.Errorf("expected 'Results' key in JSON, got: %s", output[:min(len(output), 200)])
	}
}

// ---------------------------------------------------------------------------
// runDispatchPreflight — not-skipped path
// ---------------------------------------------------------------------------

func TestRunDispatchPreflight_NotSkipped(t *testing.T) {
	cmd := newReqCmd()
	cmd.PersistentFlags().Bool("skip-preflight", false, "")
	// Don't set skip-preflight, so preflight will run

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := runDispatchPreflight(cmd)
	// May or may not error depending on environment, but shouldn't panic
	if err != nil && strings.Contains(err.Error(), "critical") {
		// Expected on CI — critical preflight issues
		t.Logf("preflight found critical issues (expected in test env): %v", err)
	}
}

func TestRunDispatchPreflight_SkipFlagTrue(t *testing.T) {
	cmd := newReqCmd()
	cmd.PersistentFlags().Bool("skip-preflight", false, "")
	cmd.PersistentFlags().Set("skip-preflight", "true")

	err := runDispatchPreflight(cmd)
	if err != nil {
		t.Fatalf("expected nil when preflight skipped, got: %v", err)
	}
}
