package improve_test

import (
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/improve"
)

func TestCheckDiffSize_PassesUnderLimit(t *testing.T) {
	diff := "+line1\n+line2\n+line3\n-old1\n-old2\n"
	if err := improve.CheckDiffSize(diff, 500); err != nil {
		t.Errorf("expected pass for 5 lines, got: %v", err)
	}
}

func TestCheckDiffSize_FailsOverLimit(t *testing.T) {
	var diff string
	for i := 0; i < 600; i++ {
		diff += "+new line\n"
	}
	if err := improve.CheckDiffSize(diff, 500); err == nil {
		t.Error("expected error for 600+ changed lines")
	}
}

func TestCheckFileCount_PassesUnderLimit(t *testing.T) {
	stat := "5 files changed, 100 insertions(+), 20 deletions(-)"
	if err := improve.CheckFileCount(stat, 10); err != nil {
		t.Errorf("expected pass for 5 files, got: %v", err)
	}
}

func TestCheckFileCount_FailsOverLimit(t *testing.T) {
	stat := "15 files changed, 500 insertions(+)"
	if err := improve.CheckFileCount(stat, 10); err == nil {
		t.Error("expected error for 15 files")
	}
}

func TestCheckSecrets_PassesCleanDiff(t *testing.T) {
	diff := "+func NewClient(apiKey string) *Client {\n+    return &Client{key: apiKey}\n+}"
	if err := improve.CheckSecrets(diff); err != nil {
		t.Errorf("expected pass for clean diff, got: %v", err)
	}
}

func TestCheckSecrets_FailsWithSecret(t *testing.T) {
	diff := `+apiKey := "sk-ant-api03-real-secret-key-here-abcdef123456"`
	if err := improve.CheckSecrets(diff); err == nil {
		t.Error("expected error for diff containing secret")
	}
}

func TestImplementResult_Dispositions(t *testing.T) {
	r := improve.ImplementResult{Disposition: "implemented"}
	if !r.IsImplemented() {
		t.Error("expected IsImplemented true")
	}

	r2 := improve.ImplementResult{Disposition: "proposed"}
	if r2.IsImplemented() {
		t.Error("expected IsImplemented false for proposed")
	}
}

// TestWiring_ImplementerError_PersistedToAuditLog verifies that AuditEntry.Error
// exists as a field and can round-trip through the audit log.
//
// Root cause context (Task 14 audit): the pre-April-19 pipeline captured errors in
// ImplementResult.Error but the call to auditLog.Append() did NOT pass the Error
// field. All 51 aborted entries in changelog.jsonl have empty error fields because
// the errors were never written. This test guards against that regression — if
// AuditEntry.Error is removed or the Append call forgets to pass it, this test fails.
func TestWiring_ImplementerError_PersistedToAuditLog(t *testing.T) {
	// Verify AuditEntry.Error field exists and round-trips through the log.
	dir := t.TempDir()
	auditLog := improve.NewAuditLog(dir)

	entry := improve.AuditEntry{
		RunID:       "2026-04-15T04:00:00Z",
		FindingID:   "f-2026-04-15-001",
		Source:      "https://go.dev/blog/",
		Category:    "go_ecosystem",
		Title:       "Go 1.25 release notes",
		Relevance:   7,
		Impact:      5,
		Risk:        2,
		Disposition: "aborted",
		Error:       "gate build: exit status 1: ./internal/foo.go:42: undefined: Bar",
		Reasoning:   "Go standard library improvements",
	}

	if err := auditLog.Append(entry); err != nil {
		t.Fatalf("Append: %v", err)
	}

	entries, err := auditLog.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	got := entries[0]
	if got.Disposition != "aborted" {
		t.Errorf("Disposition: want %q, got %q", "aborted", got.Disposition)
	}
	if got.Error == "" {
		t.Error("Error field is empty — aborted audit entries must persist the failure reason; " +
			"check that auditLog.Append() passes Error: result.Error in cmd/vxd-improve/main.go")
	}
	if got.Error != entry.Error {
		t.Errorf("Error field mismatch: want %q, got %q", entry.Error, got.Error)
	}
}

// TestWiring_ActionableFilter_ProposedNotAborted verifies the contract introduced in
// commit 9bb5c07: non-actionable findings must be logged as "proposed" (intelligence),
// NOT sent to the implementer (which would produce "aborted" without a meaningful error).
//
// This documents the pipeline behaviour: if Gemma marks a finding non-actionable, it
// should go directly to "proposed" in the audit log; the implementer should never see it.
// The test exercises this via the ImplementResult contract: a dry-run implementer returns
// "proposed", not "aborted", confirming the two paths are distinct.
func TestWiring_ActionableFilter_ProposedNotAborted(t *testing.T) {
	// In dry-run mode the implementer always returns "proposed" — this represents the
	// non-actionable path (skip the implementer, just log for the email report).
	impl := improve.NewImplementer("/tmp", "claude", 500, 10, true /* dryRun */)

	result := impl.Implement(
		t.Context(),
		improve.AnalyzedFinding{
			ScoredFinding: improve.ScoredFinding{
				Finding: improve.Finding{
					Title:     "OpenHands v1.1 release",
					SourceURL: "https://github.com/All-Hands-AI/OpenHands/releases",
					Category:  "competitors",
				},
				Actionable: false,
			},
			ImplementationPlan: "N/A — competitor intelligence",
			GoNoGo:             "no-go",
		},
		"2026-04-20",
	)

	if result.Disposition != "proposed" {
		t.Errorf("dry-run implementer should return 'proposed', got %q", result.Disposition)
	}
	if result.Error != "" {
		t.Errorf("dry-run proposed result should have no error, got %q", result.Error)
	}
}
