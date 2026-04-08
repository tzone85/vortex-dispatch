package improve_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/improve"
)

func TestAuditLog_AppendAndRead(t *testing.T) {
	dir := t.TempDir()
	log := improve.NewAuditLog(dir)

	entry := improve.AuditEntry{
		RunID:       "2026-04-08T06:00:00Z",
		FindingID:   "f-001",
		Source:      "https://go.dev/blog/",
		Category:    "go_ecosystem",
		Title:       "Go 1.24 iterators",
		Relevance:   8,
		Impact:      7,
		Risk:        3,
		Disposition: "implemented",
		PRURL:       "https://github.com/test/repo/pull/1",
		TestsPassed: true,
		Reasoning:   "Iterators simplify DAG traversal",
	}

	if err := log.Append(entry); err != nil {
		t.Fatalf("append: %v", err)
	}

	entries, err := log.ReadAll()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].FindingID != "f-001" {
		t.Errorf("expected finding ID 'f-001', got %q", entries[0].FindingID)
	}
}

func TestAuditLog_MultipleAppends(t *testing.T) {
	dir := t.TempDir()
	log := improve.NewAuditLog(dir)

	for i := 0; i < 3; i++ {
		log.Append(improve.AuditEntry{
			RunID:     "run-1",
			FindingID: "f-" + string(rune('a'+i)),
			Title:     "Finding",
		})
	}

	entries, _ := log.ReadAll()
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
}

func TestRunSummary_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	runsDir := filepath.Join(dir, "runs")

	summary := improve.RunSummary{
		RunID:            "2026-04-08T06:00:00Z",
		StartedAt:        time.Date(2026, 4, 8, 6, 0, 0, 0, time.UTC),
		CompletedAt:      time.Date(2026, 4, 8, 6, 12, 0, 0, time.UTC),
		SourcesScraped:   12,
		FindingsTotal:    27,
		FindingsRelevant: 8,
		PRsCreated:       3,
		PRsProposed:      2,
		EmailSent:        true,
	}

	if err := improve.SaveRunSummary(runsDir, "2026-04-08", summary); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := improve.LoadRunSummary(runsDir, "2026-04-08")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.PRsCreated != 3 {
		t.Errorf("expected 3 PRs, got %d", loaded.PRsCreated)
	}
	if !loaded.EmailSent {
		t.Error("expected email_sent true")
	}
}

func TestRunSummary_IdempotencyCheck(t *testing.T) {
	dir := t.TempDir()
	runsDir := filepath.Join(dir, "runs")

	if improve.IsRunComplete(runsDir, "2026-04-08") {
		t.Error("expected run not complete when no summary exists")
	}

	improve.SaveRunSummary(runsDir, "2026-04-08", improve.RunSummary{EmailSent: false})
	if improve.IsRunComplete(runsDir, "2026-04-08") {
		t.Error("expected run not complete when email not sent")
	}

	improve.SaveRunSummary(runsDir, "2026-04-08", improve.RunSummary{EmailSent: true})
	if !improve.IsRunComplete(runsDir, "2026-04-08") {
		t.Error("expected run complete when email sent")
	}
}

func TestAuditLog_ReadSince(t *testing.T) {
	dir := t.TempDir()
	log := improve.NewAuditLog(dir)

	now := time.Now().UTC()
	log.Append(improve.AuditEntry{RunID: now.Format(time.RFC3339), FindingID: "recent", Title: "Recent"})
	old := now.AddDate(0, 0, -60)
	log.Append(improve.AuditEntry{RunID: old.Format(time.RFC3339), FindingID: "old", Title: "Old"})

	recent, _ := log.ReadSince(now.AddDate(0, 0, -30))
	if len(recent) != 1 {
		t.Fatalf("expected 1 recent entry, got %d", len(recent))
	}
	if recent[0].FindingID != "recent" {
		t.Errorf("expected 'recent', got %q", recent[0].FindingID)
	}
}
