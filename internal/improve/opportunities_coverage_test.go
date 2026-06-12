package improve

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestUpdateOpportunityField_RoundTrip writes a pipeline, mutates one row,
// and asserts only that row changed.
func TestUpdateOpportunityField_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	pipeline := filepath.Join(dir, "pipeline.jsonl")

	for _, opp := range []Opportunity{
		{ID: "opp-1", Title: "A", Rank: 10, Status: "open"},
		{ID: "opp-2", Title: "B", Rank: 20, Status: "open"},
		{ID: "opp-3", Title: "C", Rank: 5, Status: "open"},
	} {
		if err := AppendOpportunity(pipeline, opp); err != nil {
			t.Fatalf("seed %s: %v", opp.ID, err)
		}
	}

	updated, err := UpdateOpportunityField(pipeline, "opp-2", func(o Opportunity) Opportunity {
		o.Status = StatusProposalDrafted
		o.ProposalDraft = "Dear company,..."
		return o
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Status != StatusProposalDrafted || updated.ProposalDraft == "" {
		t.Errorf("update did not return mutated opportunity: %+v", updated)
	}

	got, err := ReadOpportunities(pipeline)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(got))
	}
	for _, o := range got {
		switch o.ID {
		case "opp-1", "opp-3":
			if o.Status != "open" {
				t.Errorf("unrelated row %s mutated: status=%q", o.ID, o.Status)
			}
		case "opp-2":
			if o.Status != StatusProposalDrafted || o.ProposalDraft == "" {
				t.Errorf("target row not persisted: %+v", o)
			}
		}
	}
}

func TestUpdateOpportunityField_NotFound(t *testing.T) {
	dir := t.TempDir()
	pipeline := filepath.Join(dir, "pipeline.jsonl")
	if err := AppendOpportunity(pipeline, Opportunity{ID: "opp-1"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, err := UpdateOpportunityField(pipeline, "missing", func(o Opportunity) Opportunity { return o })
	if err == nil {
		t.Error("expected error for missing opportunity, got nil")
	}
}

func TestUpdateOpportunityField_ReadError(t *testing.T) {
	// Point at a directory rather than a file — ReadOpportunities will fail.
	_, err := UpdateOpportunityField(t.TempDir(), "opp-1", func(o Opportunity) Opportunity { return o })
	if err == nil {
		t.Error("expected error when pipeline path is a directory, got nil")
	}
}

func TestTopN_TruncatesByRankDescending(t *testing.T) {
	in := []Opportunity{
		{ID: "low", Rank: 5},
		{ID: "high", Rank: 100},
		{ID: "mid", Rank: 50},
	}
	got := TopN(in, 2)
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got))
	}
	if got[0].ID != "high" || got[1].ID != "mid" {
		t.Errorf("wrong top-2 order: %+v", got)
	}
}

func TestTopN_FewerThanN(t *testing.T) {
	in := []Opportunity{{ID: "a", Rank: 1}, {ID: "b", Rank: 2}}
	got := TopN(in, 10)
	if len(got) != 2 {
		t.Errorf("expected 2 entries, got %d", len(got))
	}
}

func TestTopN_Empty(t *testing.T) {
	if got := TopN(nil, 3); len(got) != 0 {
		t.Errorf("expected empty result, got %d", len(got))
	}
}

func TestAppendRevenue_AndRead(t *testing.T) {
	dir := t.TempDir()
	ledger := filepath.Join(dir, "nested", "revenue.jsonl")

	entries := []RevenueEntry{
		{OpportunityID: "opp-1", Amount: 1500, Currency: "USD", Date: "2026-06-01", Status: "received"},
		{OpportunityID: "opp-2", Amount: 750, Currency: "USD", Date: "2026-06-05", Status: "pending"},
		{OpportunityID: "opp-3", Amount: 2000, Currency: "USD", Date: "2026-06-10", Status: "received"},
	}
	for _, e := range entries {
		if err := AppendRevenue(ledger, e); err != nil {
			t.Fatalf("append %s: %v", e.OpportunityID, err)
		}
	}

	got, err := ReadRevenue(ledger)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != len(entries) {
		t.Errorf("expected %d entries, got %d", len(entries), len(got))
	}
}

func TestReadRevenue_MissingFile(t *testing.T) {
	got, err := ReadRevenue(filepath.Join(t.TempDir(), "no-such-file.jsonl"))
	if err != nil {
		t.Errorf("missing ledger should be empty, got error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %d entries", len(got))
	}
}

func TestTotalRevenue_OnlyCountsReceived(t *testing.T) {
	entries := []RevenueEntry{
		{Amount: 100, Status: "received"},
		{Amount: 200, Status: "pending"},
		{Amount: 50, Status: "received"},
		{Amount: 999, Status: "refunded"},
	}
	got := TotalRevenue(entries)
	if got != 150 {
		t.Errorf("total = %.2f, want 150.00 (only received entries)", got)
	}
}

func TestTotalRevenue_Empty(t *testing.T) {
	if got := TotalRevenue(nil); got != 0 {
		t.Errorf("empty ledger should sum to 0, got %.2f", got)
	}
}

func TestCheckMilestone_BelowFirst(t *testing.T) {
	if got := CheckMilestone(500); got != 0 {
		t.Errorf("below first threshold should return 0, got %.0f", got)
	}
}

func TestCheckMilestone_BetweenThresholds(t *testing.T) {
	// 1k and 5k are both crossed at 7500.
	if got := CheckMilestone(7500); got != 5000 {
		t.Errorf("between 5k and 10k should return 5000, got %.0f", got)
	}
}

func TestCheckMilestone_ExactMatch(t *testing.T) {
	if got := CheckMilestone(10000); got != 10000 {
		t.Errorf("exact 10k boundary should return 10000, got %.0f", got)
	}
}

func TestCheckMilestone_Highest(t *testing.T) {
	// Above all defined milestones returns the largest.
	largest := MissionMilestones[len(MissionMilestones)-1]
	if got := CheckMilestone(largest * 5); got != largest {
		t.Errorf("above all milestones should return %.0f, got %.0f", largest, got)
	}
}

func TestSortByRank_StableForTies(t *testing.T) {
	in := []Opportunity{
		{ID: "a", Rank: 50},
		{ID: "b", Rank: 50}, // tie with a
		{ID: "c", Rank: 75},
	}
	got := SortByRank(in)
	if got[0].ID != "c" {
		t.Errorf("highest rank should be first, got %s", got[0].ID)
	}
}

// Each Append* helper takes a path and must (a) create the parent dir,
// (b) open the file for append, (c) marshal+write the entry. Passing a
// path whose parent already exists as a regular file forces MkdirAll to
// fail — the cheapest way to hit the error branch without OS games.
func TestAppendOpportunity_MkdirError(t *testing.T) {
	dir := t.TempDir()
	regularFile := filepath.Join(dir, "blocker")
	if err := writeRegularFile(regularFile, "x"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Path resolves "blocker/sub/opp.jsonl" — MkdirAll can't make a dir
	// under a regular file.
	bad := filepath.Join(regularFile, "sub", "opp.jsonl")
	if err := AppendOpportunity(bad, Opportunity{ID: "x"}); err == nil {
		t.Error("expected MkdirAll error, got nil")
	}
}

func TestAppendRevenue_MkdirError(t *testing.T) {
	dir := t.TempDir()
	regularFile := filepath.Join(dir, "blocker")
	if err := writeRegularFile(regularFile, "x"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	bad := filepath.Join(regularFile, "sub", "revenue.jsonl")
	if err := AppendRevenue(bad, RevenueEntry{}); err == nil {
		t.Error("expected MkdirAll error, got nil")
	}
}

func TestAppendDiscoveredSource_MkdirError(t *testing.T) {
	dir := t.TempDir()
	regularFile := filepath.Join(dir, "blocker")
	if err := writeRegularFile(regularFile, "x"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	bad := filepath.Join(regularFile, "sub", "sources.jsonl")
	if err := AppendDiscoveredSource(bad, DiscoveredSource{URL: "https://example.com"}); err == nil {
		t.Error("expected MkdirAll error, got nil")
	}
}

func TestReadDiscoveredSources_MissingFile(t *testing.T) {
	got, err := ReadDiscoveredSources(filepath.Join(t.TempDir(), "nope.jsonl"))
	if err != nil {
		t.Errorf("missing file should be nil/nil, got error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %d entries", len(got))
	}
}

func TestReadDiscoveredSources_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sources.jsonl")
	for _, s := range []DiscoveredSource{
		{URL: "https://a.example", Name: "A", Status: "pending_approval"},
		{URL: "https://b.example", Name: "B", Status: "approved"},
	} {
		if err := AppendDiscoveredSource(path, s); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	got, err := ReadDiscoveredSources(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 sources, got %d", len(got))
	}
}

func writeRegularFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}

func TestKeywordsForDay_EmptySets(t *testing.T) {
	if got := KeywordsForDay(nil, time.Now()); got != nil {
		t.Errorf("nil sets should yield nil, got %v", got)
	}
}

func TestKeywordsForDay_RotatesByYearDay(t *testing.T) {
	sets := [][]string{
		{"a"}, {"b"}, {"c"},
	}
	// Pick days with known YearDay() values: Jan 1 = day 1.
	day1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	got := KeywordsForDay(sets, day1)
	if len(got) != 1 || got[0] != "b" { // 1 % 3 = 1
		t.Errorf("day 1 expected sets[1] (b), got %v", got)
	}
	day3 := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	got = KeywordsForDay(sets, day3)
	if len(got) != 1 || got[0] != "a" { // 3 % 3 = 0
		t.Errorf("day 3 expected sets[0] (a), got %v", got)
	}
}

func TestGenerateOpportunityID_Format(t *testing.T) {
	got := GenerateOpportunityID("2026-06-12", 7)
	want := "opp-2026-06-12-007"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestComputeRank_Weighting(t *testing.T) {
	// (relevance * 3) + (budget * 2) + win_probability
	rank := ComputeRank(Opportunity{RelevanceScore: 8, BudgetScore: 5, WinProbability: 4})
	if rank != 8*3+5*2+4 {
		t.Errorf("got %d, want %d", rank, 8*3+5*2+4)
	}
}

func TestClampScore_BelowZero(t *testing.T) {
	if got := clampScore(-5); got != 0 {
		t.Errorf("negative clamp got %d, want 0", got)
	}
}

func TestClampScore_AboveTen(t *testing.T) {
	if got := clampScore(15); got != 10 {
		t.Errorf("over-ten clamp got %d, want 10", got)
	}
}

func TestClampScore_InRange(t *testing.T) {
	for n := 0; n <= 10; n++ {
		if got := clampScore(n); got != n {
			t.Errorf("in-range %d clamp got %d", n, got)
		}
	}
}

func TestDefaultKeywordSets_NonEmpty(t *testing.T) {
	sets := DefaultKeywordSets()
	if len(sets) != 7 {
		t.Errorf("expected 7-day rotation, got %d sets", len(sets))
	}
	for i, s := range sets {
		if len(s) == 0 {
			t.Errorf("set %d is empty", i)
		}
	}
}

func TestFilterByStatus(t *testing.T) {
	in := []Opportunity{
		{ID: "1", Status: "open"},
		{ID: "2", Status: StatusProposalDrafted},
		{ID: "3", Status: "open"},
		{ID: "4", Status: "won"},
	}
	open := FilterByStatus(in, "open")
	if len(open) != 2 {
		t.Errorf("expected 2 open, got %d", len(open))
	}
	for _, o := range open {
		if o.Status != "open" {
			t.Errorf("filter leaked %q", o.Status)
		}
	}
}
