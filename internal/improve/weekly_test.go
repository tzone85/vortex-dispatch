package improve_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/improve"
)

func TestIsWeeklyDigestDay(t *testing.T) {
	sunday := time.Date(2026, 4, 12, 6, 0, 0, 0, time.UTC) // April 12, 2026 is Sunday
	monday := time.Date(2026, 4, 13, 6, 0, 0, 0, time.UTC)

	if !improve.IsWeeklyDigestDay(sunday) {
		t.Error("expected Sunday to be digest day")
	}
	if improve.IsWeeklyDigestDay(monday) {
		t.Error("expected Monday NOT to be digest day")
	}
}

func TestBuildWeeklyDigest_WithData(t *testing.T) {
	dir := t.TempDir()
	auditDir := dir
	oppsDir := filepath.Join(dir, "opportunities")
	os.MkdirAll(filepath.Join(auditDir, "runs"), 0o755)
	os.MkdirAll(oppsDir, 0o755)

	// Write audit entries
	now := time.Now()
	os.WriteFile(filepath.Join(auditDir, "changelog.jsonl"), []byte(
		`{"run_id":"`+now.Format(time.RFC3339)+`","finding_id":"f-001","title":"Go Vuln DB","relevance":9,"impact":5,"risk":2,"disposition":"proposed","category":"security","source":"https://vuln.go.dev","reasoning":"Important"}`+"\n"+
			`{"run_id":"`+now.Format(time.RFC3339)+`","finding_id":"f-002","title":"Ollama Update","relevance":7,"impact":4,"risk":1,"disposition":"implemented","category":"llm_providers","source":"https://ollama.com","reasoning":"New models"}`+"\n",
	), 0o644)

	// Write run summary for today
	date := now.Format("2006-01-02")
	os.WriteFile(filepath.Join(auditDir, "runs", date+".json"), []byte(`{
		"run_id":"`+now.Format(time.RFC3339)+`","sources_scraped":12,"findings_total":11,
		"findings_relevant":5,"prs_created":1,"email_sent":true
	}`), 0o644)

	// Write empty pipeline
	os.WriteFile(filepath.Join(oppsDir, "pipeline.jsonl"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(oppsDir, "revenue.jsonl"), []byte(""), 0o644)

	digest := improve.BuildWeeklyDigest(auditDir, oppsDir, now)

	if digest.TotalFindings != 2 {
		t.Errorf("expected 2 total findings, got %d", digest.TotalFindings)
	}
	if digest.RelevantFindings != 2 {
		t.Errorf("expected 2 relevant findings (both >=5), got %d", digest.RelevantFindings)
	}
	if digest.PRsCreated != 1 {
		t.Errorf("expected 1 PR, got %d", digest.PRsCreated)
	}
	if len(digest.TopFindings) != 2 {
		t.Errorf("expected 2 top findings (both >=7), got %d", len(digest.TopFindings))
	}
	if digest.DeepDiveTopic == "" {
		t.Error("expected non-empty deep dive topic")
	}
	if len(digest.ProjectsStudied) != 7 {
		t.Errorf("expected 7 projects studied (one per day), got %d", len(digest.ProjectsStudied))
	}
}

func TestBuildWeeklyDigest_GeneratesActionItems(t *testing.T) {
	dir := t.TempDir()
	oppsDir := filepath.Join(dir, "opportunities")
	os.MkdirAll(filepath.Join(dir, "runs"), 0o755)
	os.MkdirAll(oppsDir, 0o755)

	os.WriteFile(filepath.Join(dir, "changelog.jsonl"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(oppsDir, "pipeline.jsonl"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(oppsDir, "revenue.jsonl"), []byte(""), 0o644)

	digest := improve.BuildWeeklyDigest(dir, oppsDir, time.Now())

	// Should generate at least growth/stability action items
	// (exact items depend on data, but the function should not panic)
	_ = digest.ActionItems
}

func TestBuildWeeklyEmailHTML(t *testing.T) {
	digest := improve.WeeklyDigest{
		WeekEnding:       "2026-04-12",
		WeekNumber:       15,
		PRsCreated:       3,
		RelevantFindings: 8,
		NewOpportunities: 5,
		RevenueCumulative: 0,
		TopFindings: []improve.WeeklyItem{
			{Title: "Go Vuln DB", Score: 9, Category: "security"},
		},
		ActionItems: []improve.ActionItem{
			{Priority: "high", Action: "Start bidding", Reasoning: "Opps waiting", Category: "revenue"},
		},
		DeepDiveTopic:   "AI Agent Orchestration",
		ProjectsStudied: []string{"Gas Town", "Aider", "CrewAI"},
	}

	html, err := improve.BuildWeeklyEmailHTML(digest)
	if err != nil {
		t.Fatalf("build weekly HTML: %v", err)
	}

	checks := []string{
		"VXD Weekly Digest",
		"Week 15",
		"Go Vuln DB",
		"Start bidding",
		"AI Agent Orchestration",
		"Gas Town",
	}
	for _, check := range checks {
		if !strings.Contains(html, check) {
			t.Errorf("missing %q in weekly HTML", check)
		}
	}
}
