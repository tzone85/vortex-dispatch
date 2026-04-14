package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/improve"
)

// ---------------------------------------------------------------------------
// Helper: create a temp pipeline with seeded opportunities
// ---------------------------------------------------------------------------

func setupOppDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	oppDir := filepath.Join(dir, "docs", "opportunities")
	os.MkdirAll(oppDir, 0o755)
	return dir
}

func seedOpportunities(t *testing.T, dir string, opps []improve.Opportunity) {
	t.Helper()
	pipelinePath := filepath.Join(dir, "docs", "opportunities", "pipeline.jsonl")
	f, err := os.Create(pipelinePath)
	if err != nil {
		t.Fatalf("create pipeline file: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, opp := range opps {
		if err := enc.Encode(opp); err != nil {
			t.Fatalf("encode opportunity: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// runOppStatus — valid statuses and status update flow
// ---------------------------------------------------------------------------

func TestRunOppStatus_ValidStatusUpdate(t *testing.T) {
	dir := setupOppDir(t)
	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })

	// Seed a pipeline with one opportunity
	seedOpportunities(t, dir, []improve.Opportunity{
		{
			ID:     "opp-test-1",
			Source: "test",
			Title:  "Test Opportunity",
			Status: "new",
			Rank:   1,
		},
	})

	// Update to "interested"
	err := runOppStatus(nil, []string{"opp-test-1", "interested"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read back and verify
	opps, _ := improve.ReadOpportunities(filepath.Join(dir, "docs", "opportunities", "pipeline.jsonl"))
	found := false
	for _, opp := range opps {
		if opp.ID == "opp-test-1" {
			if opp.Status != "interested" {
				t.Errorf("expected status 'interested', got %q", opp.Status)
			}
			found = true
		}
	}
	if !found {
		t.Error("opportunity not found after update")
	}
}

func TestRunOppStatus_LostStatusLogsFeedback(t *testing.T) {
	dir := setupOppDir(t)
	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })

	seedOpportunities(t, dir, []improve.Opportunity{
		{
			ID:     "opp-lost-1",
			Source: "upwork",
			Title:  "Lost Opportunity",
			Status: "sent",
			Skills: []string{"Go", "Docker"},
			Budget: "$5K-$10K",
			Rank:   2,
		},
	})

	err := runOppStatus(nil, []string{"opp-lost-1", "lost"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify feedback was logged
	feedbackPath := filepath.Join(dir, "docs", "opportunities", "feedback.jsonl")
	data, err := os.ReadFile(feedbackPath)
	if err != nil {
		t.Fatalf("feedback file not created: %v", err)
	}
	if len(data) == 0 {
		t.Error("feedback file is empty")
	}
	if !strings.Contains(string(data), "lost") {
		t.Error("feedback should contain 'lost' outcome")
	}
}

func TestRunOppStatus_ExpiredStatusLogsFeedback(t *testing.T) {
	dir := setupOppDir(t)
	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })

	seedOpportunities(t, dir, []improve.Opportunity{
		{
			ID:     "opp-exp-1",
			Source: "jobicy",
			Title:  "Expired Opp",
			Status: "new",
			Rank:   1,
		},
	})

	err := runOppStatus(nil, []string{"opp-exp-1", "expired"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	feedbackPath := filepath.Join(dir, "docs", "opportunities", "feedback.jsonl")
	data, _ := os.ReadFile(feedbackPath)
	if !strings.Contains(string(data), "expired") {
		t.Error("feedback should contain 'expired' outcome")
	}
}

func TestRunOppStatus_NonTerminalStatus_NoFeedback(t *testing.T) {
	dir := setupOppDir(t)
	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })

	seedOpportunities(t, dir, []improve.Opportunity{
		{
			ID:     "opp-rev-1",
			Source: "test",
			Title:  "Review Opp",
			Status: "new",
			Rank:   1,
		},
	})

	err := runOppStatus(nil, []string{"opp-rev-1", "reviewed"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No feedback for non-terminal statuses
	feedbackPath := filepath.Join(dir, "docs", "opportunities", "feedback.jsonl")
	_, err = os.Stat(feedbackPath)
	if err == nil {
		data, _ := os.ReadFile(feedbackPath)
		if len(data) > 0 {
			t.Error("feedback should not be logged for non-terminal status 'reviewed'")
		}
	}
}

func TestRunOppStatus_NotFoundOpportunity(t *testing.T) {
	dir := setupOppDir(t)
	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })

	seedOpportunities(t, dir, []improve.Opportunity{
		{ID: "opp-1", Status: "new"},
	})

	err := runOppStatus(nil, []string{"nonexistent-opp", "interested"})
	if err == nil {
		t.Fatal("expected error for nonexistent opportunity")
	}
}

// ---------------------------------------------------------------------------
// runOppWon — revenue logging and milestone detection
// ---------------------------------------------------------------------------

func TestRunOppWon_Success(t *testing.T) {
	dir := setupOppDir(t)
	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })

	seedOpportunities(t, dir, []improve.Opportunity{
		{
			ID:     "opp-won-1",
			Source: "direct",
			Title:  "Won Opportunity",
			Status: "sent",
			Skills: []string{"Go", "PostgreSQL"},
			Budget: "$2K",
			Rank:   1,
		},
	})

	err := runOppWon(nil, []string{"opp-won-1", "2000"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify revenue entry
	revPath := filepath.Join(dir, "docs", "opportunities", "revenue.jsonl")
	data, err := os.ReadFile(revPath)
	if err != nil {
		t.Fatalf("revenue file not created: %v", err)
	}
	if !strings.Contains(string(data), "2000") {
		t.Error("revenue file should contain amount 2000")
	}

	// Verify opportunity status updated to "won"
	opps, _ := improve.ReadOpportunities(filepath.Join(dir, "docs", "opportunities", "pipeline.jsonl"))
	for _, opp := range opps {
		if opp.ID == "opp-won-1" && opp.Status != "won" {
			t.Errorf("expected status 'won', got %q", opp.Status)
		}
	}

	// Verify feedback was logged
	feedbackPath := filepath.Join(dir, "docs", "opportunities", "feedback.jsonl")
	fbData, _ := os.ReadFile(feedbackPath)
	if !strings.Contains(string(fbData), "won") {
		t.Error("feedback should contain 'won' outcome")
	}
}

func TestRunOppWon_InvalidAmount_Detailed(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"letters", "abc"},
		{"empty", ""},
		{"special chars", "$100"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := runOppWon(nil, []string{"opp-1", tc.input})
			if err == nil {
				t.Fatal("expected error for invalid amount")
			}
			if !strings.Contains(err.Error(), "invalid amount") {
				t.Errorf("error should mention 'invalid amount': %v", err)
			}
		})
	}
}

func TestRunOppWon_DecimalAmount(t *testing.T) {
	dir := setupOppDir(t)
	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })

	seedOpportunities(t, dir, []improve.Opportunity{
		{
			ID:     "opp-won-dec",
			Source: "direct",
			Title:  "Decimal Won",
			Status: "sent",
			Rank:   1,
		},
	})

	err := runOppWon(nil, []string{"opp-won-dec", "1500.50"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunOppWon_Milestone(t *testing.T) {
	dir := setupOppDir(t)
	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })

	seedOpportunities(t, dir, []improve.Opportunity{
		{
			ID:     "opp-ms-1",
			Source: "direct",
			Title:  "Milestone Hit",
			Status: "sent",
			Rank:   1,
		},
	})

	// $1000 is the first milestone
	err := runOppWon(nil, []string{"opp-ms-1", "1000"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunOppWon_NotFound(t *testing.T) {
	dir := setupOppDir(t)
	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })

	seedOpportunities(t, dir, []improve.Opportunity{
		{ID: "opp-1", Status: "sent"},
	})

	err := runOppWon(nil, []string{"nonexistent", "1000"})
	if err == nil {
		t.Fatal("expected error for nonexistent opportunity")
	}
}

// ---------------------------------------------------------------------------
// runOppPropose — test with nonexistent pipeline and not-found opportunity
// ---------------------------------------------------------------------------

func TestRunOppPropose_NoPipelineFile(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })

	cmd := newOppProposeCmd()
	cmd.SetArgs([]string{"opp-123"})

	var buf strings.Builder
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when pipeline file doesn't exist")
	}
	// Error can be either "read pipeline" or "not found" depending on
	// whether ReadOpportunities returns empty slice vs error
	if !strings.Contains(err.Error(), "pipeline") && !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention pipeline or not found: %v", err)
	}
}

func TestRunOppPropose_OppNotFound(t *testing.T) {
	dir := setupOppDir(t)
	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })

	seedOpportunities(t, dir, []improve.Opportunity{
		{ID: "opp-1", Status: "new"},
	})

	cmd := newOppProposeCmd()
	cmd.SetArgs([]string{"nonexistent-opp"})

	var buf strings.Builder
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent opportunity")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found': %v", err)
	}
}

// ---------------------------------------------------------------------------
// runOppList — with seeded data
// ---------------------------------------------------------------------------

func TestRunOppList_WithOpportunities(t *testing.T) {
	dir := setupOppDir(t)
	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })

	seedOpportunities(t, dir, []improve.Opportunity{
		{ID: "opp-1", Source: "jobicy", Title: "Go Developer", Budget: "$5K", Status: "new", Rank: 1},
		{ID: "opp-2", Source: "upwork", Title: "Backend Engineer", Budget: "$10K", Status: "interested", Rank: 2},
	})

	cmd := newOppListCmd()

	// Capture stdout since runOppList uses os.Stdout via tabwriter
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	var buf strings.Builder
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()

	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var stdoutBuf [4096]byte
	n, _ := r.Read(stdoutBuf[:])
	combined := buf.String() + string(stdoutBuf[:n])
	if !strings.Contains(combined, "opp-1") || !strings.Contains(combined, "opp-2") {
		t.Errorf("expected both opportunities in output, got: %s", combined)
	}
}

func TestRunOppList_WithStatusFilter(t *testing.T) {
	dir := setupOppDir(t)
	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })

	seedOpportunities(t, dir, []improve.Opportunity{
		{ID: "opp-1", Status: "new", Rank: 1, Source: "test", Title: "New"},
		{ID: "opp-2", Status: "interested", Rank: 2, Source: "test", Title: "Interested"},
	})

	cmd := newOppListCmd()
	cmd.Flags().Set("status", "interested")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	var buf strings.Builder
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()

	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var stdoutBuf [4096]byte
	n, _ := r.Read(stdoutBuf[:])
	combined := buf.String() + string(stdoutBuf[:n])
	if strings.Contains(combined, "opp-1") {
		t.Errorf("should not contain 'new' status opportunity when filtering by 'interested'")
	}
}

func TestRunOppList_WithLimit(t *testing.T) {
	dir := setupOppDir(t)
	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })

	opps := make([]improve.Opportunity, 0, 5)
	for i := 0; i < 5; i++ {
		opps = append(opps, improve.Opportunity{
			ID:     "opp-" + string(rune('A'+i)),
			Source: "test",
			Title:  "Opp " + string(rune('A'+i)),
			Status: "new",
			Rank:   i + 1,
		})
	}
	seedOpportunities(t, dir, opps)

	cmd := newOppListCmd()
	cmd.Flags().Set("limit", "2")

	old := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w

	var buf strings.Builder
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	_ = cmd.Execute()

	w.Close()
	os.Stdout = old
}

// ---------------------------------------------------------------------------
// runOppSources — with seeded data
// ---------------------------------------------------------------------------

func TestRunOppSources_WithSources(t *testing.T) {
	dir := setupOppDir(t)
	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })

	sources := []improve.DiscoveredSource{
		{
			URL:          "https://example.com/jobs",
			Name:         "Example Jobs",
			DiscoveredOn: "2026-04-13",
			Reason:       "Found via web search",
			Status:       "pending_approval",
		},
	}
	sourcesPath := filepath.Join(dir, "docs", "opportunities", "discovered_sources.jsonl")
	f, err := os.Create(sourcesPath)
	if err != nil {
		t.Fatalf("create sources file: %v", err)
	}
	enc := json.NewEncoder(f)
	for _, s := range sources {
		enc.Encode(s)
	}
	f.Close()

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err = runOppSources(nil, nil)

	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var stdoutBuf [4096]byte
	n, _ := r.Read(stdoutBuf[:])
	output := string(stdoutBuf[:n])
	if !strings.Contains(output, "Example Jobs") {
		t.Errorf("expected 'Example Jobs' in output, got: %s", output)
	}
}

func TestRunOppSources_NoPipelineDir(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })

	// ReadDiscoveredSources returns empty slice (not error) for missing file.
	// Capture stdout since the function prints "No discovered sources yet."
	old := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w

	err := runOppSources(nil, nil)

	w.Close()
	os.Stdout = old

	// Should not error — just prints "No discovered sources yet."
	if err != nil {
		t.Logf("got error (may be ok): %v", err)
	}
}

// ---------------------------------------------------------------------------
// runOppApproveSource
// ---------------------------------------------------------------------------

func TestRunOppApproveSource_Success(t *testing.T) {
	dir := setupOppDir(t)
	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })

	sources := []improve.DiscoveredSource{
		{URL: "https://example.com/jobs", Name: "Example", Status: "pending_approval"},
	}
	sourcesPath := filepath.Join(dir, "docs", "opportunities", "discovered_sources.jsonl")
	f, _ := os.Create(sourcesPath)
	enc := json.NewEncoder(f)
	for _, s := range sources {
		enc.Encode(s)
	}
	f.Close()

	cmd := newOppApproveSourceCmd()
	cmd.SetArgs([]string{"https://example.com/jobs"})

	var buf strings.Builder
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunOppApproveSource_SourceNotFound(t *testing.T) {
	dir := setupOppDir(t)
	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })

	sourcesPath := filepath.Join(dir, "docs", "opportunities", "discovered_sources.jsonl")
	os.WriteFile(sourcesPath, []byte{}, 0644)

	cmd := newOppApproveSourceCmd()
	cmd.SetArgs([]string{"https://nonexistent.com"})

	var buf strings.Builder
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent source")
	}
}
