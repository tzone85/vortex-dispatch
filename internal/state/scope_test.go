package state_test

import (
	"path/filepath"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

func TestLoadScopedView_FiltersByRepoPathAndReturnsRecentVisibleEvents(t *testing.T) {
	dir := t.TempDir()
	es, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	defer es.Close()

	ps, err := state.NewSQLiteStore(filepath.Join(dir, "proj.db"))
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer ps.Close()

	appendAndProject := func(evt state.Event) {
		t.Helper()
		if err := es.Append(evt); err != nil {
			t.Fatalf("append %s: %v", evt.Type, err)
		}
		if err := ps.Project(evt); err != nil {
			t.Fatalf("project %s: %v", evt.Type, err)
		}
	}
	appendOnly := func(evt state.Event) {
		t.Helper()
		if err := es.Append(evt); err != nil {
			t.Fatalf("append %s: %v", evt.Type, err)
		}
	}

	appendAndProject(state.NewEvent(state.EventReqSubmitted, "system", "", map[string]any{
		"id":          "req-alpha",
		"title":       "Alpha",
		"description": "Repo alpha",
		"repo_path":   "/repo/alpha",
	}))
	appendAndProject(state.NewEvent(state.EventReqSubmitted, "system", "", map[string]any{
		"id":          "req-beta",
		"title":       "Beta",
		"description": "Repo beta",
		"repo_path":   "/repo/beta",
	}))

	appendAndProject(state.NewEvent(state.EventStoryCreated, "system", "story-alpha", map[string]any{
		"id":                  "story-alpha",
		"req_id":              "req-alpha",
		"title":               "Alpha story",
		"description":         "Alpha desc",
		"acceptance_criteria": "Alpha AC",
		"complexity":          2,
	}))
	appendAndProject(state.NewEvent(state.EventStoryCreated, "system", "story-beta", map[string]any{
		"id":                  "story-beta",
		"req_id":              "req-beta",
		"title":               "Beta story",
		"description":         "Beta desc",
		"acceptance_criteria": "Beta AC",
		"complexity":          3,
	}))

	appendAndProject(state.NewEvent(state.EventStoryEscalated, "lead", "story-beta", map[string]any{
		"from_tier": 0,
		"to_tier":   1,
		"reason":    "beta escalation",
	}))
	appendAndProject(state.NewEvent(state.EventStoryEscalated, "lead", "story-alpha", map[string]any{
		"from_tier": 0,
		"to_tier":   2,
		"reason":    "alpha escalation",
	}))

	appendOnly(state.NewEvent(state.EventStoryReviewFailed, "reviewer", "story-alpha", map[string]any{
		"source": "test",
	}))
	appendOnly(state.NewEvent(state.EventStoryReviewFailed, "reviewer", "story-beta", map[string]any{
		"source": "test",
	}))
	appendOnly(state.NewEvent(state.EventStoryCompleted, "reviewer", "story-alpha", map[string]any{
		"source": "test",
	}))

	view, err := state.LoadScopedView(es, ps, state.ReqFilter{RepoPath: "/repo/alpha"}, 2)
	if err != nil {
		t.Fatalf("LoadScopedView: %v", err)
	}

	if len(view.Requirements) != 1 || view.Requirements[0].ID != "req-alpha" {
		t.Fatalf("requirements = %+v, want only req-alpha", view.Requirements)
	}
	if len(view.Stories) != 1 || view.Stories[0].ID != "story-alpha" {
		t.Fatalf("stories = %+v, want only story-alpha", view.Stories)
	}
	if len(view.Escalations) != 1 || view.Escalations[0].StoryID != "story-alpha" {
		t.Fatalf("escalations = %+v, want only story-alpha", view.Escalations)
	}
	if len(view.Events) != 2 {
		t.Fatalf("events len = %d, want 2", len(view.Events))
	}
	if view.Events[0].StoryID != "story-alpha" || view.Events[0].Type != state.EventStoryReviewFailed {
		t.Fatalf("events[0] = %+v, want recent alpha review failure", view.Events[0])
	}
	if view.Events[1].StoryID != "story-alpha" || view.Events[1].Type != state.EventStoryCompleted {
		t.Fatalf("events[1] = %+v, want recent alpha completion", view.Events[1])
	}
}
