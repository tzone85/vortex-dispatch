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
	"log"
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
	reqs, err := s.projStore.ListRequirementsFiltered(s.reqFilter)
	if err != nil {
		log.Printf("[api] list requirements: %v", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"count":        len(reqs),
		"requirements": reqs,
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

	if len(parts) == 2 && parts[1] == "stories" {
		stories, err := s.projStore.ListStories(state.StoryFilter{ReqID: reqID})
		if err != nil {
			log.Printf("[api] list stories for %s: %v", reqID, err)
			writeAPIError(w, http.StatusInternalServerError, "internal error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"requirement_id": reqID,
			"count":          len(stories),
			"stories":        stories,
		})
		return
	}

	req, err := s.projStore.GetRequirement(reqID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "requirement not found")
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
	filter := state.StoryFilter{
		Status: r.URL.Query().Get("status"),
		ReqID:  r.URL.Query().Get("req_id"),
	}
	stories, err := s.projStore.ListStories(filter)
	if err != nil {
		log.Printf("[api] list stories with filter %+v: %v", filter, err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
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
	story, err := s.projStore.GetStory(storyID)
	if err != nil {
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
	reqs, err := s.projStore.ListRequirementsFiltered(s.reqFilter)
	if err != nil {
		log.Printf("[api] metrics list requirements: %v", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	stories, err := s.projStore.ListStories(state.StoryFilter{})
	if err != nil {
		log.Printf("[api] metrics list stories: %v", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	totalEvents, _ := s.eventStore.Count(state.EventFilter{})
	slaBreaches, _ := s.eventStore.Count(state.EventFilter{Type: state.EventStorySLABreached})
	escalations, _ := s.eventStore.Count(state.EventFilter{Type: state.EventStoryEscalated})

	statusCounts := make(map[string]int)
	for _, st := range stories {
		statusCounts[st.Status]++
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"requirements_total":  len(reqs),
		"stories_total":       len(stories),
		"stories_by_status":   statusCounts,
		"events_total":        totalEvents,
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
