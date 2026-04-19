// API handlers for read-only programmatic access (Phase 2/3).
//
// These endpoints expose VXD state to external integrations (Prometheus,
// custom dashboards, monitoring tools, future partner APIs). All endpoints
// are read-only in this iteration — mutations require auth and come in a
// later phase.
//
// Endpoints (versioned under /api/v1/):
//   GET /api/v1/requirements              — list all requirements
//   GET /api/v1/requirements/{id}         — single requirement detail
//   GET /api/v1/requirements/{id}/stories — stories for a requirement
//   GET /api/v1/stories                   — list stories (filter by ?status=)
//   GET /api/v1/stories/{id}              — single story detail
//   GET /api/v1/metrics                   — pipeline-wide metrics summary
//
// All responses are JSON. Errors return {"error": "..."} with an
// appropriate HTTP status code.

package web

import (
	"encoding/json"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

// registerAPIRoutes wires the read-only REST API onto the given mux.
// Called from Server.Start() after the WebSocket and health endpoints.
func (s *Server) registerAPIRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/requirements", s.handleRequirements)
	mux.HandleFunc("/api/v1/requirements/", s.handleRequirementDetail)
	mux.HandleFunc("/api/v1/stories", s.handleStories)
	mux.HandleFunc("/api/v1/stories/", s.handleStoryDetail)
	mux.HandleFunc("/api/v1/metrics", s.handleMetricsSummary)
}

// handleRequirements lists all requirements (most recent first via
// projection store ordering).
func (s *Server) handleRequirements(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "only GET supported")
		return
	}
	view, err := state.LoadScopedView(nil, s.projStore, s.reqFilter, 0)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"count":        len(view.Requirements),
		"requirements": view.Requirements,
	})
}

// handleRequirementDetail returns a single requirement and (optionally)
// its stories at /api/v1/requirements/{id} or
// /api/v1/requirements/{id}/stories.
func (s *Server) handleRequirementDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "only GET supported")
		return
	}
	// Strip prefix and split: "abc123" or "abc123/stories"
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/requirements/")
	if rest == "" {
		writeAPIError(w, http.StatusBadRequest, "requirement id required")
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	reqID := parts[0]
	req, err := s.findRequirement(reqID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req == nil {
		writeAPIError(w, http.StatusNotFound, "requirement not found")
		return
	}

	if len(parts) == 2 && parts[1] == "stories" {
		view, err := state.LoadScopedView(nil, s.projStore, s.reqFilter, 0)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		var stories []state.Story
		for _, story := range view.Stories {
			if story.ReqID == reqID {
				stories = append(stories, story)
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"requirement_id": reqID,
			"count":          len(stories),
			"stories":        stories,
		})
		return
	}
	writeJSON(w, http.StatusOK, req)
}

// handleStories lists all stories with optional status filter.
//
//	GET /api/v1/stories?status=in_progress
func (s *Server) handleStories(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "only GET supported")
		return
	}
	view, err := state.LoadScopedView(nil, s.projStore, s.reqFilter, 0)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	statusFilter := r.URL.Query().Get("status")
	reqIDFilter := r.URL.Query().Get("req_id")

	var stories []state.Story
	for _, story := range view.Stories {
		if statusFilter != "" && story.Status != statusFilter {
			continue
		}
		if reqIDFilter != "" && story.ReqID != reqIDFilter {
			continue
		}
		stories = append(stories, story)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"count":   len(stories),
		"stories": stories,
	})
}

// handleStoryDetail returns a single story by ID.
func (s *Server) handleStoryDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "only GET supported")
		return
	}
	storyID := path.Base(r.URL.Path)
	if storyID == "" || storyID == "stories" {
		writeAPIError(w, http.StatusBadRequest, "story id required")
		return
	}
	story, err := s.findStory(storyID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if story == nil {
		writeAPIError(w, http.StatusNotFound, "story not found")
		return
	}
	writeJSON(w, http.StatusOK, story)
}

// handleMetricsSummary returns aggregate counts for monitoring dashboards.
// Lighter-weight than the full vxd metrics CLI command — designed for
// frequent polling by Prometheus, Grafana, etc.
func (s *Server) handleMetricsSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "only GET supported")
		return
	}
	view, err := state.LoadScopedView(s.eventStore, s.projStore, s.reqFilter, 0)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	statusCounts := make(map[string]int)
	slaBreaches := 0
	escalations := 0
	for _, evt := range view.Events {
		switch evt.Type {
		case state.EventStorySLABreached:
			slaBreaches++
		case state.EventStoryEscalated:
			escalations++
		}
	}
	for _, st := range view.Stories {
		statusCounts[st.Status]++
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"requirements_total":  len(view.Requirements),
		"stories_total":       len(view.Stories),
		"stories_by_status":   statusCounts,
		"events_total":        len(view.Events),
		"sla_breaches_total":  slaBreaches,
		"escalations_total":   escalations,
		"uptime_seconds":      int(time.Since(s.startTime).Seconds()),
	})
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// writeAPIError writes a uniform error response.
func writeAPIError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
