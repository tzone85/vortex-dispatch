package memory

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestActivityLevel(t *testing.T) {
	tests := []struct {
		name     string
		prs      int
		findings int
		commits  int
		want     int
	}{
		{"zero activity", 0, 0, 0, 0},
		{"low activity", 0, 1, 0, 1},
		{"single pr", 1, 0, 0, 1},       // 1*3 = 3, <=3 is level 1
		{"medium activity", 1, 2, 2, 2}, // 3+2+2=7, <=8 is 2
		{"high activity", 3, 5, 3, 3},   // 9+5+3=17, >8 is 3
		{"boundary low", 0, 3, 0, 1},    // 3 <=3 is 1
		{"boundary med", 0, 4, 0, 2},    // 4 <=8 is 2
		{"boundary high", 0, 9, 0, 3},   // 9 >8 is 3
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ActivityLevel(tt.prs, tt.findings, tt.commits)
			if got != tt.want {
				t.Errorf("ActivityLevel(%d, %d, %d) = %d, want %d",
					tt.prs, tt.findings, tt.commits, got, tt.want)
			}
		})
	}
}

func TestExtractDate(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"2026-04-08T14:43:45Z", "2026-04-08"},
		{"2026-01-15T00:00:00Z", "2026-01-15"},
		{"short", ""},
		{"not-a-date-at-all", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := extractDate(tt.input)
			if got != tt.want {
				t.Errorf("extractDate(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBuildTimeline(t *testing.T) {
	dir := t.TempDir()

	// Create changelog.jsonl
	entries := []changelogEntry{
		{RunID: "2026-04-08T14:00:00Z", Title: "Finding A", Category: "security", Relevance: 8},
		{RunID: "2026-04-08T14:00:00Z", Title: "Finding B", Category: "go_ecosystem", Relevance: 6, PRURL: "https://github.com/test/pr/1"},
		{RunID: "2026-04-07T10:00:00Z", Title: "Finding C", Category: "competitors", Relevance: 7},
	}
	writeChangelog(t, dir, entries)

	// Create runs directory with a summary
	runsDir := filepath.Join(dir, "runs")
	if err := os.MkdirAll(runsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	runData := runSummary{
		RunID:            "2026-04-08T14:00:00Z",
		SourcesScraped:   11,
		FindingsTotal:    11,
		FindingsRelevant: 5,
		PRsCreated:       1,
		EmailSent:        true,
	}
	writeJSON(t, filepath.Join(runsDir, "2026-04-08.json"), runData)

	tl, err := BuildTimeline(dir)
	if err != nil {
		t.Fatalf("BuildTimeline: %v", err)
	}

	if len(tl.Entries) < 2 {
		t.Fatalf("expected at least 2 entries, got %d", len(tl.Entries))
	}
	if tl.Min != "2026-04-07" {
		t.Errorf("expected Min=2026-04-07, got %q", tl.Min)
	}
	if tl.Max != "2026-04-08" {
		t.Errorf("expected Max=2026-04-08, got %q", tl.Max)
	}

	// Check the 2026-04-08 entry
	var apr8 *TimelineEntry
	for i := range tl.Entries {
		if tl.Entries[i].Date == "2026-04-08" {
			apr8 = &tl.Entries[i]
			break
		}
	}
	if apr8 == nil {
		t.Fatal("no entry for 2026-04-08")
	}
	if apr8.Findings != 2 {
		t.Errorf("expected 2 findings for 2026-04-08, got %d", apr8.Findings)
	}
	// PRs comes from run summary (1) since PRsCreated > 0
	if apr8.PRs != 1 {
		t.Errorf("expected 1 PR for 2026-04-08, got %d", apr8.PRs)
	}
}

func TestBuildTimeline_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	tl, err := BuildTimeline(dir)
	if err != nil {
		t.Fatalf("BuildTimeline: %v", err)
	}
	if len(tl.Entries) != 0 {
		t.Errorf("expected 0 entries for empty dir, got %d", len(tl.Entries))
	}
	if tl.Today == "" {
		t.Error("Today should be set even for empty timeline")
	}
}

func TestGetDayDetail(t *testing.T) {
	dir := t.TempDir()

	entries := []changelogEntry{
		{
			RunID:       "2026-04-08T14:00:00Z",
			Title:       "Go Vuln DB",
			Category:    "security",
			Source:      "https://vuln.go.dev/",
			Relevance:   9,
			Impact:      5,
			Risk:        2,
			Disposition: "proposed",
			Reasoning:   "Important for Go security",
		},
		{
			RunID:       "2026-04-08T14:00:00Z",
			Title:       "PR Finding",
			Category:    "tooling",
			Source:      "https://example.com",
			Relevance:   7,
			Disposition: "merged",
			PRURL:       "https://github.com/test/pr/42",
			Lines:       150,
		},
		{
			RunID:     "2026-04-07T10:00:00Z",
			Title:     "Other Day Finding",
			Category:  "other",
			Relevance: 5,
		},
	}
	writeChangelog(t, dir, entries)

	runsDir := filepath.Join(dir, "runs")
	if err := os.MkdirAll(runsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(runsDir, "2026-04-08.json"), runSummary{
		SourcesScraped:   11,
		FindingsTotal:    11,
		FindingsRelevant: 5,
		PRsCreated:       1,
		EmailSent:        true,
	})

	dd, err := GetDayDetail(dir, "2026-04-08")
	if err != nil {
		t.Fatalf("GetDayDetail: %v", err)
	}

	if dd.Date != "2026-04-08" {
		t.Errorf("expected date=2026-04-08, got %q", dd.Date)
	}
	if len(dd.Findings) != 2 {
		t.Errorf("expected 2 findings, got %d", len(dd.Findings))
	}
	if len(dd.PRs) != 1 {
		t.Errorf("expected 1 PR, got %d", len(dd.PRs))
	}
	if dd.PRs[0].URL != "https://github.com/test/pr/42" {
		t.Errorf("expected PR URL, got %q", dd.PRs[0].URL)
	}
	if dd.RunSummary == nil {
		t.Fatal("expected run summary to be present")
	}
	if dd.RunSummary.SourcesScraped != 11 {
		t.Errorf("expected SourcesScraped=11, got %d", dd.RunSummary.SourcesScraped)
	}
	if !dd.RunSummary.EmailSent {
		t.Error("expected EmailSent=true")
	}
}

func TestGetDayDetail_NoData(t *testing.T) {
	dir := t.TempDir()

	dd, err := GetDayDetail(dir, "2026-01-01")
	if err != nil {
		t.Fatalf("GetDayDetail: %v", err)
	}
	if len(dd.Findings) != 0 {
		t.Errorf("expected 0 findings, got %d", len(dd.Findings))
	}
	if dd.RunSummary != nil {
		t.Error("expected nil run summary")
	}
}

func TestReadChangelog_MalformedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "changelog.jsonl")

	content := `{"run_id":"2026-04-08T14:00:00Z","title":"Good Entry","category":"test"}
this is not json
{"run_id":"2026-04-08T14:00:00Z","title":"Also Good","category":"test"}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := readChangelog(path)
	if err != nil {
		t.Fatalf("readChangelog: %v", err)
	}
	// Should skip the malformed line
	if len(entries) != 2 {
		t.Errorf("expected 2 entries (skip malformed), got %d", len(entries))
	}
}

func TestReadChangelog_LogsMalformedLineCount(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "changelog.jsonl")

	content := `{"run_id":"2026-04-08T14:00:00Z","title":"Good Entry","category":"test"}
not-json
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(orig)

	if _, err := readChangelog(path); err != nil {
		t.Fatalf("readChangelog: %v", err)
	}

	if !strings.Contains(buf.String(), "skipped 1 malformed changelog lines") {
		t.Fatalf("log output = %q, want malformed line warning", buf.String())
	}
}

// --- test helpers ---

func writeChangelog(t *testing.T, dir string, entries []changelogEntry) {
	t.Helper()
	path := filepath.Join(dir, "changelog.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			t.Fatal(err)
		}
	}
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
