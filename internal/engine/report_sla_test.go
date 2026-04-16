package engine

import (
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

func TestBuildStories_PopulatesSLABreached(t *testing.T) {
	es, ps, cleanup := newReportStores(t)
	defer cleanup()

	storyID := "s-001"
	created := state.NewEvent(state.EventStoryCreated, "system", storyID, map[string]any{
		"id": storyID, "req_id": "req-001", "title": "Slow story",
		"description": "x", "complexity": 3,
	})
	es.Append(created)
	ps.Project(created)

	breach := state.NewEvent(state.EventStorySLABreached, "agent-1", storyID, map[string]any{
		"complexity":      3,
		"elapsed_seconds": 18000, // 5h
		"max_minutes":     240,
	})
	es.Append(breach)

	cfg := config.DefaultConfig()
	rb := NewReportBuilder(es, ps, cfg)
	stories, err := rb.buildStories([]state.Story{{ID: storyID, Title: "Slow story", Complexity: 3}})
	if err != nil {
		t.Fatal(err)
	}
	if len(stories) != 1 {
		t.Fatalf("got %d stories, want 1", len(stories))
	}

	if !stories[0].SLABreached {
		t.Error("SLABreached should be true")
	}
	want := 5 * time.Hour
	if stories[0].SLAElapsed != want {
		t.Errorf("SLAElapsed = %v, want %v", stories[0].SLAElapsed, want)
	}
}

func TestBuildStories_NoSLABreach(t *testing.T) {
	es, ps, cleanup := newReportStores(t)
	defer cleanup()

	storyID := "s-002"
	created := state.NewEvent(state.EventStoryCreated, "system", storyID, map[string]any{
		"id": storyID, "req_id": "req-001", "title": "Fast story",
		"description": "x", "complexity": 1,
	})
	es.Append(created)
	ps.Project(created)

	cfg := config.DefaultConfig()
	rb := NewReportBuilder(es, ps, cfg)
	stories, err := rb.buildStories([]state.Story{{ID: storyID, Title: "Fast story", Complexity: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(stories) != 1 {
		t.Fatalf("got %d stories, want 1", len(stories))
	}
	if stories[0].SLABreached {
		t.Error("SLABreached should be false for non-breached story")
	}
	if stories[0].SLAElapsed != 0 {
		t.Errorf("SLAElapsed should be 0 for non-breached story, got %v", stories[0].SLAElapsed)
	}
}

func TestRenderMarkdown_SLABreachBadge(t *testing.T) {
	data := ReportData{
		RequirementID: "r-001",
		Title:         "Test Req",
		Description:   "desc",
		Status:        ReportStatusDoneWithConcerns,
		Stories: []ReportStory{
			{
				ID:          "s-001",
				Title:       "Slow story",
				Status:      "merged",
				Complexity:  3,
				SLABreached: true,
				SLAElapsed:  5 * time.Hour,
			},
		},
		Effort: Estimate{Summary: EstimateSummary{Currency: "USD"}},
	}

	md := RenderMarkdown(data, "test-project", true)

	if !containsStr(md, "BREACH") {
		t.Error("markdown should contain BREACH for SLA-breached story")
	}
	if !containsStr(md, "5h") {
		t.Error("markdown should contain elapsed time for SLA-breached story")
	}
}

func TestRenderHTML_SLABreachBadge(t *testing.T) {
	data := ReportData{
		RequirementID: "r-001",
		Title:         "Test Req",
		Description:   "desc",
		Status:        ReportStatusDoneWithConcerns,
		Stories: []ReportStory{
			{
				ID:          "s-001",
				Title:       "Slow story",
				Status:      "merged",
				Complexity:  3,
				SLABreached: true,
				SLAElapsed:  5 * time.Hour,
			},
		},
		Effort: Estimate{Summary: EstimateSummary{Currency: "USD"}},
	}

	html := RenderHTML(data, "test-project", true)

	if !containsStr(html, "sla-breach") {
		t.Error("HTML should contain sla-breach CSS class for breached story")
	}
	if !containsStr(html, "BREACH") {
		t.Error("HTML should contain BREACH text for SLA-breached story")
	}
	if !containsStr(html, "5h") {
		t.Error("HTML should contain elapsed time for SLA-breached story")
	}
}

func TestRenderHTML_NoSLABreach_ShowsOK(t *testing.T) {
	data := ReportData{
		RequirementID: "r-001",
		Title:         "Test Req",
		Description:   "desc",
		Status:        ReportStatusDone,
		Stories: []ReportStory{
			{
				ID:         "s-001",
				Title:      "Fast story",
				Status:     "merged",
				Complexity: 1,
			},
		},
		Effort: Estimate{Summary: EstimateSummary{Currency: "USD"}},
	}

	html := RenderHTML(data, "test-project", true)

	if !containsStr(html, ">OK<") {
		t.Error("HTML should show OK for non-breached story SLA column")
	}
}

// containsStr is a helper to check substring containment.
func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
