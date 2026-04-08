package improve_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/improve"
)

func TestOpportunityStatus_Lifecycle(t *testing.T) {
	statuses := []string{
		improve.StatusNew,
		improve.StatusReviewed,
		improve.StatusInterested,
		improve.StatusProposalDrafted,
		improve.StatusSent,
		improve.StatusWon,
		improve.StatusLost,
		improve.StatusExpired,
	}
	if len(statuses) != 8 {
		t.Errorf("expected 8 status values, got %d", len(statuses))
	}
}

func TestKeywordsForDay_RotatesDaily(t *testing.T) {
	keywords := improve.DefaultKeywordSets()
	day1 := time.Date(2026, 4, 8, 6, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 4, 9, 6, 0, 0, 0, time.UTC)

	kw1 := improve.KeywordsForDay(keywords, day1)
	kw2 := improve.KeywordsForDay(keywords, day2)

	if len(kw1) == 0 {
		t.Error("expected non-empty keywords for day 1")
	}
	// Different days should return different keyword sets (7-day rotation)
	if kw1[0] == kw2[0] {
		t.Error("expected different keywords on consecutive days")
	}
}

func TestKeywordsForDay_WrapsAfter7Days(t *testing.T) {
	keywords := improve.DefaultKeywordSets()
	day1 := time.Date(2026, 4, 8, 6, 0, 0, 0, time.UTC)
	day8 := time.Date(2026, 4, 15, 6, 0, 0, 0, time.UTC)

	kw1 := improve.KeywordsForDay(keywords, day1)
	kw8 := improve.KeywordsForDay(keywords, day8)

	if kw1[0] != kw8[0] {
		t.Errorf("expected same keywords after 7-day wrap, got %q and %q", kw1[0], kw8[0])
	}
}

func TestGenerateOpportunityID(t *testing.T) {
	id := improve.GenerateOpportunityID("2026-04-09", 1)
	if id != "opp-2026-04-09-001" {
		t.Errorf("expected opp-2026-04-09-001, got %q", id)
	}
	id2 := improve.GenerateOpportunityID("2026-04-09", 42)
	if id2 != "opp-2026-04-09-042" {
		t.Errorf("expected opp-2026-04-09-042, got %q", id2)
	}
}

func TestComputeRank(t *testing.T) {
	opp := improve.Opportunity{
		RelevanceScore: 8,
		BudgetScore:    8,
		WinProbability: 7,
	}
	rank := improve.ComputeRank(opp)
	// (relevance * 3) + (budget * 2) + win_probability = 24 + 16 + 7 = 47
	if rank != 47 {
		t.Errorf("expected rank 47, got %d", rank)
	}
}

func TestOpportunityPipeline_WriteAndRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pipeline.jsonl")

	opp := improve.Opportunity{
		ID:     "opp-2026-04-09-001",
		Source: "jobicy",
		Title:  "Build REST API",
		URL:    "https://jobicy.com/test",
		Status: improve.StatusNew,
	}

	if err := improve.AppendOpportunity(path, opp); err != nil {
		t.Fatalf("append: %v", err)
	}

	opps, err := improve.ReadOpportunities(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(opps) != 1 {
		t.Fatalf("expected 1 opportunity, got %d", len(opps))
	}
	if opps[0].ID != "opp-2026-04-09-001" {
		t.Errorf("expected ID opp-2026-04-09-001, got %q", opps[0].ID)
	}
	if opps[0].Source != "jobicy" {
		t.Errorf("expected source jobicy, got %q", opps[0].Source)
	}
}

func TestOpportunityPipeline_UpdateStatus(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pipeline.jsonl")

	opp := improve.Opportunity{
		ID:     "opp-2026-04-09-001",
		Source: "jobicy",
		Title:  "Build REST API",
		Status: improve.StatusNew,
	}
	improve.AppendOpportunity(path, opp)

	updated, err := improve.UpdateOpportunityStatus(path, "opp-2026-04-09-001", improve.StatusInterested)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Status != improve.StatusInterested {
		t.Errorf("expected status interested, got %q", updated.Status)
	}

	// Re-read and verify
	opps, _ := improve.ReadOpportunities(path)
	if opps[0].Status != improve.StatusInterested {
		t.Errorf("expected persisted status interested, got %q", opps[0].Status)
	}
}

func TestReadOpportunities_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pipeline.jsonl")
	os.WriteFile(path, []byte{}, 0o644)

	opps, err := improve.ReadOpportunities(path)
	if err != nil {
		t.Fatalf("read empty: %v", err)
	}
	if len(opps) != 0 {
		t.Errorf("expected 0 opportunities, got %d", len(opps))
	}
}

func TestReadOpportunities_FileNotExist(t *testing.T) {
	opps, err := improve.ReadOpportunities("/nonexistent/pipeline.jsonl")
	if err != nil {
		t.Fatalf("read nonexistent should return nil error: %v", err)
	}
	if len(opps) != 0 {
		t.Errorf("expected 0, got %d", len(opps))
	}
}

func TestFilterOpportunities_ByStatus(t *testing.T) {
	opps := []improve.Opportunity{
		{ID: "1", Status: improve.StatusNew},
		{ID: "2", Status: improve.StatusInterested},
		{ID: "3", Status: improve.StatusNew},
	}
	filtered := improve.FilterByStatus(opps, improve.StatusNew)
	if len(filtered) != 2 {
		t.Errorf("expected 2 new, got %d", len(filtered))
	}
}

func TestSortByRank_Descending(t *testing.T) {
	opps := []improve.Opportunity{
		{ID: "1", Rank: 30},
		{ID: "2", Rank: 47},
		{ID: "3", Rank: 20},
	}
	sorted := improve.SortByRank(opps)
	if sorted[0].ID != "2" || sorted[1].ID != "1" || sorted[2].ID != "3" {
		t.Errorf("expected sort by rank descending, got %v", sorted)
	}
}

// Suppress unused import warnings -- these are used in later tests
var _ = context.Background
var _ = json.Marshal
var _ = http.StatusOK
var _ = httptest.NewServer
var _ = strings.Contains

func TestScrapeJobicy_ParsesJobs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/api/v2/remote-jobs") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		resp := map[string]any{
			"jobs": []map[string]any{
				{
					"id":              1234,
					"url":             "https://jobicy.com/jobs/1234",
					"jobTitle":        "Backend Go Developer",
					"companyName":     "Acme Corp",
					"jobGeo":          "Anywhere",
					"jobType":         []string{"full-time"},
					"annualSalaryMin": "80000",
					"annualSalaryMax": "120000",
					"jobIndustry":     []string{"Go", "PostgreSQL"},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	scraper := improve.NewOpportunityScraper(server.URL, "", "")
	opps, err := scraper.ScrapeJobicy(context.Background(), []string{"backend"})
	if err != nil {
		t.Fatalf("scrape jobicy: %v", err)
	}
	if len(opps) != 1 {
		t.Fatalf("expected 1 opportunity, got %d", len(opps))
	}
	if opps[0].Source != "jobicy" {
		t.Errorf("expected source jobicy, got %q", opps[0].Source)
	}
	if opps[0].Title != "Backend Go Developer" {
		t.Errorf("expected title, got %q", opps[0].Title)
	}
	if opps[0].Company != "Acme Corp" {
		t.Errorf("expected company Acme Corp, got %q", opps[0].Company)
	}
}

func TestScrapeRemotive_ParsesJobs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/api/remote-jobs") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		resp := map[string]any{
			"jobs": []map[string]any{
				{
					"id":           5678,
					"url":          "https://remotive.com/jobs/5678",
					"title":        "Full Stack Developer",
					"company_name": "StartupCo",
					"tags":         []string{"JavaScript", "React", "Node.js"},
					"salary":       "$60,000 - $100,000",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	scraper := improve.NewOpportunityScraper("", server.URL, "")
	opps, err := scraper.ScrapeRemotive(context.Background())
	if err != nil {
		t.Fatalf("scrape remotive: %v", err)
	}
	if len(opps) != 1 {
		t.Fatalf("expected 1 opportunity, got %d", len(opps))
	}
	if opps[0].Source != "remotive" {
		t.Errorf("expected source remotive, got %q", opps[0].Source)
	}
}

func TestScrapeHNWhoIsHiring_FindsThreadAndParsesComments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/api/v1/search") {
			resp := map[string]any{
				"hits": []map[string]any{
					{"objectID": "99999", "title": "Ask HN: Who is hiring? (April 2026)"},
				},
			}
			json.NewEncoder(w).Encode(resp)
		} else if strings.Contains(r.URL.Path, "/api/v1/items/99999") {
			resp := map[string]any{
				"id": 99999,
				"children": []map[string]any{
					{
						"id":     100001,
						"text":   "Acme Corp | Senior Backend Engineer | Remote | $150K-$200K | Go, PostgreSQL, gRPC",
						"author": "acme_hiring",
					},
					{
						"id":     100002,
						"text":   "StartupX | Full Stack Developer | San Francisco, CA (Remote OK) | Python, React",
						"author": "startupx_hr",
					},
				},
			}
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer server.Close()

	scraper := improve.NewOpportunityScraper("", "", server.URL)
	opps, err := scraper.ScrapeHNWhoIsHiring(context.Background(), time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("scrape HN: %v", err)
	}
	if len(opps) < 1 {
		t.Fatalf("expected at least 1 opportunity from HN, got %d", len(opps))
	}
	if opps[0].Source != "hn_who_is_hiring" {
		t.Errorf("expected source hn_who_is_hiring, got %q", opps[0].Source)
	}
}

func TestScrapeJobicy_HandlesHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	scraper := improve.NewOpportunityScraper(server.URL, "", "")
	_, err := scraper.ScrapeJobicy(context.Background(), []string{"backend"})
	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestScrapeRemotive_HandlesEmptyJobs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"jobs": []any{}})
	}))
	defer server.Close()

	scraper := improve.NewOpportunityScraper("", server.URL, "")
	opps, err := scraper.ScrapeRemotive(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(opps) != 0 {
		t.Errorf("expected 0, got %d", len(opps))
	}
}
