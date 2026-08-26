package state

import (
	"path/filepath"
	"testing"
)

// TestProject_StoryQAFlaky pins the STORY_QA_FLAKY projection contract: the
// event is handled explicitly (never the default-WARNING branch — the
// exhaustiveness test enforces that separately) and is informational — it must
// NOT mutate story status, because the accompanying STORY_QA_PASSED /
// STORY_QA_FAILED event carries the actual transition.
func TestProject_StoryQAFlaky(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer s.Close()

	s.Project(NewEvent(EventReqSubmitted, "", "", map[string]any{"id": "REQ-FLK1", "title": "Flaky"}))
	s.Project(NewEvent(EventStoryCreated, "", "S-FLK1", map[string]any{
		"id": "S-FLK1", "req_id": "REQ-FLK1", "title": "Story", "complexity": 2,
	}))
	s.Project(NewEvent(EventStoryStarted, "", "S-FLK1", nil))

	if err := s.Project(NewEvent(EventStoryQAFlaky, "qa", "S-FLK1", map[string]any{
		"step": "test", "attempts": 2,
	})); err != nil {
		t.Fatalf("Project STORY_QA_FLAKY: %v", err)
	}

	story, err := s.GetStory("S-FLK1")
	if err != nil {
		t.Fatalf("get story: %v", err)
	}
	if story.Status != "in_progress" {
		t.Errorf("STORY_QA_FLAKY mutated story status: got %q, want in_progress (event must be informational)", story.Status)
	}
}
