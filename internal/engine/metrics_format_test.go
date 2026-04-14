package engine

import (
	"strings"
	"testing"
	"time"
)

func TestFormatDuration_Zero(t *testing.T) {
	got := formatDuration(0)
	if got != "—" {
		t.Errorf("expected em dash for zero, got %q", got)
	}
}

func TestFormatDuration_Seconds(t *testing.T) {
	got := formatDuration(45 * time.Second)
	if got != "45s" {
		t.Errorf("expected 45s, got %q", got)
	}
}

func TestFormatDuration_Minutes(t *testing.T) {
	got := formatDuration(3*time.Minute + 15*time.Second)
	if got != "3m 15s" {
		t.Errorf("expected 3m 15s, got %q", got)
	}
}

func TestFormatDuration_Hours(t *testing.T) {
	got := formatDuration(2*time.Hour + 30*time.Minute)
	if got != "2h 30m" {
		t.Errorf("expected 2h 30m, got %q", got)
	}
}

func TestFormatDuration_ExactMinute(t *testing.T) {
	got := formatDuration(1 * time.Minute)
	if got != "1m 0s" {
		t.Errorf("expected 1m 0s, got %q", got)
	}
}

func TestFormatDuration_ExactHour(t *testing.T) {
	got := formatDuration(1 * time.Hour)
	if got != "1h 0m" {
		t.Errorf("expected 1h 0m, got %q", got)
	}
}

func TestFormatMetrics_EscalationTiers(t *testing.T) {
	m := PipelineMetrics{
		TotalRequirements: 1,
		TotalStories:      1,
		EscalationsPerTier: map[int]int{
			1: 3,
			2: 1,
			3: 2,
		},
	}

	output := FormatMetrics(m)
	if !strings.Contains(output, "Escalations:") {
		t.Error("expected Escalations section")
	}
	if !strings.Contains(output, "Tier 1 (Senior): 3") {
		t.Errorf("expected tier 1 escalation, got:\n%s", output)
	}
	if !strings.Contains(output, "Tier 2 (Manager): 1") {
		t.Errorf("expected tier 2 escalation, got:\n%s", output)
	}
	if !strings.Contains(output, "Tier 3 (Tech Lead): 2") {
		t.Errorf("expected tier 3 escalation, got:\n%s", output)
	}
}

func TestFormatMetrics_RequirementStats(t *testing.T) {
	m := PipelineMetrics{
		TotalRequirements: 1,
		EscalationsPerTier: map[int]int{},
		RequirementStats: []RequirementStat{
			{
				ReqID:           "r-001",
				Title:           "Implement authentication",
				Status:          "completed",
				StoryCount:      3,
				MergedCount:     3,
				FirstPassRate:   66.7,
				EscalationCount: 1,
				TotalDuration:   5 * time.Minute,
			},
		},
	}

	output := FormatMetrics(m)
	if !strings.Contains(output, "Recent Requirements:") {
		t.Error("expected Recent Requirements section")
	}
	if !strings.Contains(output, "[completed]") {
		t.Error("expected status in output")
	}
	if !strings.Contains(output, "Implement authentication") {
		t.Error("expected title in output")
	}
}

func TestFormatMetrics_LongTitleTruncation(t *testing.T) {
	longTitle := "This is a very long requirement title that exceeds fifty characters and should be truncated"
	m := PipelineMetrics{
		TotalRequirements:  1,
		EscalationsPerTier: map[int]int{},
		RequirementStats: []RequirementStat{
			{
				ReqID:  "r-001",
				Title:  longTitle,
				Status: "pending",
			},
		},
	}

	output := FormatMetrics(m)
	if !strings.Contains(output, "...") {
		t.Error("expected truncated title with ellipsis")
	}
}

func TestFormatMetrics_EmptyEscalations(t *testing.T) {
	m := PipelineMetrics{
		TotalRequirements:  1,
		EscalationsPerTier: map[int]int{},
	}

	output := FormatMetrics(m)
	if strings.Contains(output, "Escalations:") {
		t.Error("should not show Escalations section when empty")
	}
}
