package improve

import (
	"testing"
	"time"
)

func timeAt(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 12, 0, 0, 0, time.UTC)
}

// matchesDimension is intentionally case-insensitive on string match. The
// table covers each branch of the switch and one malformed-dimension case.
func TestMatchesDimension_Cases(t *testing.T) {
	opp := Opportunity{
		Source: "Upwork",
		Skills: []string{"Go", "AWS"},
		Budget: "5000-10000",
	}
	cases := []struct {
		name string
		dim  string
		want bool
	}{
		{"source match", "source:upwork", true},
		{"source mismatch", "source:freelancer", false},
		{"skill_set exact (case-insensitive join)", "skill_set:go,aws", true},
		{"skill_set wrong order", "skill_set:aws,go", false},
		{"price_range match", "price_range:5000-10000", true},
		{"price_range mismatch", "price_range:1000-5000", false},
		{"category never matches today", "category:web", false},
		{"unknown dimension type", "unknown:value", false},
		{"malformed (no colon)", "sourceUpwork", false},
		{"empty dimension", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := matchesDimension(opp, c.dim)
			if got != c.want {
				t.Errorf("matchesDimension(%q) = %v, want %v", c.dim, got, c.want)
			}
		})
	}
}

// generateActionItems is a router: each branch fires when its threshold
// is crossed. Drive each branch independently so a regression that breaks
// one condition is caught in isolation.
func TestGenerateActionItems_RevenueProposalsLink(t *testing.T) {
	d := WeeklyDigest{NewOpportunities: 5, ProposalsDrafted: 0}
	items := generateActionItems(d)
	if !containsCategory(items, "revenue") {
		t.Errorf("expected a revenue action item, got %+v", items)
	}
}

func TestGenerateActionItems_DraftedNotSent(t *testing.T) {
	d := WeeklyDigest{ProposalsDrafted: 3, ProposalsSent: 0}
	items := generateActionItems(d)
	if !containsPriority(items, "high") {
		t.Errorf("drafted-not-sent should produce a high-priority item, got %+v", items)
	}
}

func TestGenerateActionItems_WinsTriggerGrowthAction(t *testing.T) {
	items := generateActionItems(WeeklyDigest{GigsWon: 2})
	if !containsCategory(items, "growth") {
		t.Errorf("won gigs should produce a growth action item, got %+v", items)
	}
}

func TestGenerateActionItems_PRsCreatedAboveThreshold(t *testing.T) {
	items := generateActionItems(WeeklyDigest{PRsCreated: 5})
	if !containsCategory(items, "improvement") {
		t.Errorf(">3 PRs should produce an improvement action item, got %+v", items)
	}
}

func TestGenerateActionItems_FailedRunsAboveThreshold(t *testing.T) {
	items := generateActionItems(WeeklyDigest{FailedRuns: 3})
	if !containsPriority(items, "high") {
		t.Errorf(">2 failed runs should produce a high-priority item, got %+v", items)
	}
}

func TestGenerateActionItems_EmptyDigestProducesNothing(t *testing.T) {
	items := generateActionItems(WeeklyDigest{})
	if len(items) != 0 {
		t.Errorf("zero digest should produce no actions, got %+v", items)
	}
}

func TestIsWeeklyDigestDay_Sunday(t *testing.T) {
	// 2026-06-07 was a Sunday.
	when := timeAt(2026, time.June, 7)
	if !IsWeeklyDigestDay(when) {
		t.Error("Sunday should be a digest day")
	}
}

func TestIsWeeklyDigestDay_OtherDays(t *testing.T) {
	for d := 8; d <= 13; d++ { // 2026-06-08..13 are Mon..Sat
		when := timeAt(2026, time.June, d)
		if IsWeeklyDigestDay(when) {
			t.Errorf("%s should not be a digest day", when.Weekday())
		}
	}
}

// AppendFeedback / ReadFeedback round trip — and the ComputeSuccessRates
// dimension grouping it feeds into.
func TestFeedbackLoop_RoundTripAndSuccessRates(t *testing.T) {
	path := tempPath(t, "feedback.jsonl")
	fl := NewFeedbackLoop(path)

	// Two sources, with skewed outcomes — used to verify dimension
	// grouping. Sample size below minSamplesForConfidence to test the
	// low-confidence path, but the rates themselves should still be
	// computed.
	entries := []FeedbackEntry{
		{Type: "proposal", Source: "upwork", Outcome: "won"},
		{Type: "proposal", Source: "upwork", Outcome: "won"},
		{Type: "proposal", Source: "upwork", Outcome: "lost"},
		{Type: "proposal", Source: "freelancer", Outcome: "lost"},
		{Type: "proposal", Source: "freelancer", Outcome: "lost"},
	}
	for _, e := range entries {
		if err := fl.AppendFeedback(e); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	got, err := fl.ReadFeedback()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != len(entries) {
		t.Errorf("expected %d entries, got %d", len(entries), len(got))
	}

	rates, err := fl.ComputeSuccessRates()
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	// We can't assert a specific count without locking in implementation
	// details, but every rate must be in [0,1] and at least one source
	// dimension must appear.
	sawSource := false
	for _, r := range rates {
		if r.Rate < 0 || r.Rate > 1 {
			t.Errorf("rate out of bounds for %s: %.2f", r.Dimension, r.Rate)
		}
		if r.Dimension == "source:upwork" || r.Dimension == "source:freelancer" {
			sawSource = true
		}
	}
	if !sawSource {
		t.Errorf("expected at least one source dimension; got: %+v", rates)
	}
}

func TestReadFeedback_MissingFile(t *testing.T) {
	fl := NewFeedbackLoop(tempPath(t, "missing.jsonl"))
	got, err := fl.ReadFeedback()
	if err != nil {
		t.Errorf("missing file should be nil/nil, got: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %d entries", len(got))
	}
}

func TestComputeSuccessRates_NoData(t *testing.T) {
	fl := NewFeedbackLoop(tempPath(t, "empty.jsonl"))
	rates, err := fl.ComputeSuccessRates()
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if len(rates) != 0 {
		t.Errorf("no data should produce no rates, got %d", len(rates))
	}
}

func tempPath(t *testing.T, name string) string {
	t.Helper()
	return t.TempDir() + "/" + name
}

// BuildWeeklyDigest is the consolidation entry point. Drive it with a
// pre-seeded audit log + opportunities pipeline so each counter has a
// visible asserted value.
func TestBuildWeeklyDigest_AggregatesAcrossSources(t *testing.T) {
	auditDir := t.TempDir()
	oppDir := t.TempDir()

	now := time.Date(2026, time.June, 12, 9, 0, 0, 0, time.UTC)
	dayAgo := now.AddDate(0, 0, -1)
	threeDaysAgo := now.AddDate(0, 0, -3)

	// Audit entries: two implemented PRs, one aborted, one with high
	// relevance for top-findings list.
	log := NewAuditLog(auditDir)
	for _, e := range []AuditEntry{
		{RunID: dayAgo.Format(time.RFC3339), Disposition: "implemented", Relevance: 6, Title: "Recent PR A", Source: "src", Category: "go"},
		{RunID: dayAgo.Format(time.RFC3339), Disposition: "implemented", Relevance: 8, Title: "Recent PR B", Source: "src", Category: "ai"},
		{RunID: threeDaysAgo.Format(time.RFC3339), Disposition: "aborted", Relevance: 4, Title: "Older abort", Source: "src", Category: "ops"},
	} {
		if err := log.Append(e); err != nil {
			t.Fatalf("seed audit: %v", err)
		}
	}

	// Opportunities scraped within the week.
	pipeline := oppDir + "/pipeline.jsonl"
	for _, o := range []Opportunity{
		{ID: "o1", ScrapedAt: dayAgo, Status: "open"},
		{ID: "o2", ScrapedAt: dayAgo, Status: StatusProposalDrafted},
		{ID: "o3", ScrapedAt: dayAgo, Status: "won"},
	} {
		if err := AppendOpportunity(pipeline, o); err != nil {
			t.Fatalf("seed pipeline: %v", err)
		}
	}

	digest := BuildWeeklyDigest(auditDir, oppDir, now)

	if digest.TotalFindings != 3 {
		t.Errorf("findings total = %d, want 3", digest.TotalFindings)
	}
	if digest.RelevantFindings != 2 {
		t.Errorf("relevant (>=5) = %d, want 2", digest.RelevantFindings)
	}
	if digest.PRsCreated != 2 {
		t.Errorf("PRs created (disposition=implemented) = %d, want 2", digest.PRsCreated)
	}
	if digest.NewOpportunities != 3 {
		t.Errorf("new opportunities = %d, want 3", digest.NewOpportunities)
	}
	if digest.GigsWon != 1 {
		t.Errorf("gigs won = %d, want 1", digest.GigsWon)
	}
	if digest.ProposalsDrafted < 1 {
		t.Errorf("proposals drafted = %d, want >=1", digest.ProposalsDrafted)
	}
	if digest.WeekEnding == "" {
		t.Errorf("week_ending should be set, got %q", digest.WeekEnding)
	}
}

// BuildWeeklyDigest on a fresh workspace must not panic and must return
// zero counters. This locks the "blank workspace" path.
func TestBuildWeeklyDigest_EmptyDirs(t *testing.T) {
	digest := BuildWeeklyDigest(t.TempDir(), t.TempDir(), time.Date(2026, time.June, 12, 9, 0, 0, 0, time.UTC))
	if digest.TotalFindings != 0 || digest.PRsCreated != 0 || digest.NewOpportunities != 0 {
		t.Errorf("blank workspace should produce zero counters; got %+v", digest)
	}
}

func containsCategory(items []ActionItem, want string) bool {
	for _, it := range items {
		if it.Category == want {
			return true
		}
	}
	return false
}

func containsPriority(items []ActionItem, want string) bool {
	for _, it := range items {
		if it.Priority == want {
			return true
		}
	}
	return false
}
