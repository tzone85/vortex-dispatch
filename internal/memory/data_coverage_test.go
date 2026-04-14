package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOpportunities_ValidData(t *testing.T) {
	dir := t.TempDir()

	opps := []OpportunityDetail{
		{
			ID:             "opp-001",
			Source:         "upwork",
			Title:          "Go Backend Developer",
			Company:        "Acme Corp",
			URL:            "https://upwork.com/job/1",
			Budget:         "$5000",
			Skills:         []string{"Go", "PostgreSQL"},
			Status:         "new",
			RelevanceScore: 85,
			BudgetScore:    70,
			WinProbability: 60,
			Rank:           80,
			EffortEstimate: "40h",
		},
		{
			ID:             "opp-002",
			Source:         "freelancer",
			Title:          "API Integration",
			Company:        "Beta Inc",
			URL:            "https://freelancer.com/job/2",
			Budget:         "$2000",
			Skills:         []string{"REST", "Go"},
			Status:         "proposal_drafted",
			RelevanceScore: 70,
			BudgetScore:    50,
			WinProbability: 40,
			Rank:           55,
			EffortEstimate: "20h",
		},
	}

	writeJSONL(t, filepath.Join(dir, "pipeline.jsonl"), opps)

	loaded, err := LoadOpportunities(dir)
	if err != nil {
		t.Fatalf("LoadOpportunities: %v", err)
	}

	if len(loaded) != 2 {
		t.Fatalf("expected 2 opportunities, got %d", len(loaded))
	}
	if loaded[0].ID != "opp-001" {
		t.Errorf("expected ID=opp-001, got %q", loaded[0].ID)
	}
	if loaded[0].Title != "Go Backend Developer" {
		t.Errorf("expected Title=Go Backend Developer, got %q", loaded[0].Title)
	}
	if loaded[1].Status != "proposal_drafted" {
		t.Errorf("expected Status=proposal_drafted, got %q", loaded[1].Status)
	}
}

func TestLoadOpportunities_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "pipeline.jsonl"), []byte(""), 0o644) //nolint:errcheck

	loaded, err := LoadOpportunities(dir)
	if err != nil {
		t.Fatalf("LoadOpportunities: %v", err)
	}
	if len(loaded) != 0 {
		t.Errorf("expected 0 opportunities for empty file, got %d", len(loaded))
	}
}

func TestLoadOpportunities_FileNotExist(t *testing.T) {
	dir := t.TempDir()

	loaded, err := LoadOpportunities(dir)
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if loaded != nil {
		t.Errorf("expected nil opportunities for missing file, got %d", len(loaded))
	}
}

func TestLoadOpportunities_MalformedLines(t *testing.T) {
	dir := t.TempDir()
	content := `{"id":"opp-001","title":"Good","status":"new"}
not valid json
{"id":"opp-002","title":"Also Good","status":"won"}
`
	os.WriteFile(filepath.Join(dir, "pipeline.jsonl"), []byte(content), 0o644) //nolint:errcheck

	loaded, err := LoadOpportunities(dir)
	if err != nil {
		t.Fatalf("LoadOpportunities: %v", err)
	}
	if len(loaded) != 2 {
		t.Errorf("expected 2 opportunities (skip malformed), got %d", len(loaded))
	}
}

func TestComputeOpportunityStats(t *testing.T) {
	opps := []OpportunityDetail{
		{ID: "1", Status: "new"},
		{ID: "2", Status: "new"},
		{ID: "3", Status: "proposal_drafted"},
		{ID: "4", Status: "won"},
		{ID: "5", Status: "won"},
		{ID: "6", Status: "won"},
	}

	stats := ComputeOpportunityStats(opps, 15000.0)

	if stats.Total != 6 {
		t.Errorf("expected Total=6, got %d", stats.Total)
	}
	if stats.New != 2 {
		t.Errorf("expected New=2, got %d", stats.New)
	}
	if stats.Drafts != 1 {
		t.Errorf("expected Drafts=1, got %d", stats.Drafts)
	}
	if stats.Won != 3 {
		t.Errorf("expected Won=3, got %d", stats.Won)
	}
	if stats.Revenue != 15000.0 {
		t.Errorf("expected Revenue=15000, got %f", stats.Revenue)
	}
}

func TestComputeOpportunityStats_Empty(t *testing.T) {
	stats := ComputeOpportunityStats(nil, 0)

	if stats.Total != 0 {
		t.Errorf("expected Total=0, got %d", stats.Total)
	}
	if stats.Revenue != 0 {
		t.Errorf("expected Revenue=0, got %f", stats.Revenue)
	}
}

func TestComputeOpportunityStats_UnknownStatus(t *testing.T) {
	opps := []OpportunityDetail{
		{ID: "1", Status: "unknown_status"},
		{ID: "2", Status: "lost"},
	}

	stats := ComputeOpportunityStats(opps, 0)

	if stats.Total != 2 {
		t.Errorf("expected Total=2, got %d", stats.Total)
	}
	if stats.New != 0 {
		t.Errorf("expected New=0, got %d", stats.New)
	}
	if stats.Drafts != 0 {
		t.Errorf("expected Drafts=0, got %d", stats.Drafts)
	}
	if stats.Won != 0 {
		t.Errorf("expected Won=0, got %d", stats.Won)
	}
}

func TestLoadDiscoveredSources_ValidData(t *testing.T) {
	dir := t.TempDir()

	sources := []DiscoveredSourceDetail{
		{
			URL:          "https://golang-weekly.com",
			Name:         "Golang Weekly",
			DiscoveredOn: "2026-04-10",
			Reason:       "Go ecosystem news source",
			Status:       "pending",
		},
		{
			URL:          "https://hacker-news.firebaseio.com",
			Name:         "Hacker News API",
			DiscoveredOn: "2026-04-11",
			Reason:       "Tech community",
			Status:       "approved",
		},
	}

	writeJSONL(t, filepath.Join(dir, "discovered_sources.jsonl"), sources)

	loaded, err := LoadDiscoveredSources(dir)
	if err != nil {
		t.Fatalf("LoadDiscoveredSources: %v", err)
	}

	if len(loaded) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(loaded))
	}
	if loaded[0].URL != "https://golang-weekly.com" {
		t.Errorf("expected URL=https://golang-weekly.com, got %q", loaded[0].URL)
	}
	if loaded[1].Status != "approved" {
		t.Errorf("expected Status=approved, got %q", loaded[1].Status)
	}
}

func TestLoadDiscoveredSources_FileNotExist(t *testing.T) {
	dir := t.TempDir()

	loaded, err := LoadDiscoveredSources(dir)
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if loaded != nil {
		t.Errorf("expected nil sources for missing file, got %d", len(loaded))
	}
}

func TestLoadDiscoveredSources_MalformedLines(t *testing.T) {
	dir := t.TempDir()
	content := `{"url":"https://example.com","name":"Good","status":"pending"}
garbage data
{"url":"https://other.com","name":"Also Good","status":"approved"}
`
	os.WriteFile(filepath.Join(dir, "discovered_sources.jsonl"), []byte(content), 0o644) //nolint:errcheck

	loaded, err := LoadDiscoveredSources(dir)
	if err != nil {
		t.Fatalf("LoadDiscoveredSources: %v", err)
	}
	if len(loaded) != 2 {
		t.Errorf("expected 2 sources (skip malformed), got %d", len(loaded))
	}
}

func TestLoadTotalRevenue_ValidData(t *testing.T) {
	dir := t.TempDir()

	entries := []struct {
		Amount float64 `json:"amount"`
		Status string  `json:"status"`
	}{
		{Amount: 1500.0, Status: "received"},
		{Amount: 2500.0, Status: "received"},
		{Amount: 500.0, Status: "pending"},  // should not count
		{Amount: 1000.0, Status: "received"},
	}

	writeJSONL(t, filepath.Join(dir, "revenue.jsonl"), entries)

	total := LoadTotalRevenue(dir)
	expected := 5000.0 // 1500 + 2500 + 1000 (pending excluded)
	if total != expected {
		t.Errorf("expected total=%.2f, got %.2f", expected, total)
	}
}

func TestLoadTotalRevenue_FileNotExist(t *testing.T) {
	dir := t.TempDir()

	total := LoadTotalRevenue(dir)
	if total != 0 {
		t.Errorf("expected 0 for missing file, got %f", total)
	}
}

func TestLoadTotalRevenue_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "revenue.jsonl"), []byte(""), 0o644) //nolint:errcheck

	total := LoadTotalRevenue(dir)
	if total != 0 {
		t.Errorf("expected 0 for empty file, got %f", total)
	}
}

func TestLoadTotalRevenue_OnlyPending(t *testing.T) {
	dir := t.TempDir()

	entries := []struct {
		Amount float64 `json:"amount"`
		Status string  `json:"status"`
	}{
		{Amount: 1000.0, Status: "pending"},
		{Amount: 2000.0, Status: "invoiced"},
	}

	writeJSONL(t, filepath.Join(dir, "revenue.jsonl"), entries)

	total := LoadTotalRevenue(dir)
	if total != 0 {
		t.Errorf("expected 0 when no received entries, got %f", total)
	}
}

// writeJSONL writes a slice of items as JSONL to the given path.
func writeJSONL[T any](t *testing.T, path string, items []T) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, item := range items {
		if err := enc.Encode(item); err != nil {
			t.Fatal(err)
		}
	}
}
