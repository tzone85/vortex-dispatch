package improve_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/improve"
)

func TestBuildEmailHTML_IncludesAllSections(t *testing.T) {
	data := improve.EmailData{
		Date:       "2026-04-08",
		PRsCreated: 2,
		AlertCount: 1,
		Summary:    "Found 12 improvements, implemented 2, proposed 3.",
		PRs: []improve.EmailPR{
			{Title: "Add iterator support", URL: "https://github.com/test/pull/1", Category: "go_ecosystem", TestsPassed: true, LinesChanged: 87},
		},
		Trends: []improve.EmailSection{
			{Title: "Go 1.24 Released", Content: "New iterator support in stdlib", SourceURL: "https://go.dev/blog/"},
		},
		SecurityAlerts: []improve.EmailSection{
			{Title: "CVE-2026-1234", Content: "HTTP smuggling vulnerability", SourceURL: "https://vuln.go.dev/"},
		},
	}

	html, err := improve.BuildEmailHTML(data)
	if err != nil {
		t.Fatalf("build HTML: %v", err)
	}

	checks := []string{
		"VXD Daily Improvement Report",
		"github.com/test/pull/1",
		"CVE-2026-1234",
		"Go 1.24 Released",
		"#summary",
	}
	for _, check := range checks {
		if !strings.Contains(html, check) {
			t.Errorf("missing %q in HTML output", check)
		}
	}
}

func TestBuildEmailHTML_OmitsEmptySections(t *testing.T) {
	data := improve.EmailData{
		Date:    "2026-04-08",
		Summary: "Quiet day.",
	}

	html, _ := improve.BuildEmailHTML(data)
	if strings.Contains(html, "PRs Created") {
		t.Error("should omit empty PRs section")
	}
	if strings.Contains(html, "Security Alerts") {
		t.Error("should omit empty security section")
	}
}

func TestBuildChartURL_ReturnsValidURL(t *testing.T) {
	url := improve.BuildChartURL("bar", map[string]any{
		"labels": []string{"Mon", "Tue", "Wed"},
		"data":   []int{1, 2, 3},
	})
	if !strings.HasPrefix(url, "https://quickchart.io/chart?") {
		t.Errorf("expected quickchart URL, got %q", url)
	}
}

func TestBuildEmailHTML_IncludesOpportunities(t *testing.T) {
	data := improve.EmailData{
		Date:    "2026-04-09",
		Summary: "Test summary.",
		Opportunities: []improve.EmailOpportunity{
			{
				Title:    "Build REST API",
				Source:   "jobicy",
				Budget:   "$5000-$10000",
				Rank:     47,
				Status:   "new",
				HasDraft: false,
			},
			{
				Title:    "Fix OAuth flow",
				Source:   "algora",
				Budget:   "$500",
				Rank:     35,
				Status:   "proposal_drafted",
				HasDraft: true,
			},
		},
		OpportunityStats: &improve.OpportunityStats{
			TotalPipeline:    52,
			NewToday:         3,
			ProposalsDrafted: 1,
			TotalRevenue:     5000,
		},
	}

	html, err := improve.BuildEmailHTML(data)
	if err != nil {
		t.Fatalf("build HTML: %v", err)
	}

	checks := []string{
		"Opportunities Found Today",
		"Build REST API",
		"Fix OAuth flow",
		"jobicy",
		"algora",
		"47",
		"Pipeline: 52",
	}
	for _, check := range checks {
		if !strings.Contains(html, check) {
			t.Errorf("missing %q in HTML output", check)
		}
	}
}

func TestBuildEmailHTML_IncludesMissionMilestone(t *testing.T) {
	data := improve.EmailData{
		Date:    "2026-04-09",
		Summary: "Test summary.",
		MissionMilestone: &improve.MissionMilestoneData{
			Amount:  5000,
			Message: "You started this to free your village from poverty.",
		},
	}

	html, err := improve.BuildEmailHTML(data)
	if err != nil {
		t.Fatalf("build HTML: %v", err)
	}

	if !strings.Contains(html, "Mission Milestone") {
		t.Error("missing Mission Milestone header")
	}
	if !strings.Contains(html, "5000") {
		t.Error("missing milestone amount")
	}
	if !strings.Contains(html, "village") {
		t.Error("missing mission reminder text")
	}
}

func TestBuildEmailHTML_OmitsOpportunitiesWhenEmpty(t *testing.T) {
	data := improve.EmailData{
		Date:    "2026-04-09",
		Summary: "Quiet day.",
	}

	html, _ := improve.BuildEmailHTML(data)
	if strings.Contains(html, "Opportunities Found") {
		t.Error("should omit empty opportunities section")
	}
}

func TestSendEmail_ResendAPI(t *testing.T) {
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer re-test-key" {
			t.Errorf("expected auth header")
		}

		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"email-123"}`))
	}))
	defer server.Close()

	sender := improve.NewEmailSender("re-test-key", server.URL)
	err := sender.Send(context.Background(), "Test Subject", "<h1>Test</h1>", "test@example.com", "from@test.com")
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	if receivedBody["subject"] != "Test Subject" {
		t.Errorf("expected subject, got %v", receivedBody["subject"])
	}
}
