// internal/web/data.go
package web

import (
	"encoding/json"

	"github.com/tzone85/vortex-dispatch/internal/graph"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

type StateSnapshot struct {
	Agents       []state.Agent       `json:"agents"`
	Stories      []state.Story       `json:"stories"`
	Pipeline     PipelineCounts      `json:"pipeline"`
	Events       []EventSummary      `json:"events"`
	Escalations  []state.Escalation  `json:"escalations"`
	Requirements []state.Requirement `json:"requirements"`
	DAG          *graph.DAGExport    `json:"dag,omitempty"`
}

type PipelineCounts struct {
	Planned    int `json:"planned"`
	InProgress int `json:"in_progress"`
	Review     int `json:"review"`
	QA         int `json:"qa"`
	PR         int `json:"pr"`
	Merged     int `json:"merged"`
	Split      int `json:"split"`
}

type EventSummary struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	AgentID   string `json:"agent_id"`
	StoryID   string `json:"story_id"`
}

func (s *Server) BuildSnapshot() (StateSnapshot, error) {
	snap := StateSnapshot{}

	view, err := state.LoadScopedView(s.eventStore, s.projStore, s.reqFilter, 50)
	if err != nil {
		return snap, err
	}
	snap.Requirements = view.Requirements
	snap.Stories = view.Stories
	snap.Agents = view.Agents
	snap.Escalations = view.Escalations

	// Pipeline counts
	for _, story := range snap.Stories {
		switch mapStatusToBucket(story.Status) {
		case "planned":
			snap.Pipeline.Planned++
		case "in_progress":
			snap.Pipeline.InProgress++
		case "review":
			snap.Pipeline.Review++
		case "qa":
			snap.Pipeline.QA++
		case "pr_submitted":
			snap.Pipeline.PR++
		case "merged":
			snap.Pipeline.Merged++
		case "split":
			snap.Pipeline.Split++
		}
	}

	for _, evt := range view.Events {
		snap.Events = append(snap.Events, EventSummary{
			Type:      string(evt.Type),
			Timestamp: evt.Timestamp.Format("15:04:05"),
			AgentID:   evt.AgentID,
			StoryID:   evt.StoryID,
		})
	}

	// Include DAG export if available.
	snap.DAG = s.dagExport

	return snap, nil
}

func (s *Server) SnapshotJSON() ([]byte, error) {
	snap, err := s.BuildSnapshot()
	if err != nil {
		return nil, err
	}
	return json.Marshal(snap)
}

// mapStatusToBucket maps story statuses to pipeline buckets.
// This duplicates dashboard.mapStatusToBucket — both packages need it
// and they live in different packages.
func mapStatusToBucket(status string) string {
	switch status {
	case "draft", "estimated", "planned", "assigned":
		return "planned"
	case "in_progress":
		return "in_progress"
	case "review":
		return "review"
	case "qa", "qa_started", "qa_failed":
		return "qa"
	case "pr_submitted":
		return "pr_submitted"
	case "merged":
		return "merged"
	case "split":
		return "split"
	default:
		return "planned"
	}
}
