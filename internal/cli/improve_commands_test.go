package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/improve"
)

// withChdir chdirs into the given dir for the duration of the test. The
// improve commands read from $PWD/docs/self-improvement — we need control
// over that.
func withChdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}

// seedAuditDir creates docs/self-improvement under root, returns the
// audit-dir path. Tests then append entries via NewAuditLog.
func seedAuditDir(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, "docs", "self-improvement")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	return dir
}

func TestRunImproveLog_EmptyAuditDir(t *testing.T) {
	root := t.TempDir()
	withChdir(t, root)

	cmd := newImproveLogCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Errorf("execute empty log: %v", err)
	}
}

func TestRunImproveLog_FiltersByDisposition(t *testing.T) {
	root := t.TempDir()
	withChdir(t, root)
	dir := seedAuditDir(t, root)
	log := improve.NewAuditLog(dir)

	now := time.Now().UTC().Format(time.RFC3339)
	for _, e := range []improve.AuditEntry{
		{RunID: now, FindingID: "f-1", Title: "Imp 1", Disposition: "implemented"},
		{RunID: now, FindingID: "f-2", Title: "Prop 1", Disposition: "proposed"},
		{RunID: now, FindingID: "f-3", Title: "Imp 2", Disposition: "implemented"},
	} {
		if err := log.Append(e); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	cmd := newImproveLogCmd()
	if err := cmd.Flags().Set("disposition", "implemented"); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	if err := cmd.Execute(); err != nil {
		t.Errorf("execute: %v", err)
	}
}

func TestRunImproveLog_InvalidSinceDate(t *testing.T) {
	root := t.TempDir()
	withChdir(t, root)
	cmd := newImproveLogCmd()
	if err := cmd.Flags().Set("since", "not-a-date"); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	err := cmd.Execute()
	if err == nil {
		t.Error("expected error for invalid since date")
	}
	if !strings.Contains(err.Error(), "invalid date") {
		t.Errorf("expected 'invalid date' in error, got: %v", err)
	}
}

func TestRunImproveLog_SinceFilter(t *testing.T) {
	root := t.TempDir()
	withChdir(t, root)
	dir := seedAuditDir(t, root)
	log := improve.NewAuditLog(dir)

	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format(time.RFC3339)
	lastMonth := time.Now().UTC().AddDate(0, -1, 0).Format(time.RFC3339)
	for _, e := range []improve.AuditEntry{
		{RunID: yesterday, FindingID: "fresh", Disposition: "implemented"},
		{RunID: lastMonth, FindingID: "stale", Disposition: "implemented"},
	} {
		if err := log.Append(e); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	cmd := newImproveLogCmd()
	sinceDate := time.Now().AddDate(0, 0, -7).Format("2006-01-02")
	if err := cmd.Flags().Set("since", sinceDate); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	if err := cmd.Execute(); err != nil {
		t.Errorf("execute: %v", err)
	}
}

func TestRunImproveLog_JSONOutput(t *testing.T) {
	root := t.TempDir()
	withChdir(t, root)
	dir := seedAuditDir(t, root)
	log := improve.NewAuditLog(dir)
	if err := log.Append(improve.AuditEntry{
		RunID:       time.Now().UTC().Format(time.RFC3339),
		FindingID:   "f-json",
		Title:       "Test",
		Disposition: "implemented",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	cmd := newImproveLogCmd()
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	if err := cmd.Execute(); err != nil {
		t.Errorf("execute: %v", err)
	}
}

func TestRunImproveLog_ErrorsOnly(t *testing.T) {
	root := t.TempDir()
	withChdir(t, root)
	dir := seedAuditDir(t, root)
	log := improve.NewAuditLog(dir)
	now := time.Now().UTC().Format(time.RFC3339)
	for _, e := range []improve.AuditEntry{
		{RunID: now, FindingID: "f-ok", Disposition: "implemented"},
		{RunID: now, FindingID: "f-bad", Disposition: "aborted", Error: "boom"},
	} {
		if err := log.Append(e); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	cmd := newImproveLogCmd()
	if err := cmd.Flags().Set("errors", "true"); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	if err := cmd.Execute(); err != nil {
		t.Errorf("execute: %v", err)
	}
}

func TestRunImproveRuns_NoRunsDir(t *testing.T) {
	root := t.TempDir()
	withChdir(t, root)
	cmd := newImproveRunsCmd()
	err := cmd.Execute()
	if err == nil {
		t.Error("expected error reading missing runs dir")
	}
}

func TestRunImproveRuns_WithSummaries(t *testing.T) {
	root := t.TempDir()
	withChdir(t, root)
	runsDir := filepath.Join(seedAuditDir(t, root), "runs")
	if err := os.MkdirAll(runsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, date := range []string{"2026-06-10", "2026-06-11", "2026-06-12"} {
		if err := improve.SaveRunSummary(runsDir, date, improve.RunSummary{
			RunID:            date,
			StartedAt:        time.Now(),
			CompletedAt:      time.Now().Add(5 * time.Minute),
			FindingsTotal:    5,
			FindingsRelevant: 2,
			PRsCreated:       1,
			EmailSent:        true,
		}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	cmd := newImproveRunsCmd()
	if err := cmd.Execute(); err != nil {
		t.Errorf("execute: %v", err)
	}
}

func TestRunImproveDetail_FoundEntry(t *testing.T) {
	root := t.TempDir()
	withChdir(t, root)
	dir := seedAuditDir(t, root)
	log := improve.NewAuditLog(dir)
	now := time.Now().UTC().Format(time.RFC3339)
	if err := log.Append(improve.AuditEntry{
		RunID:        now,
		FindingID:    "f-detail-1",
		Title:        "Detail test",
		Disposition:  "implemented",
		Relevance:    8,
		Impact:       7,
		Risk:         2,
		PRURL:        "https://example.com/pr/1",
		TestsPassed:  true,
		FilesChanged: 3,
		LinesChanged: 50,
		Reasoning:    "Strong fit",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	cmd := newImproveDetailCmd()
	cmd.SetArgs([]string{"f-detail-1"})
	if err := cmd.Execute(); err != nil {
		t.Errorf("execute: %v", err)
	}
}

func TestRunImproveDetail_NotFound(t *testing.T) {
	root := t.TempDir()
	withChdir(t, root)
	seedAuditDir(t, root) // exists but empty

	cmd := newImproveDetailCmd()
	cmd.SetArgs([]string{"f-missing"})
	err := cmd.Execute()
	if err == nil {
		t.Error("expected error for unknown finding-id")
	}
}

func TestRunImproveDetail_WithError(t *testing.T) {
	root := t.TempDir()
	withChdir(t, root)
	dir := seedAuditDir(t, root)
	log := improve.NewAuditLog(dir)
	if err := log.Append(improve.AuditEntry{
		RunID:          time.Now().UTC().Format(time.RFC3339),
		FindingID:      "f-err",
		Title:          "Aborted",
		Disposition:    "aborted",
		Error:          "claude failed: max-turns exceeded",
		SecurityReview: "no new inputs",
		Reasoning:      "Stop on error",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cmd := newImproveDetailCmd()
	cmd.SetArgs([]string{"f-err"})
	if err := cmd.Execute(); err != nil {
		t.Errorf("execute: %v", err)
	}
}
