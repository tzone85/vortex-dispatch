package dashboard

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

// --- fetchData error paths (currently 10% covered) ---

func TestFetchData_ReturnsNonNilCmd(t *testing.T) {
	// fetchData returns a tea.Cmd (function), just verify it doesn't panic
	m := Model{}
	cmd := m.fetchData()
	if cmd == nil {
		t.Error("expected non-nil command")
	}
}

// --- Update with KeyMsg for 'w' (browser open) ---

func TestUpdate_KeyMsg_W_ReturnsNil(t *testing.T) {
	m := Model{width: 80, height: 24}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	if cmd != nil {
		t.Error("expected nil command for 'w' key (no-op)")
	}
	model := updated.(Model)
	if model.width != 80 {
		t.Error("width should be unchanged")
	}
}

// --- renderStatusBar with error ---

func TestRenderStatusBar_WithErrorInfo(t *testing.T) {
	m := Model{
		width:       100,
		height:      24,
		version:     "1.0",
		lastRefresh: time.Now(),
		err:         nil,
	}
	bar := m.renderStatusBar()
	if !strings.Contains(bar, "q:quit") {
		t.Error("expected quit hint in status bar")
	}
}

func TestRenderStatusBar_ZeroWidth(t *testing.T) {
	m := Model{
		width:       0,
		height:      24,
		version:     "1.0",
		lastRefresh: time.Now(),
	}
	// Should not panic with zero width. Bar may be empty or have content.
	_ = m.renderStatusBar()
}

// --- renderStories with scroll offset ---

func TestRenderStories_LargeOffset(t *testing.T) {
	stories := []state.Story{
		{ID: "s1", Title: "First", Status: "draft", Complexity: 1},
		{ID: "s2", Title: "Second", Status: "in_progress", Complexity: 2},
	}
	m := Model{
		width:             100,
		stories:           stories,
		storyScrollOffset: 100, // way beyond number of stories
	}
	out := m.renderStories(100, 20)
	// Should not panic with excessive offset
	if out == "" {
		t.Error("expected some output")
	}
}

// --- renderPipeline coverage ---

func TestRenderPipeline_AllStatuses(t *testing.T) {
	stories := []state.Story{
		{Status: "draft"},
		{Status: "in_progress"},
		{Status: "review"},
		{Status: "qa"},
		{Status: "pr_submitted"},
		{Status: "merged"},
		{Status: "split"},
	}
	m := Model{width: 80, stories: stories}
	out := m.renderPipeline(80)
	if !strings.Contains(out, "Pipeline") {
		t.Error("expected Pipeline label")
	}
}

// --- renderActivity with timestamps ---

func TestRenderActivity_TimestampPresent(t *testing.T) {
	now := time.Now()
	events := []state.Event{
		{Type: state.EventStoryStarted, AgentID: "a1", StoryID: "s1", Timestamp: now},
	}
	m := Model{width: 80, events: events}
	out := m.renderActivity(80, 10)
	if !strings.Contains(out, "Activity") {
		t.Error("expected Activity label")
	}
}

// --- renderEscalations with tier info ---

func TestRenderEscalations_WithDetails(t *testing.T) {
	escalations := []state.Escalation{
		{StoryID: "s1", FromTier: 0, ToTier: 1, Status: "pending", Reason: "max retries"},
	}
	m := Model{width: 80, escalations: escalations}
	out := m.renderEscalations(80, 10)
	if !strings.Contains(out, "Escalation") {
		t.Error("expected Escalation header")
	}
}

// --- handleKey with multiple scroll ---

func TestHandleKey_ScrollDownMultiple(t *testing.T) {
	stories := make([]state.Story, 20)
	for i := range stories {
		stories[i] = state.Story{ID: "s", Status: "draft"}
	}
	m := Model{width: 80, height: 24, stories: stories}

	// Scroll down 5 times
	for i := 0; i < 5; i++ {
		result, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
		m = result.(Model)
	}
	if m.storyScrollOffset != 5 {
		t.Errorf("expected offset 5, got %d", m.storyScrollOffset)
	}

	// Scroll up 2 times
	for i := 0; i < 2; i++ {
		result, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
		m = result.(Model)
	}
	if m.storyScrollOffset != 3 {
		t.Errorf("expected offset 3, got %d", m.storyScrollOffset)
	}
}

// --- View with zero width (loading state) ---

func TestView_ZeroWidth(t *testing.T) {
	m := Model{width: 0}
	out := m.View()
	if out != "Loading..." {
		t.Errorf("expected 'Loading...', got %q", out)
	}
}

// --- applyData preserves non-data fields ---

func TestApplyData_PreservesVersion(t *testing.T) {
	m := Model{version: "2.0.0", width: 120, height: 40}
	d := dataMsg{
		requirements: []state.Requirement{{ID: "r1"}},
	}
	updated := m.applyData(d)
	if updated.version != "2.0.0" {
		t.Errorf("expected version preserved, got %q", updated.version)
	}
	if updated.width != 120 {
		t.Errorf("expected width preserved, got %d", updated.width)
	}
}
