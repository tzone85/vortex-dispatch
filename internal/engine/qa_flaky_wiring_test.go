package engine_test

import (
	"os"
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/config"
)

// qa_flaky_wiring_test.go — proves the flaky-retry feature is ACTIVATED on the
// dispatch path, not merely implemented (same source-scan pattern as
// TestResume_WiresTechLeadFixer / TestWiring_MetricsCommand_Registered).

// TestWiring_QAFlakyRetries_DefaultEnabled pins qa.flaky_retries default = 1:
// a fresh install retries a failed TEST step once before calling the failure
// real (lint/build are never retried). 0 disables retrying.
func TestWiring_QAFlakyRetries_DefaultEnabled(t *testing.T) {
	cfg := config.DefaultConfig()
	if cfg.QA.FlakyRetries != 1 {
		t.Errorf("WIRING FAILURE: qa.flaky_retries default = %d, want 1", cfg.QA.FlakyRetries)
	}
}

// TestWiring_QAFlakyRetries_FromConfig proves the config knob reaches the QA
// stage: buildQAConfig (internal/cli/resume.go) must copy qa.flaky_retries
// into engine.QAConfig, otherwise the retry logic exists but never fires.
func TestWiring_QAFlakyRetries_FromConfig(t *testing.T) {
	src, err := os.ReadFile("../cli/resume.go")
	if err != nil {
		t.Fatalf("read resume.go: %v", err)
	}
	if !strings.Contains(string(src), "qaCfg.FlakyRetries = cfg.QA.FlakyRetries") {
		t.Error("WIRING FAILURE: buildQAConfig does not map qa.flaky_retries into engine.QAConfig — " +
			"the flaky-retry feature exists but is never activated on the dispatch path")
	}
}
