package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

func setupAPIServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	es, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { es.Close() })
	ps, err := state.NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ps.Close() })

	// Seed: 1 requirement, 2 stories, 1 SLA breach
	reqEvt := state.NewEvent(state.EventReqSubmitted, "system", "", map[string]any{
		"id":          "req-001",
		"title":       "Test requirement",
		"description": "for API tests",
		"repo_path":   "/tmp",
	})
	es.Append(reqEvt)
	ps.Project(reqEvt)

	for i, sid := range []string{"s-001", "s-002"} {
		_ = i
		created := state.NewEvent(state.EventStoryCreated, "system", sid, map[string]any{
			"id":          sid,
			"req_id":      "req-001",
			"title":       "Story " + sid,
			"description": "x",
			"complexity":  3,
		})
		es.Append(created)
		ps.Project(created)
	}

	es.Append(state.NewEvent(state.EventStorySLABreached, "agent", "s-001", map[string]any{
		"complexity": 3, "elapsed_seconds": 18000, "max_minutes": 240,
	}))

	return NewServer(es, ps, 0, state.ReqFilter{})
}

func setupScopedAPIServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	es, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { es.Close() })
	ps, err := state.NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ps.Close() })

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

	for _, tc := range []struct {
		reqID    string
		storyID  string
		repoPath string
	}{
		{reqID: "req-alpha", storyID: "s-alpha", repoPath: "/repo/alpha"},
		{reqID: "req-beta", storyID: "s-beta", repoPath: "/repo/beta"},
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
	appendOnly(state.NewEvent(state.EventStorySLABreached, "agent", "s-alpha", map[string]any{"source": "test"}))
	appendOnly(state.NewEvent(state.EventStorySLABreached, "agent", "s-beta", map[string]any{"source": "test"}))

	return NewServer(es, ps, 0, state.ReqFilter{RepoPath: "/repo/alpha"})
}

func TestAPI_ListRequirements(t *testing.T) {
	srv := setupAPIServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/requirements", nil)

	srv.handleRequirements(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if count, ok := resp["count"].(float64); !ok || count != 1 {
		t.Errorf("count = %v, want 1", resp["count"])
	}
}

func TestAPI_GetRequirement(t *testing.T) {
	srv := setupAPIServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/requirements/req-001", nil)

	srv.handleRequirementDetail(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var req state.Requirement
	json.NewDecoder(w.Body).Decode(&req)
	if req.ID != "req-001" {
		t.Errorf("id = %q, want req-001", req.ID)
	}
}

func TestAPI_GetRequirement_StoriesSubpath(t *testing.T) {
	srv := setupAPIServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/requirements/req-001/stories", nil)

	srv.handleRequirementDetail(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if count, ok := resp["count"].(float64); !ok || count != 2 {
		t.Errorf("count = %v, want 2", resp["count"])
	}
}

func TestAPI_GetRequirement_NotFound(t *testing.T) {
	srv := setupAPIServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/requirements/nonexistent", nil)

	srv.handleRequirementDetail(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestAPI_ListStories(t *testing.T) {
	srv := setupAPIServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/stories", nil)

	srv.handleStories(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if count, ok := resp["count"].(float64); !ok || count != 2 {
		t.Errorf("count = %v, want 2", resp["count"])
	}
}

func TestAPI_GetStory(t *testing.T) {
	srv := setupAPIServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/stories/s-001", nil)

	srv.handleStoryDetail(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var story state.Story
	json.NewDecoder(w.Body).Decode(&story)
	if story.ID != "s-001" {
		t.Errorf("id = %q, want s-001", story.ID)
	}
}

func TestAPI_MetricsSummary(t *testing.T) {
	srv := setupAPIServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/metrics", nil)

	srv.handleMetricsSummary(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)

	if total, ok := resp["requirements_total"].(float64); !ok || total != 1 {
		t.Errorf("requirements_total = %v, want 1", resp["requirements_total"])
	}
	if total, ok := resp["stories_total"].(float64); !ok || total != 2 {
		t.Errorf("stories_total = %v, want 2", resp["stories_total"])
	}
	if total, ok := resp["sla_breaches_total"].(float64); !ok || total != 1 {
		t.Errorf("sla_breaches_total = %v, want 1", resp["sla_breaches_total"])
	}
}

func TestAPI_RejectsNonGET(t *testing.T) {
	srv := setupAPIServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/requirements", nil)

	srv.handleRequirements(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestAPI_ScopesRequirementStoryAndMetricsEndpoints(t *testing.T) {
	srv := setupScopedAPIServer(t)

	wReq := httptest.NewRecorder()
	srv.handleRequirementDetail(wReq, httptest.NewRequest(http.MethodGet, "/api/v1/requirements/req-beta", nil))
	if wReq.Code != http.StatusNotFound {
		t.Fatalf("requirement detail status = %d, want 404", wReq.Code)
	}

	wStory := httptest.NewRecorder()
	srv.handleStoryDetail(wStory, httptest.NewRequest(http.MethodGet, "/api/v1/stories/s-beta", nil))
	if wStory.Code != http.StatusNotFound {
		t.Fatalf("story detail status = %d, want 404", wStory.Code)
	}

	wMetrics := httptest.NewRecorder()
	srv.handleMetricsSummary(wMetrics, httptest.NewRequest(http.MethodGet, "/api/v1/metrics", nil))
	if wMetrics.Code != http.StatusOK {
		t.Fatalf("metrics status = %d, want 200", wMetrics.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(wMetrics.Body).Decode(&resp); err != nil {
		t.Fatalf("decode metrics: %v", err)
	}
	if total, ok := resp["requirements_total"].(float64); !ok || total != 1 {
		t.Fatalf("requirements_total = %v, want 1", resp["requirements_total"])
	}
	if total, ok := resp["stories_total"].(float64); !ok || total != 1 {
		t.Fatalf("stories_total = %v, want 1", resp["stories_total"])
	}
	if total, ok := resp["sla_breaches_total"].(float64); !ok || total != 1 {
		t.Fatalf("sla_breaches_total = %v, want 1", resp["sla_breaches_total"])
	}
}
