package engine

import (
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

func TestClassifyStatus_Done(t *testing.T) {
	rb := &ReportBuilder{}
	req := state.Requirement{Status: "completed"}
	stories := []ReportStory{
		{Status: "merged", EscalationCount: 0, RetryCount: 0},
		{Status: "merged", EscalationCount: 0, RetryCount: 0},
	}
	got := rb.classifyStatus(req, stories)
	if got != ReportStatusDone {
		t.Errorf("expected DONE, got %s", got)
	}
}

func TestClassifyStatus_DoneWithConcerns(t *testing.T) {
	rb := &ReportBuilder{}
	req := state.Requirement{Status: "completed"}
	stories := []ReportStory{
		{Status: "merged", EscalationCount: 1, RetryCount: 0},
		{Status: "merged", EscalationCount: 0, RetryCount: 0},
	}
	got := rb.classifyStatus(req, stories)
	if got != ReportStatusDoneWithConcerns {
		t.Errorf("expected DONE_WITH_CONCERNS, got %s", got)
	}
}

func TestClassifyStatus_DoneWithRetries(t *testing.T) {
	rb := &ReportBuilder{}
	req := state.Requirement{Status: "completed"}
	stories := []ReportStory{
		{Status: "merged", EscalationCount: 0, RetryCount: 2},
	}
	got := rb.classifyStatus(req, stories)
	if got != ReportStatusDoneWithConcerns {
		t.Errorf("expected DONE_WITH_CONCERNS for retries, got %s", got)
	}
}

func TestClassifyStatus_Blocked(t *testing.T) {
	rb := &ReportBuilder{}
	req := state.Requirement{Status: "planned"}
	stories := []ReportStory{
		{Status: "paused"},
	}
	got := rb.classifyStatus(req, stories)
	if got != ReportStatusBlocked {
		t.Errorf("expected BLOCKED, got %s", got)
	}
}

func TestClassifyStatus_BlockedState(t *testing.T) {
	rb := &ReportBuilder{}
	req := state.Requirement{Status: "planned"}
	stories := []ReportStory{
		{Status: "blocked"},
	}
	got := rb.classifyStatus(req, stories)
	if got != ReportStatusBlocked {
		t.Errorf("expected BLOCKED, got %s", got)
	}
}

func TestClassifyStatus_NeedsContext(t *testing.T) {
	rb := &ReportBuilder{}
	req := state.Requirement{Status: "planned"}
	stories := []ReportStory{
		{Status: "in_progress"},
	}
	got := rb.classifyStatus(req, stories)
	if got != ReportStatusNeedsContext {
		t.Errorf("expected NEEDS_CONTEXT, got %s", got)
	}
}

func TestClassifyStatus_EmptyStories(t *testing.T) {
	rb := &ReportBuilder{}
	req := state.Requirement{Status: "completed"}
	got := rb.classifyStatus(req, nil)
	if got != ReportStatusDone {
		t.Errorf("expected DONE for completed with no stories, got %s", got)
	}
}

func TestDescribeReqEvent_AllTypes(t *testing.T) {
	rb := &ReportBuilder{}
	tests := []struct {
		evtType state.EventType
		want    string
	}{
		{state.EventReqSubmitted, "Requirement submitted"},
		{state.EventReqPlanned, "Stories planned"},
		{state.EventReqCompleted, "Requirement completed"},
		{state.EventReqPaused, "Requirement paused"},
		{state.EventType("UNKNOWN"), "UNKNOWN"},
	}
	for _, tt := range tests {
		t.Run(string(tt.evtType), func(t *testing.T) {
			got := rb.describeReqEvent(tt.evtType)
			if got != tt.want {
				t.Errorf("describeReqEvent(%s) = %q, want %q", tt.evtType, got, tt.want)
			}
		})
	}
}

func TestDescribeStoryEvent_AllTypes(t *testing.T) {
	rb := &ReportBuilder{}
	tests := []struct {
		evtType state.EventType
		want    string
	}{
		{state.EventStoryMerged, "Story merged: My Task"},
		{state.EventStoryEscalated, "Story escalated: My Task"},
		{state.EventStoryPRCreated, "PR created: My Task"},
		{state.EventStoryReviewFailed, "Review failed: My Task"},
		{state.EventStoryQAFailed, "QA failed: My Task"},
		{state.EventType("OTHER"), "OTHER: My Task"},
	}
	for _, tt := range tests {
		t.Run(string(tt.evtType), func(t *testing.T) {
			got := rb.describeStoryEvent(tt.evtType, "My Task")
			if got != tt.want {
				t.Errorf("describeStoryEvent(%s) = %q, want %q", tt.evtType, got, tt.want)
			}
		})
	}
}

func TestStoryDuration_Normal(t *testing.T) {
	rb := &ReportBuilder{}
	now := time.Now()
	s := state.Story{
		CreatedAt: now.Add(-1 * time.Hour),
		MergedAt:  now,
	}
	got := rb.storyDuration(s)
	if got != 1*time.Hour {
		t.Errorf("expected 1 hour, got %v", got)
	}
}

func TestStoryDuration_ZeroMergedAt(t *testing.T) {
	rb := &ReportBuilder{}
	s := state.Story{
		CreatedAt: time.Now(),
	}
	got := rb.storyDuration(s)
	if got != 0 {
		t.Errorf("expected 0 for zero MergedAt, got %v", got)
	}
}

func TestStoryDuration_ZeroCreatedAt(t *testing.T) {
	rb := &ReportBuilder{}
	s := state.Story{
		MergedAt: time.Now(),
	}
	got := rb.storyDuration(s)
	if got != 0 {
		t.Errorf("expected 0 for zero CreatedAt, got %v", got)
	}
}

func TestStoryDuration_NegativeDuration(t *testing.T) {
	rb := &ReportBuilder{}
	now := time.Now()
	s := state.Story{
		CreatedAt: now,
		MergedAt:  now.Add(-1 * time.Hour),
	}
	got := rb.storyDuration(s)
	if got != 0 {
		t.Errorf("expected 0 for negative duration, got %v", got)
	}
}

func TestSortTimelineEntries(t *testing.T) {
	now := time.Now()
	entries := []TimelineEntry{
		{Timestamp: now.Add(2 * time.Hour), Description: "third"},
		{Timestamp: now, Description: "first"},
		{Timestamp: now.Add(1 * time.Hour), Description: "second"},
	}
	sortTimelineEntries(entries)

	if entries[0].Description != "first" {
		t.Errorf("expected first, got %s", entries[0].Description)
	}
	if entries[1].Description != "second" {
		t.Errorf("expected second, got %s", entries[1].Description)
	}
	if entries[2].Description != "third" {
		t.Errorf("expected third, got %s", entries[2].Description)
	}
}

func TestSortTimelineEntries_Empty(t *testing.T) {
	var entries []TimelineEntry
	sortTimelineEntries(entries) // should not panic
}

func TestSortTimelineEntries_Single(t *testing.T) {
	entries := []TimelineEntry{{Description: "only"}}
	sortTimelineEntries(entries) // should not panic
	if entries[0].Description != "only" {
		t.Error("single entry should remain unchanged")
	}
}
