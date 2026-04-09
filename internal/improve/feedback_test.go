package improve_test

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/improve"
)

func TestFeedbackLoop_AppendAndRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "feedback.jsonl")
	fl := improve.NewFeedbackLoop(path)

	entry := improve.FeedbackEntry{
		Type:      "proposal",
		Category:  "backend_api",
		Source:    "jobicy",
		SkillSet:  "Go,PostgreSQL",
		Outcome:   "won",
		Timestamp: time.Now(),
	}

	if err := fl.AppendFeedback(entry); err != nil {
		t.Fatalf("append: %v", err)
	}

	entries, err := fl.ReadFeedback()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Source != "jobicy" {
		t.Errorf("expected source jobicy, got %s", entries[0].Source)
	}
	if entries[0].Outcome != "won" {
		t.Errorf("expected outcome won, got %s", entries[0].Outcome)
	}
}

func TestFeedbackLoop_ReadNonexistentFile(t *testing.T) {
	fl := improve.NewFeedbackLoop("/tmp/nonexistent-feedback-test.jsonl")
	entries, err := fl.ReadFeedback()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entries != nil {
		t.Fatalf("expected nil entries for missing file, got %d", len(entries))
	}
}

func TestFeedbackLoop_ComputeSuccessRates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "feedback.jsonl")
	fl := improve.NewFeedbackLoop(path)

	// Add enough entries for meaningful rates
	entries := []improve.FeedbackEntry{
		{Type: "proposal", Source: "jobicy", Outcome: "won"},
		{Type: "proposal", Source: "jobicy", Outcome: "won"},
		{Type: "proposal", Source: "jobicy", Outcome: "lost"},
		{Type: "proposal", Source: "jobicy", Outcome: "lost"},
		{Type: "proposal", Source: "jobicy", Outcome: "won"},
		{Type: "proposal", Source: "remotive", Outcome: "lost"},
		{Type: "proposal", Source: "remotive", Outcome: "lost"},
		{Type: "proposal", Source: "remotive", Outcome: "lost"},
		{Type: "proposal", Source: "remotive", Outcome: "lost"},
		{Type: "proposal", Source: "remotive", Outcome: "won"},
	}
	for _, e := range entries {
		if err := fl.AppendFeedback(e); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	rates, err := fl.ComputeSuccessRates()
	if err != nil {
		t.Fatalf("compute: %v", err)
	}

	// Find source rates
	rateMap := make(map[string]improve.SuccessRate)
	for _, r := range rates {
		rateMap[r.Dimension] = r
	}

	jobicy, ok := rateMap["source:jobicy"]
	if !ok {
		t.Fatal("missing source:jobicy rate")
	}
	if jobicy.Total != 5 {
		t.Errorf("expected 5 total for jobicy, got %d", jobicy.Total)
	}
	if jobicy.Successes != 3 {
		t.Errorf("expected 3 successes for jobicy, got %d", jobicy.Successes)
	}
	if math.Abs(jobicy.Rate-0.6) > 0.01 {
		t.Errorf("expected rate 0.6 for jobicy, got %f", jobicy.Rate)
	}

	remotive, ok := rateMap["source:remotive"]
	if !ok {
		t.Fatal("missing source:remotive rate")
	}
	if remotive.Successes != 1 {
		t.Errorf("expected 1 success for remotive, got %d", remotive.Successes)
	}
}

func TestFeedbackLoop_ComputeAdjustments_InsufficientData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "feedback.jsonl")
	fl := improve.NewFeedbackLoop(path)

	// Only 2 entries — below the minimum of 5
	for i := 0; i < 2; i++ {
		fl.AppendFeedback(improve.FeedbackEntry{
			Type:    "proposal",
			Source:  "jobicy",
			Outcome: "won",
		})
	}

	adjustments := fl.ComputeAdjustments()
	if len(adjustments) != 0 {
		t.Errorf("expected 0 adjustments with insufficient data, got %d", len(adjustments))
	}
}

func TestFeedbackLoop_ComputeAdjustments_WithData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "feedback.jsonl")
	fl := improve.NewFeedbackLoop(path)

	// 30 entries for high confidence (all won from jobicy)
	for i := 0; i < 30; i++ {
		fl.AppendFeedback(improve.FeedbackEntry{
			Type:    "proposal",
			Source:  "jobicy",
			Outcome: "won",
		})
	}

	adjustments := fl.ComputeAdjustments()
	if len(adjustments) == 0 {
		t.Fatal("expected adjustments with 30 samples")
	}

	// Find the source:jobicy adjustment
	var found *improve.ScoringAdjustment
	for _, adj := range adjustments {
		if adj.Dimension == "source:jobicy" {
			found = &adj
			break
		}
	}
	if found == nil {
		t.Fatal("missing source:jobicy adjustment")
	}

	// 100% success rate -> multiplier should be 1.5 (0.5 + 1.0)
	if math.Abs(found.Multiplier-1.5) > 0.01 {
		t.Errorf("expected multiplier ~1.5 for 100%% success, got %f", found.Multiplier)
	}
	if math.Abs(found.Confidence-1.0) > 0.01 {
		t.Errorf("expected confidence 1.0 with 30 samples, got %f", found.Confidence)
	}
}

func TestFeedbackLoop_ComputeAdjustments_MixedOutcomes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "feedback.jsonl")
	fl := improve.NewFeedbackLoop(path)

	// 10 entries, 5 won 5 lost = 50% rate
	for i := 0; i < 5; i++ {
		fl.AppendFeedback(improve.FeedbackEntry{
			Type: "proposal", Source: "mixed_source", Outcome: "won",
		})
	}
	for i := 0; i < 5; i++ {
		fl.AppendFeedback(improve.FeedbackEntry{
			Type: "proposal", Source: "mixed_source", Outcome: "lost",
		})
	}

	adjustments := fl.ComputeAdjustments()
	var found *improve.ScoringAdjustment
	for _, adj := range adjustments {
		if adj.Dimension == "source:mixed_source" {
			found = &adj
			break
		}
	}
	if found == nil {
		t.Fatal("missing source:mixed_source adjustment")
	}

	// 50% rate -> raw multiplier = 1.0 -> blended toward 1.0
	if math.Abs(found.Multiplier-1.0) > 0.05 {
		t.Errorf("expected multiplier ~1.0 for 50%% success, got %f", found.Multiplier)
	}

	// 10/30 = 0.33 confidence
	expectedConf := 10.0 / 30.0
	if math.Abs(found.Confidence-expectedConf) > 0.01 {
		t.Errorf("expected confidence ~%.2f, got %f", expectedConf, found.Confidence)
	}
}

func TestFeedbackLoop_AdjustOpportunityScore(t *testing.T) {
	fl := improve.NewFeedbackLoop("/dev/null") // path not used here

	opp := improve.Opportunity{
		ID:     "opp-001",
		Source: "jobicy",
		Rank:   100,
	}

	adjustments := []improve.ScoringAdjustment{
		{Dimension: "source:jobicy", Multiplier: 1.5, Confidence: 1.0},
	}

	adjusted := fl.AdjustOpportunityScore(opp, adjustments)
	if adjusted.Rank != 150 {
		t.Errorf("expected rank 150 after 1.5x multiplier, got %d", adjusted.Rank)
	}

	// Original should be unchanged (immutability)
	if opp.Rank != 100 {
		t.Errorf("original rank mutated: expected 100, got %d", opp.Rank)
	}
}

func TestFeedbackLoop_AdjustOpportunityScore_NoMatch(t *testing.T) {
	fl := improve.NewFeedbackLoop("/dev/null")

	opp := improve.Opportunity{
		ID:     "opp-001",
		Source: "hn",
		Rank:   100,
	}

	adjustments := []improve.ScoringAdjustment{
		{Dimension: "source:jobicy", Multiplier: 1.5, Confidence: 1.0},
	}

	adjusted := fl.AdjustOpportunityScore(opp, adjustments)
	if adjusted.Rank != 100 {
		t.Errorf("expected unchanged rank 100, got %d", adjusted.Rank)
	}
}

func TestFeedbackLoop_AdjustOpportunityScore_EmptyAdjustments(t *testing.T) {
	fl := improve.NewFeedbackLoop("/dev/null")

	opp := improve.Opportunity{ID: "opp-001", Rank: 50}
	adjusted := fl.AdjustOpportunityScore(opp, nil)
	if adjusted.Rank != 50 {
		t.Errorf("expected unchanged rank, got %d", adjusted.Rank)
	}
}

func TestFeedbackLoop_GenerateInsights(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "feedback.jsonl")
	fl := improve.NewFeedbackLoop(path)

	// Add 10 entries per source for insights
	for i := 0; i < 8; i++ {
		fl.AppendFeedback(improve.FeedbackEntry{
			Type: "proposal", Source: "jobicy", Outcome: "won",
		})
	}
	for i := 0; i < 2; i++ {
		fl.AppendFeedback(improve.FeedbackEntry{
			Type: "proposal", Source: "jobicy", Outcome: "lost",
		})
	}
	for i := 0; i < 1; i++ {
		fl.AppendFeedback(improve.FeedbackEntry{
			Type: "proposal", Source: "remotive", Outcome: "won",
		})
	}
	for i := 0; i < 9; i++ {
		fl.AppendFeedback(improve.FeedbackEntry{
			Type: "proposal", Source: "remotive", Outcome: "lost",
		})
	}

	insights := fl.GenerateInsights()
	if insights == "" {
		t.Fatal("expected non-empty insights")
	}
	if !contains(insights, "jobicy") {
		t.Error("insights should mention jobicy")
	}
	if !contains(insights, "remotive") {
		t.Error("insights should mention remotive")
	}
	if !contains(insights, "80%") {
		t.Errorf("insights should show 80%% for jobicy, got:\n%s", insights)
	}
}

func TestFeedbackLoop_GenerateInsights_NoData(t *testing.T) {
	fl := improve.NewFeedbackLoop("/tmp/nonexistent-insights-test.jsonl")
	insights := fl.GenerateInsights()
	if insights != "" {
		t.Errorf("expected empty insights for no data, got: %s", insights)
	}
}

func TestFeedbackLoop_ZeroDataGraceful(t *testing.T) {
	// The feedback loop should work gracefully with zero data
	fl := improve.NewFeedbackLoop("/tmp/definitely-does-not-exist.jsonl")

	rates, err := fl.ComputeSuccessRates()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rates) != 0 {
		t.Errorf("expected 0 rates, got %d", len(rates))
	}

	adjustments := fl.ComputeAdjustments()
	if len(adjustments) != 0 {
		t.Errorf("expected 0 adjustments, got %d", len(adjustments))
	}

	opp := improve.Opportunity{ID: "opp-1", Rank: 42}
	adjusted := fl.AdjustOpportunityScore(opp, adjustments)
	if adjusted.Rank != 42 {
		t.Errorf("expected unchanged rank 42, got %d", adjusted.Rank)
	}

	insights := fl.GenerateInsights()
	if insights != "" {
		t.Errorf("expected empty insights, got: %s", insights)
	}
}

func TestFeedbackLoop_SkipsMalformedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "feedback.jsonl")

	// Write a valid entry, then garbage, then another valid entry
	f, _ := os.Create(path)
	f.WriteString(`{"type":"proposal","source":"jobicy","outcome":"won","timestamp":"2026-01-01T00:00:00Z"}` + "\n")
	f.WriteString("this is not json\n")
	f.WriteString(`{"type":"proposal","source":"hn","outcome":"lost","timestamp":"2026-01-02T00:00:00Z"}` + "\n")
	f.Close()

	fl := improve.NewFeedbackLoop(path)
	entries, err := fl.ReadFeedback()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 valid entries (skipping malformed), got %d", len(entries))
	}
}

func TestFeedbackLoop_MultipleAppends(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "feedback.jsonl")
	fl := improve.NewFeedbackLoop(path)

	for i := 0; i < 5; i++ {
		fl.AppendFeedback(improve.FeedbackEntry{
			Type: "pr", Source: "github", Outcome: "merged",
		})
	}

	entries, _ := fl.ReadFeedback()
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries after 5 appends, got %d", len(entries))
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
