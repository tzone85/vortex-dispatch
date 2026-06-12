package improve

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAuditLog_AppendAndReadAll(t *testing.T) {
	log := NewAuditLog(t.TempDir())

	entries := []AuditEntry{
		{RunID: "2026-06-10T10:00:00Z", FindingID: "f-1", Title: "First", Disposition: "implemented"},
		{RunID: "2026-06-11T10:00:00Z", FindingID: "f-2", Title: "Second", Disposition: "proposed"},
		{RunID: "2026-06-12T10:00:00Z", FindingID: "f-3", Title: "Third", Disposition: "aborted"},
	}
	for _, e := range entries {
		if err := log.Append(e); err != nil {
			t.Fatalf("append %s: %v", e.FindingID, err)
		}
	}

	got, err := log.ReadAll()
	if err != nil {
		t.Fatalf("read all: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("expected 3 entries, got %d", len(got))
	}
}

func TestAuditLog_ReadSince_FiltersByRunID(t *testing.T) {
	log := NewAuditLog(t.TempDir())
	for _, e := range []AuditEntry{
		{RunID: "2026-06-10T10:00:00Z", FindingID: "old"},
		{RunID: "2026-06-11T10:00:00Z", FindingID: "edge"},
		{RunID: "2026-06-12T10:00:00Z", FindingID: "recent"},
	} {
		if err := log.Append(e); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	cutoff, _ := time.Parse(time.RFC3339, "2026-06-11T10:00:00Z")
	got, err := log.ReadSince(cutoff)
	if err != nil {
		t.Fatalf("read since: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 entries on/after cutoff, got %d", len(got))
	}
	ids := map[string]bool{}
	for _, e := range got {
		ids[e.FindingID] = true
	}
	if !ids["edge"] || !ids["recent"] || ids["old"] {
		t.Errorf("filter wrong; got: %+v", ids)
	}
}

func TestAuditLog_ReadSince_SkipsBadRunIDs(t *testing.T) {
	log := NewAuditLog(t.TempDir())
	for _, e := range []AuditEntry{
		{RunID: "not-a-timestamp", FindingID: "skip-me"},
		{RunID: "2026-06-11T10:00:00Z", FindingID: "keep-me"},
	} {
		if err := log.Append(e); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	cutoff := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	got, _ := log.ReadSince(cutoff)
	if len(got) != 1 || got[0].FindingID != "keep-me" {
		t.Errorf("malformed RunID should be skipped; got %+v", got)
	}
}

func TestAuditLog_ReadAll_EmptyFile(t *testing.T) {
	// Read before any Append: file does not exist yet — should return
	// nil/nil without error.
	log := NewAuditLog(t.TempDir())
	got, err := log.ReadAll()
	if err != nil {
		t.Errorf("missing file should not error, got: %v", err)
	}
	if got != nil {
		t.Errorf("missing file should be nil, got %d entries", len(got))
	}
}

func TestSaveAndLoadRunSummary_RoundTrip(t *testing.T) {
	runsDir := filepath.Join(t.TempDir(), "runs")
	summary := RunSummary{
		RunID:            "2026-06-12",
		StartedAt:        time.Date(2026, 6, 12, 6, 0, 0, 0, time.UTC),
		CompletedAt:      time.Date(2026, 6, 12, 6, 30, 0, 0, time.UTC),
		FindingsTotal:    42,
		FindingsRelevant: 10,
		PRsCreated:       2,
		EmailSent:        true,
		Errors:           []string{"one minor blip"},
	}

	if err := SaveRunSummary(runsDir, "2026-06-12", summary); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := LoadRunSummary(runsDir, "2026-06-12")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.RunID != summary.RunID || got.FindingsTotal != 42 || !got.EmailSent {
		t.Errorf("round trip lost data; got %+v", got)
	}
	if len(got.Errors) != 1 || got.Errors[0] != "one minor blip" {
		t.Errorf("errors lost in round trip; got %+v", got.Errors)
	}
}

func TestLoadRunSummary_MissingFile(t *testing.T) {
	_, err := LoadRunSummary(t.TempDir(), "no-such-date")
	if err == nil {
		t.Error("expected error for missing run summary, got nil")
	}
}
