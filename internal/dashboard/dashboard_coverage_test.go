package dashboard

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// TestTruncateStr_ShortString verifies no truncation when string fits.
func TestTruncateStr_ShortString(t *testing.T) {
	result := truncateStr("hello", 10)
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}
}

// TestTruncateStr_ExactLength verifies no truncation at exact boundary.
func TestTruncateStr_ExactLength(t *testing.T) {
	result := truncateStr("hello", 5)
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}
}

// TestTruncateStr_Truncated verifies ellipsis is added when truncated.
func TestTruncateStr_Truncated(t *testing.T) {
	result := truncateStr("hello world", 8)
	if result != "hello..." {
		t.Errorf("expected 'hello...', got %q", result)
	}
}

// TestTruncateStr_VeryShortMax verifies behavior when maxLen <= 3.
func TestTruncateStr_VeryShortMax(t *testing.T) {
	result := truncateStr("hello", 3)
	if result != "hel" {
		t.Errorf("expected 'hel', got %q", result)
	}
}

// TestTruncateStr_MaxLenOne verifies behavior at minimum maxLen.
func TestTruncateStr_MaxLenOne(t *testing.T) {
	result := truncateStr("hello", 1)
	if result != "h" {
		t.Errorf("expected 'h', got %q", result)
	}
}

// TestTruncateStr_EmptyString verifies empty string passthrough.
func TestTruncateStr_EmptyString(t *testing.T) {
	result := truncateStr("", 5)
	if result != "" {
		t.Errorf("expected '', got %q", result)
	}
}

// TestTruncateStr_MaxLenZero verifies maxLen 0 returns empty.
func TestTruncateStr_MaxLenZero(t *testing.T) {
	result := truncateStr("hello", 0)
	// len("hello") > 0, maxLen <= 3, so s[:0] = ""
	if result != "" {
		t.Errorf("expected '', got %q", result)
	}
}

// TestTruncateStr_NegativeMaxLenNoPanic pins the narrow-terminal crash fix:
// renderStories calls truncateStr(s.Title, width-65) with the raw terminal
// width, so a terminal narrower than 65 columns yields a negative maxLen. Before
// the guard, s[:maxLen] panicked ("slice bounds out of range") and crashed the
// TUI View(). A negative maxLen must return "" without panicking.
func TestTruncateStr_NegativeMaxLenNoPanic(t *testing.T) {
	for _, tc := range []struct {
		s      string
		maxLen int
	}{
		{"Add login", -5}, // width 60: 60-65
		{"", -5},          // empty title on a narrow terminal
		{"a very long story title that overflows", -40},
	} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("truncateStr(%q, %d) panicked: %v", tc.s, tc.maxLen, r)
				}
			}()
			if got := truncateStr(tc.s, tc.maxLen); got != "" {
				t.Errorf("truncateStr(%q, %d) = %q, want \"\"", tc.s, tc.maxLen, got)
			}
		}()
	}
}

// TestHandleKey_Quit verifies 'q' produces quit command.
func TestHandleKey_Quit(t *testing.T) {
	m := Model{version: "test"}
	_, cmd := m.handleKey(newKeyMsg('q'))
	if cmd == nil {
		t.Error("'q' key should return a quit command")
	}
}

// TestHandleKey_CtrlC verifies Ctrl+C produces quit command.
func TestHandleKey_CtrlC(t *testing.T) {
	m := Model{version: "test"}
	msg := tea.KeyMsg{Type: tea.KeyCtrlC}
	_, cmd := m.handleKey(msg)
	if cmd == nil {
		t.Error("Ctrl+C should return a quit command")
	}
}

// TestHandleKey_W_NoOp verifies 'w' key is a no-op (no command returned).
func TestHandleKey_W_NoOp(t *testing.T) {
	m := Model{version: "test"}
	_, cmd := m.handleKey(newKeyMsg('w'))
	if cmd != nil {
		t.Error("'w' key should be a no-op (nil command)")
	}
}

// TestHandleKey_UnknownRune verifies unknown runes are no-ops.
func TestHandleKey_UnknownRune(t *testing.T) {
	m := Model{version: "test", storyScrollOffset: 5}
	updated, cmd := m.handleKey(newKeyMsg('x'))
	if cmd != nil {
		t.Error("unknown rune should return nil command")
	}
	m2 := updated.(Model)
	if m2.storyScrollOffset != 5 {
		t.Error("offset should be unchanged for unknown rune")
	}
}

// TestHandleKey_EmptyRunes verifies empty runes message is a no-op.
func TestHandleKey_EmptyRunes(t *testing.T) {
	m := Model{version: "test"}
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{}}
	_, cmd := m.handleKey(msg)
	if cmd != nil {
		t.Error("empty runes should return nil command")
	}
}

// TestHandleKey_ScrollMultiple verifies multiple scroll operations.
func TestHandleKey_ScrollMultiple(t *testing.T) {
	m := Model{storyScrollOffset: 0}

	// Scroll down 3 times
	for i := 0; i < 3; i++ {
		result, _ := m.handleKey(newKeyMsg('j'))
		m = result.(Model)
	}
	if m.storyScrollOffset != 3 {
		t.Errorf("expected offset 3 after 3 'j' presses, got %d", m.storyScrollOffset)
	}

	// Scroll up 2 times
	for i := 0; i < 2; i++ {
		result, _ := m.handleKey(newKeyMsg('k'))
		m = result.(Model)
	}
	if m.storyScrollOffset != 1 {
		t.Errorf("expected offset 1 after 2 'k' presses, got %d", m.storyScrollOffset)
	}
}

// TestRenderAgentSummary_Empty verifies empty agent list summary.
func TestRenderAgentSummary_Empty(t *testing.T) {
	result := renderAgentSummary(nil)
	if !strings.Contains(result, "Total: 0") {
		t.Error("empty agent list should show Total: 0")
	}
}

// TestRenderAgentSummary_AllActive verifies all-active summary.
func TestRenderAgentSummary_AllActive(t *testing.T) {
	agents := []state.Agent{
		{ID: "a1", Status: "active"},
		{ID: "a2", Status: "active"},
	}
	result := renderAgentSummary(agents)
	if !strings.Contains(result, "Total: 2") {
		t.Error("should show Total: 2")
	}
	if !strings.Contains(result, "Active: 2") {
		t.Error("should show Active: 2")
	}
}

// TestRenderAgentSummary_MixedStatuses verifies mixed status summary.
func TestRenderAgentSummary_MixedStatuses(t *testing.T) {
	agents := []state.Agent{
		{ID: "a1", Status: "active"},
		{ID: "a2", Status: "idle"},
		{ID: "a3", Status: "stuck"},
		{ID: "a4", Status: "terminated"},
		{ID: "a5", Status: "active"},
	}
	result := renderAgentSummary(agents)
	if !strings.Contains(result, "Total: 5") {
		t.Error("should show Total: 5")
	}
	if !strings.Contains(result, "Active: 2") {
		t.Error("should show Active: 2")
	}
	if !strings.Contains(result, "Idle: 1") {
		t.Error("should show Idle: 1")
	}
	if !strings.Contains(result, "Stuck: 1") {
		t.Error("should show Stuck: 1")
	}
	if !strings.Contains(result, "Terminated: 1") {
		t.Error("should show Terminated: 1")
	}
}

// TestRenderAgentSummary_OnlyIdle verifies only idle agents.
func TestRenderAgentSummary_OnlyIdle(t *testing.T) {
	agents := []state.Agent{
		{ID: "a1", Status: "idle"},
	}
	result := renderAgentSummary(agents)
	if !strings.Contains(result, "Total: 1") {
		t.Error("should show Total: 1")
	}
	if !strings.Contains(result, "Idle: 1") {
		t.Error("should show Idle: 1")
	}
	// Should NOT contain Active or Stuck sections
	if strings.Contains(result, "Active:") {
		t.Error("should not contain Active when no active agents")
	}
}

// TestRenderAgentSummary_OnlyStuck verifies only stuck agents.
func TestRenderAgentSummary_OnlyStuck(t *testing.T) {
	agents := []state.Agent{
		{ID: "a1", Status: "stuck"},
		{ID: "a2", Status: "stuck"},
	}
	result := renderAgentSummary(agents)
	if !strings.Contains(result, "Stuck: 2") {
		t.Error("should show Stuck: 2")
	}
}

// TestRenderAgentTable_Empty verifies empty agents renders "No agents found."
func TestRenderAgentTable_Empty(t *testing.T) {
	result := renderAgentTable(nil, 120, 10)
	if !strings.Contains(result, "No agents found") {
		t.Error("empty agents should show 'No agents found'")
	}
}

// TestRenderAgentTable_WithAgents verifies agents are rendered into table.
func TestRenderAgentTable_WithAgents(t *testing.T) {
	agents := []state.Agent{
		{ID: "agent-1", Type: "senior", Model: "opus-4", Status: "active", CurrentStoryID: "s-001", SessionName: "vxd-s-001"},
	}
	result := renderAgentTable(agents, 120, 10)
	if !strings.Contains(result, "agent-1") {
		t.Error("should contain agent ID")
	}
	if !strings.Contains(result, "senior") {
		t.Error("should contain agent role")
	}
	if !strings.Contains(result, "opus-4") {
		t.Error("should contain model")
	}
	if !strings.Contains(result, "s-001") {
		t.Error("should contain story ID")
	}
}

// TestRenderAgentTable_EmptyStoryAndSession verifies dash placeholders.
func TestRenderAgentTable_EmptyStoryAndSession(t *testing.T) {
	agents := []state.Agent{
		{ID: "idle-1", Type: "junior", Model: "mini", Status: "idle"},
	}
	result := renderAgentTable(agents, 120, 10)
	// Empty story and session should show "-"
	if !strings.Contains(result, "-") {
		t.Error("should show dash for empty story/session")
	}
}

// TestRenderAgentTable_MaxRows verifies row limit with "... and N more".
func TestRenderAgentTable_MaxRows(t *testing.T) {
	agents := make([]state.Agent, 5)
	for i := range agents {
		agents[i] = state.Agent{ID: "a", Type: "jr", Model: "m", Status: "active"}
	}
	result := renderAgentTable(agents, 120, 2)
	if !strings.Contains(result, "and 3 more") {
		t.Error("should show overflow count when maxRows exceeded")
	}
}

// TestRenderStories_Empty verifies "No stories" message.
func TestRenderStories_Empty(t *testing.T) {
	m := Model{width: 120, height: 40}
	result := m.renderStories(120, 10)
	if !strings.Contains(result, "No stories") {
		t.Error("should show 'No stories' when empty")
	}
}

// TestRenderStories_WithStories verifies stories are rendered.
func TestRenderStories_WithStories(t *testing.T) {
	m := Model{
		width:  120,
		height: 40,
		stories: []state.Story{
			{ID: "s-001", Title: "Add login", Status: "in_progress", Complexity: 3},
			{ID: "s-002", Title: "Add signup", Status: "merged", Complexity: 5, EscalationTier: 1},
		},
	}
	result := m.renderStories(120, 15)
	if !strings.Contains(result, "s-001") {
		t.Error("should contain first story ID")
	}
	if !strings.Contains(result, "s-002") {
		t.Error("should contain second story ID")
	}
	if !strings.Contains(result, "Add login") {
		t.Error("should contain story title")
	}
}

// TestRenderStories_Scroll verifies scrolling shows different stories.
func TestRenderStories_Scroll(t *testing.T) {
	stories := make([]state.Story, 20)
	for i := range stories {
		stories[i] = state.Story{
			ID:     "s-" + strings.Repeat("x", i),
			Title:  "Story",
			Status: "planned",
		}
	}
	m := Model{width: 120, height: 40, stories: stories, storyScrollOffset: 10}
	result := m.renderStories(120, 8)
	// Should show scroll info
	if !strings.Contains(result, "of 20") {
		t.Error("should contain total count in scroll info")
	}
}

// TestRenderPipeline verifies pipeline summary rendering.
func TestRenderPipeline(t *testing.T) {
	m := Model{
		width:  120,
		height: 40,
		stories: []state.Story{
			{Status: "planned"},
			{Status: "in_progress"},
			{Status: "merged"},
		},
	}
	result := m.renderPipeline(120)
	if !strings.Contains(result, "Pipeline") {
		t.Error("should contain Pipeline header")
	}
	if !strings.Contains(result, "complete") {
		t.Error("should contain completion percentage")
	}
}

// TestRenderActivity_Empty verifies empty activity feed.
func TestRenderActivity_Empty(t *testing.T) {
	m := Model{width: 120, height: 40}
	result := m.renderActivity(120, 10)
	if !strings.Contains(result, "No events") {
		t.Error("should show 'No events' when empty")
	}
}

// TestRenderActivity_WithEvents verifies events are rendered.
func TestRenderActivity_WithEvents(t *testing.T) {
	m := Model{
		width:  120,
		height: 40,
		events: []state.Event{
			{Type: "STORY_CREATED", AgentID: "a-1", StoryID: "s-1", Timestamp: time.Now()},
			{Type: "STORY_COMPLETED", AgentID: "a-1", StoryID: "s-1", Timestamp: time.Now()},
		},
	}
	result := m.renderActivity(120, 10)
	if !strings.Contains(result, "Activity") {
		t.Error("should contain Activity header")
	}
}

// TestRenderEscalations_Empty verifies empty escalations.
func TestRenderEscalations_Empty(t *testing.T) {
	m := Model{width: 120, height: 40}
	result := m.renderEscalations(120, 5)
	if !strings.Contains(result, "No escalations") {
		t.Error("should show 'No escalations' when empty")
	}
}

// TestRenderEscalations_WithPending verifies pending escalations rendering.
func TestRenderEscalations_WithPending(t *testing.T) {
	m := Model{
		width:  120,
		height: 40,
		escalations: []state.Escalation{
			{StoryID: "s-1", FromAgent: "a-1", Status: "pending", FromTier: 0, ToTier: 1, Reason: "stuck on build"},
		},
	}
	result := m.renderEscalations(120, 5)
	if !strings.Contains(result, "pending") {
		t.Error("should show pending count")
	}
}

// TestReverseEvents verifies event reversal.
func TestReverseEvents(t *testing.T) {
	events := []state.Event{
		{Type: "A"},
		{Type: "B"},
		{Type: "C"},
	}
	reversed := reverseEvents(events)
	if len(reversed) != 3 {
		t.Fatalf("expected 3 events, got %d", len(reversed))
	}
	if reversed[0].Type != "C" {
		t.Errorf("first reversed event should be C, got %s", reversed[0].Type)
	}
	if reversed[2].Type != "A" {
		t.Errorf("last reversed event should be A, got %s", reversed[2].Type)
	}
}

// TestReverseEvents_Empty verifies empty slice reversal.
func TestReverseEvents_Empty(t *testing.T) {
	reversed := reverseEvents(nil)
	if len(reversed) != 0 {
		t.Errorf("expected 0 events, got %d", len(reversed))
	}
}

// TestReverseEvents_Single verifies single element reversal.
func TestReverseEvents_Single(t *testing.T) {
	events := []state.Event{{Type: "ONLY"}}
	reversed := reverseEvents(events)
	if reversed[0].Type != "ONLY" {
		t.Errorf("single element should remain the same")
	}
}

// TestCountByStatus verifies story status bucketing.
func TestCountByStatus(t *testing.T) {
	m := Model{
		stories: []state.Story{
			{Status: "planned"},
			{Status: "planned"},
			{Status: "in_progress"},
			{Status: "review"},
			{Status: "qa"},
			{Status: "qa_failed"},
			{Status: "merged"},
			{Status: "merged"},
			{Status: "merged"},
			{Status: "pr_submitted"},
		},
	}
	buckets := m.countByStatus()
	if buckets["planned"] != 2 {
		t.Errorf("expected 2 planned, got %d", buckets["planned"])
	}
	if buckets["in_progress"] != 1 {
		t.Errorf("expected 1 in_progress, got %d", buckets["in_progress"])
	}
	if buckets["review"] != 1 {
		t.Errorf("expected 1 review, got %d", buckets["review"])
	}
	if buckets["qa"] != 2 {
		t.Errorf("expected 2 qa (qa + qa_failed), got %d", buckets["qa"])
	}
	if buckets["merged"] != 3 {
		t.Errorf("expected 3 merged, got %d", buckets["merged"])
	}
	if buckets["pr_submitted"] != 1 {
		t.Errorf("expected 1 pr_submitted, got %d", buckets["pr_submitted"])
	}
}

// TestMapStatusToBucket verifies all status-to-bucket mappings.
func TestMapStatusToBucket(t *testing.T) {
	tests := []struct {
		status   string
		expected string
	}{
		{"draft", "planned"},
		{"estimated", "planned"},
		{"planned", "planned"},
		{"assigned", "planned"},
		{"in_progress", "in_progress"},
		{"review", "review"},
		{"qa", "qa"},
		{"qa_started", "qa"},
		{"qa_failed", "qa"},
		{"pr_submitted", "pr_submitted"},
		{"merged", "merged"},
		{"split", "split"},
		{"unknown_status", "planned"}, // default
	}
	for _, tt := range tests {
		result := mapStatusToBucket(tt.status)
		if result != tt.expected {
			t.Errorf("status %q: expected bucket %q, got %q", tt.status, tt.expected, result)
		}
	}
}

// TestStoryStatusStyle verifies all status styles return non-zero values.
func TestStoryStatusStyle(t *testing.T) {
	statuses := []string{
		"draft", "planned", "estimated", "assigned",
		"in_progress", "review", "qa", "qa_failed",
		"pr_submitted", "merged", "paused", "unknown",
	}
	for _, s := range statuses {
		style := storyStatusStyle(s)
		// Just verify it doesn't panic and returns a style
		_ = style.Render("test")
	}
}

// TestAgentStatusStyle verifies agent status styles.
func TestAgentStatusStyle(t *testing.T) {
	statuses := []string{"active", "stuck", "idle", "terminated", "unknown"}
	for _, s := range statuses {
		style := agentStatusStyle(s)
		_ = style.Render("test")
	}
}

// TestEventCategoryStyle verifies event category style resolution.
func TestEventCategoryStyle(t *testing.T) {
	tests := []string{
		"REQ_CREATED",
		"STORY_COMPLETED",
		"AGENT_STARTED",
		"ESCALATION_CREATED",
		"SUPERVISOR_REVIEW",
		"UNKNOWN_EVENT",
	}
	for _, eventType := range tests {
		style := eventCategoryStyle(eventType)
		_ = style.Render("test")
	}
}

// TestRenderPausedBanner_NoPaused verifies no banner when nothing paused.
func TestRenderPausedBanner_NoPaused(t *testing.T) {
	m := Model{
		requirements: []state.Requirement{
			{ID: "r-1", Status: "active"},
		},
	}
	result := m.renderPausedBanner(120)
	if result != "" {
		t.Error("should return empty string when no paused requirements")
	}
}

// TestRenderPausedBanner_WithPaused verifies banner shows paused requirements.
func TestRenderPausedBanner_WithPaused(t *testing.T) {
	m := Model{
		requirements: []state.Requirement{
			{ID: "r-1234567890", Title: "Test Req", Status: "paused"},
		},
	}
	result := m.renderPausedBanner(120)
	if !strings.Contains(result, "PAUSED") {
		t.Error("should contain PAUSED label")
	}
	if !strings.Contains(result, "r-123456") {
		t.Error("should contain truncated req ID")
	}
	if !strings.Contains(result, "Test Req") {
		t.Error("should contain req title")
	}
}

// TestRenderStatusBar_WithError verifies error is shown in status bar.
func TestRenderStatusBar_WithError(t *testing.T) {
	m := Model{
		version: "1.0",
		width:   100,
		err:     errMock,
	}
	result := m.renderStatusBar()
	if !strings.Contains(result, "ERR:") {
		t.Error("should show error in status bar")
	}
}

// TestRenderStatusBar_WithRefresh verifies refresh time shown.
func TestRenderStatusBar_WithRefresh(t *testing.T) {
	m := Model{
		version:     "1.0",
		width:       100,
		lastRefresh: time.Now(),
	}
	result := m.renderStatusBar()
	if !strings.Contains(result, "Last refresh:") {
		t.Error("should show last refresh time")
	}
}

// TestUpdate_UnknownMsg verifies unknown message types are no-ops.
func TestUpdate_UnknownMsg(t *testing.T) {
	m := Model{version: "test", width: 80, height: 24}
	type unknownMsg struct{}
	updated, cmd := m.Update(unknownMsg{})
	if cmd != nil {
		t.Error("unknown message should return nil command")
	}
	m2 := updated.(Model)
	if m2.version != "test" {
		t.Error("model should be unchanged for unknown message")
	}
}

// TestRenderEscalations_Resolved verifies resolved escalations render correctly.
func TestRenderEscalations_Resolved(t *testing.T) {
	m := Model{
		width:  120,
		height: 40,
		escalations: []state.Escalation{
			{StoryID: "s-1", FromAgent: "a-1", Status: "resolved", FromTier: 0, ToTier: 1, Reason: "was stuck"},
		},
	}
	result := m.renderEscalations(120, 5)
	if !strings.Contains(result, "Escalations") {
		t.Error("should contain Escalations header")
	}
	if !strings.Contains(result, "resolved") {
		t.Error("should show resolved status")
	}
}

// TestRenderEscalations_Mixed verifies mixed pending/resolved escalations.
func TestRenderEscalations_Mixed(t *testing.T) {
	m := Model{
		width:  120,
		height: 40,
		escalations: []state.Escalation{
			{StoryID: "s-1", FromAgent: "a-1", Status: "pending", FromTier: 0, ToTier: 1, Reason: "build fail"},
			{StoryID: "s-2", FromAgent: "a-2", Status: "resolved", FromTier: 1, ToTier: 2, Reason: "test fail"},
			{StoryID: "s-3", FromAgent: "a-3", Status: "pending", FromTier: 2, ToTier: 3, Reason: "review rejected"},
		},
	}
	result := m.renderEscalations(120, 5)
	if !strings.Contains(result, "2 pending") {
		t.Error("should show 2 pending count")
	}
}

// TestRenderEscalations_RowLimit verifies escalation row limit.
func TestRenderEscalations_RowLimit(t *testing.T) {
	escalations := make([]state.Escalation, 10)
	for i := range escalations {
		escalations[i] = state.Escalation{
			StoryID:   "s-x",
			FromAgent: "a-x",
			Status:    "pending",
			FromTier:  0,
			ToTier:    1,
			Reason:    "test",
		}
	}
	m := Model{width: 120, height: 40, escalations: escalations}
	// maxRows = 3 means only 2 data rows (3 - 1 for header)
	result := m.renderEscalations(120, 3)
	_ = result // should not panic
}

// TestRenderEscalations_EmptyStoryID verifies dash for empty story ID.
func TestRenderEscalations_EmptyStoryID(t *testing.T) {
	m := Model{
		width:  120,
		height: 40,
		escalations: []state.Escalation{
			{StoryID: "", FromAgent: "a-1", Status: "pending", FromTier: 0, ToTier: 1, Reason: "stuck"},
		},
	}
	result := m.renderEscalations(120, 5)
	if !strings.Contains(result, "-") {
		t.Error("empty story ID should show dash")
	}
}

// TestRenderEscalations_NarrowWidth verifies narrow width doesn't panic.
func TestRenderEscalations_NarrowWidth(t *testing.T) {
	m := Model{
		width:  60,
		height: 40,
		escalations: []state.Escalation{
			{StoryID: "s-1", FromAgent: "a-1", Status: "pending", FromTier: 0, ToTier: 1, Reason: "a very long reason that exceeds the width limit"},
		},
	}
	// Should not panic with narrow width
	result := m.renderEscalations(60, 5)
	_ = result
}

// TestRenderActivity_ManyEvents verifies overflow message for many events.
func TestRenderActivity_ManyEvents(t *testing.T) {
	events := make([]state.Event, 15)
	for i := range events {
		events[i] = state.Event{Type: "STORY_CREATED", AgentID: "a-1", StoryID: "s-1", Timestamp: time.Now()}
	}
	m := Model{width: 120, height: 40, events: events}
	result := m.renderActivity(120, 8)
	if !strings.Contains(result, "more events") {
		t.Error("should show overflow count for many events")
	}
}

// TestRenderActivity_EmptyAgentID verifies dash for empty agent ID.
func TestRenderActivity_EmptyAgentID(t *testing.T) {
	m := Model{
		width:  120,
		height: 40,
		events: []state.Event{
			{Type: "REQ_CREATED", AgentID: "", StoryID: "", Timestamp: time.Now()},
		},
	}
	result := m.renderActivity(120, 10)
	if !strings.Contains(result, "-") {
		t.Error("empty agent/story IDs should show dashes")
	}
}

// TestRenderPipeline_WithPaused verifies paused banner in pipeline.
func TestRenderPipeline_WithPaused(t *testing.T) {
	m := Model{
		width:  120,
		height: 40,
		stories: []state.Story{
			{Status: "planned"},
		},
		requirements: []state.Requirement{
			{ID: "r-1", Title: "Paused req", Status: "paused"},
		},
	}
	result := m.renderPipeline(120)
	if !strings.Contains(result, "PAUSED") {
		t.Error("should show paused banner when requirements are paused")
	}
}

// TestRenderPipeline_NarrowWidth verifies narrow pipeline doesn't panic.
func TestRenderPipeline_NarrowWidth(t *testing.T) {
	m := Model{width: 30, height: 20, stories: []state.Story{{Status: "merged"}}}
	result := m.renderPipeline(30)
	if !strings.Contains(result, "Pipeline") {
		t.Error("should contain Pipeline header even at narrow width")
	}
}

// TestView_WithAllData verifies full View with comprehensive data.
func TestView_WithAllData(t *testing.T) {
	m := Model{
		version: "2.0.0",
		width:   120,
		height:  40,
		agents: []state.Agent{
			{ID: "a-1", Type: "senior", Model: "opus", Status: "active", CurrentStoryID: "s-1", SessionName: "vxd-s-1"},
			{ID: "a-2", Type: "junior", Model: "mini", Status: "idle"},
		},
		stories: []state.Story{
			{ID: "s-1", Title: "Login", Status: "in_progress", Complexity: 5},
			{ID: "s-2", Title: "Signup", Status: "merged", Complexity: 3},
		},
		events: []state.Event{
			{Type: "STORY_CREATED", Timestamp: time.Now()},
		},
		escalations: []state.Escalation{
			{StoryID: "s-1", Status: "pending", FromTier: 0, ToTier: 1},
		},
		lastRefresh: time.Now(),
	}
	output := m.View()
	if !strings.Contains(output, "VXD DASHBOARD") {
		t.Error("should contain header")
	}
	if !strings.Contains(output, "2.0.0") {
		t.Error("should contain version")
	}
}

// TestView_WithError verifies View shows error state.
func TestView_WithError(t *testing.T) {
	m := Model{
		version:     "1.0",
		width:       120,
		height:      40,
		lastRefresh: time.Now(),
		err:         errMock,
	}
	output := m.View()
	if !strings.Contains(output, "ERR:") {
		t.Error("should show error in view")
	}
}

// TestRenderStories_WithEscalationTier verifies escalation tier display.
func TestRenderStories_WithEscalationTier(t *testing.T) {
	m := Model{
		width:  120,
		height: 40,
		stories: []state.Story{
			{ID: "s-1", Title: "Stuck story", Status: "in_progress", Complexity: 3, EscalationTier: 2},
		},
	}
	result := m.renderStories(120, 10)
	if !strings.Contains(result, "T2") {
		t.Error("should show escalation tier T2")
	}
}
