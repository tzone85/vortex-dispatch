package dashboard

import (
	"path/filepath"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

func TestFetchData_ScopesToVisibleWorkspace(t *testing.T) {
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

	for _, tc := range []struct {
		reqID    string
		storyID  string
		repoPath string
	}{
		{reqID: "req-alpha", storyID: "story-alpha", repoPath: "/repo/alpha"},
		{reqID: "req-beta", storyID: "story-beta", repoPath: "/repo/beta"},
	} {
		appendAndProject(state.NewEvent(state.EventReqSubmitted, "system", "", map[string]any{
			"id":          tc.reqID,
			"title":       tc.reqID,
			"description": tc.reqID,
			"repo_path":   tc.repoPath,
		}))
		appendAndProject(state.NewEvent(state.EventStoryCreated, "system", tc.storyID, map[string]any{
			"id":                  tc.storyID,
			"req_id":              tc.reqID,
			"title":               tc.storyID,
			"description":         tc.storyID,
			"acceptance_criteria": "ok",
			"complexity":          2,
		}))
	}

	m := New(es, ps, "test", state.ReqFilter{RepoPath: "/repo/alpha"})
	msg := m.fetchData()().(dataMsg)
	if msg.err != nil {
		t.Fatalf("fetchData err: %v", msg.err)
	}

	if len(msg.requirements) != 1 || msg.requirements[0].ID != "req-alpha" {
		t.Fatalf("requirements = %+v, want only req-alpha", msg.requirements)
	}
	if len(msg.stories) != 1 || msg.stories[0].ID != "story-alpha" {
		t.Fatalf("stories = %+v, want only story-alpha", msg.stories)
	}
}
