package engine

import (
	"strings"
	"testing"
	"time"
)

func TestFormatStatus_Done(t *testing.T) {
	got := formatStatus(ReportData{Status: ReportStatusDone})
	if got != "✅ Completed" {
		t.Errorf("expected completed, got %q", got)
	}
}

func TestFormatStatus_DoneWithConcerns(t *testing.T) {
	got := formatStatus(ReportData{Status: ReportStatusDoneWithConcerns})
	if !strings.Contains(got, "Completed") || !strings.Contains(got, "concerns") {
		t.Errorf("expected completed with concerns, got %q", got)
	}
}

func TestFormatStatus_Blocked(t *testing.T) {
	got := formatStatus(ReportData{Status: ReportStatusBlocked})
	if !strings.Contains(got, "Blocked") {
		t.Errorf("expected blocked, got %q", got)
	}
}

func TestFormatStatus_NeedsContext(t *testing.T) {
	data := ReportData{
		Status: ReportStatusNeedsContext,
		Stories: []ReportStory{
			{Status: "merged"},
			{Status: "in_progress"},
			{Status: "merged"},
		},
	}
	got := formatStatus(data)
	if !strings.Contains(got, "2 of 3") {
		t.Errorf("expected '2 of 3 merged', got %q", got)
	}
}

func TestFormatStatus_Unknown(t *testing.T) {
	got := formatStatus(ReportData{Status: "custom"})
	if got != "custom" {
		t.Errorf("expected custom, got %q", got)
	}
}

func TestEscapeHTML(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"hello", "hello"},
		{"<script>alert('xss')</script>", "&lt;script&gt;alert('xss')&lt;/script&gt;"},
		{`He said "hello" & 'bye'`, `He said &#34;hello&#34; &amp; 'bye'`},
		{"a & b < c > d", "a &amp; b &lt; c &gt; d"},
	}
	for _, tt := range tests {
		got := escapeHTML(tt.input)
		if got != tt.want {
			t.Errorf("escapeHTML(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestRenderHTML_BasicStructure(t *testing.T) {
	data := ReportData{
		RequirementID: "r-001",
		Title:         "My Feature",
		Description:   "Implement the feature",
		Status:        ReportStatusDone,
		GeneratedAt:   time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC),
		Stories: []ReportStory{
			{Title: "Task 1", Status: "merged", Complexity: 3, PRNumber: 1, PRUrl: "https://github.com/pr/1"},
		},
	}

	got := RenderHTML(data, "acme-corp", false)

	if !strings.Contains(got, "<!DOCTYPE html>") {
		t.Error("expected HTML doctype")
	}
	if !strings.Contains(got, "My Feature") {
		t.Error("expected title")
	}
	if !strings.Contains(got, "acme-corp") {
		t.Error("expected project name")
	}
	if !strings.Contains(got, "#1") {
		t.Error("expected PR number")
	}
	if !strings.Contains(got, "</html>") {
		t.Error("expected closing html tag")
	}
}

func TestRenderHTML_Internal(t *testing.T) {
	data := ReportData{
		RequirementID: "r-001",
		Title:         "Feature",
		Description:   "Desc",
		Status:        ReportStatusDone,
		GeneratedAt:   time.Now(),
		Stories: []ReportStory{
			{
				Title:           "Task",
				Status:          "merged",
				Complexity:      2,
				EscalationCount: 1,
				RetryCount:      2,
				Duration:        5 * time.Minute,
				Attempts: []Attempt{
					{Tier: 0, Role: "junior", Outcome: "failed"},
					{Tier: 1, Role: "senior", Outcome: "success"},
				},
			},
		},
	}

	got := RenderHTML(data, "project", true)

	if !strings.Contains(got, "Internal: Story Detail") {
		t.Error("expected internal story detail section")
	}
	if !strings.Contains(got, "Attempt History") {
		t.Error("expected attempt history for story with multiple attempts")
	}
}

func TestRenderHTML_EmptyTimeline(t *testing.T) {
	data := ReportData{
		RequirementID: "r-001",
		Title:         "Test",
		Description:   "Desc",
		Status:        ReportStatusDone,
		GeneratedAt:   time.Now(),
		Timeline:      nil,
	}

	got := RenderHTML(data, "proj", false)
	if !strings.Contains(got, "No significant events recorded") {
		t.Error("expected empty timeline message")
	}
}

func TestRenderHTML_XSSProtection(t *testing.T) {
	data := ReportData{
		RequirementID: "r-001",
		Title:         `<script>alert("XSS")</script>`,
		Description:   `<img onerror="evil()">`,
		Status:        ReportStatusDone,
		GeneratedAt:   time.Now(),
	}

	got := RenderHTML(data, "proj", false)
	if strings.Contains(got, "<script>") {
		t.Error("expected XSS to be escaped")
	}
	if strings.Contains(got, "<img") {
		t.Error("expected img tag to be escaped")
	}
}

func TestRenderMarkdown_BasicStructure(t *testing.T) {
	data := ReportData{
		RequirementID: "r-001",
		Title:         "My Feature",
		Description:   "Build it",
		Status:        ReportStatusDone,
		GeneratedAt:   time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC),
		Stories: []ReportStory{
			{Title: "Task 1", Status: "merged", Complexity: 3},
		},
		Timeline: []TimelineEntry{
			{Timestamp: time.Now(), EventType: "REQ_SUBMITTED", Description: "Requirement submitted"},
		},
	}

	got := RenderMarkdown(data, "acme-corp", false)

	if !strings.Contains(got, "# Delivery Report: My Feature") {
		t.Error("expected markdown header")
	}
	if !strings.Contains(got, "acme-corp") {
		t.Error("expected project name")
	}
	if !strings.Contains(got, "Deliverables") {
		t.Error("expected deliverables section")
	}
	if !strings.Contains(got, "Task 1") {
		t.Error("expected story in deliverables")
	}
}

func TestRenderMarkdown_Internal(t *testing.T) {
	data := ReportData{
		RequirementID: "r-001",
		Title:         "Feature",
		Description:   "Desc",
		Status:        ReportStatusDoneWithConcerns,
		GeneratedAt:   time.Now(),
		Stories: []ReportStory{
			{Title: "Task", Status: "merged", Complexity: 2, EscalationCount: 1},
		},
		AgentStats: []AgentStat{
			{AgentID: "junior-01", StoriesWorked: 3, Escalations: 0},
		},
	}

	got := RenderMarkdown(data, "proj", true)

	if !strings.Contains(got, "Internal: Story Detail") {
		t.Error("expected internal story detail section")
	}
	if !strings.Contains(got, "Internal: Agent Performance") {
		t.Error("expected internal agent performance section")
	}
}

func TestRenderMarkdown_NoPR(t *testing.T) {
	data := ReportData{
		RequirementID: "r-001",
		Title:         "Feature",
		Description:   "Desc",
		Status:        ReportStatusDone,
		GeneratedAt:   time.Now(),
		Stories: []ReportStory{
			{Title: "Task", Status: "merged", Complexity: 2, PRNumber: 0},
		},
	}

	got := RenderMarkdown(data, "proj", false)
	// Should show em dash for missing PR
	if !strings.Contains(got, "—") {
		t.Error("expected em dash for missing PR")
	}
}

func TestHtmlStyle_ContainsCSSRules(t *testing.T) {
	style := htmlStyle()
	if !strings.Contains(style, "<style>") {
		t.Error("expected <style> tag")
	}
	if !strings.Contains(style, "font-family") {
		t.Error("expected font-family rule")
	}
	if !strings.Contains(style, "@media print") {
		t.Error("expected print media query")
	}
}
