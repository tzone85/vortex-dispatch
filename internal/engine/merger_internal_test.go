package engine

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

func TestBuildPRBody_NoTemplate(t *testing.T) {
	m := &Merger{
		config: config.MergeConfig{
			PRTemplate: "",
		},
	}
	got := m.buildPRBody("s-001", "Add user model")
	if !strings.Contains(got, "s-001") {
		t.Error("expected story ID in body")
	}
	if !strings.Contains(got, "Add user model") {
		t.Error("expected story title in body")
	}
}

func TestBuildPRBody_WithTemplate(t *testing.T) {
	m := &Merger{
		config: config.MergeConfig{
			PRTemplate: "## {story_id}\n\n{description}\n\nAC: {acceptance_criteria}",
		},
	}
	got := m.buildPRBody("s-002", "Create handler")
	if !strings.Contains(got, "## s-002") {
		t.Error("expected story ID placeholder replaced")
	}
	if !strings.Contains(got, "Create handler") {
		t.Error("expected description placeholder replaced with title")
	}
}

func TestBuildPRBody_WithTemplateAndProjStore(t *testing.T) {
	dir := t.TempDir()
	es, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("create event store: %v", err)
	}
	defer es.Close()
	ps, err := state.NewSQLiteStore(filepath.Join(dir, "proj.db"))
	if err != nil {
		t.Fatalf("create sqlite store: %v", err)
	}
	defer ps.Close()

	// Create story in projection
	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-003", map[string]any{
		"id":                  "s-003",
		"req_id":              "r-001",
		"title":               "Create model",
		"description":         "Implement the user model with validation",
		"acceptance_criteria": "User struct exists",
		"complexity":          2,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	m := &Merger{
		config: config.MergeConfig{
			PRTemplate: "Story: {story_id}\nDesc: {description}\nAC: {acceptance_criteria}",
		},
		projStore: ps,
	}

	got := m.buildPRBody("s-003", "Create model")
	if !strings.Contains(got, "Implement the user model with validation") {
		t.Errorf("expected rich description from projStore, got:\n%s", got)
	}
	if !strings.Contains(got, "User struct exists") {
		t.Errorf("expected acceptance criteria from projStore, got:\n%s", got)
	}
}

func TestBuildPRBody_NilProjStore(t *testing.T) {
	m := &Merger{
		config: config.MergeConfig{
			PRTemplate: "{story_id}: {description}",
		},
		projStore: nil,
	}
	got := m.buildPRBody("s-001", "My task")
	if !strings.Contains(got, "s-001") {
		t.Error("expected story ID")
	}
	// Should use title as description fallback
	if !strings.Contains(got, "My task") {
		t.Error("expected title as description fallback")
	}
}
