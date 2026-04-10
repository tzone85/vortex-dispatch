package engine

import (
	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// ReviewGate resolves the human-review mode for a requirement and tracks
// plan/story approval events emitted by the CLI approve/reject commands.
type ReviewGate struct {
	events state.EventStore
}

// NewReviewGate returns a ReviewGate backed by the given event store.
func NewReviewGate(es state.EventStore) *ReviewGate {
	return &ReviewGate{events: es}
}

// ResolveMode determines the effective review mode for reqID using a
// three-tier priority chain:
//
//  1. Latest REVIEW_MODE_SET event for the requirement (highest priority)
//  2. cfg.ReviewMode if non-empty
//  3. "auto" when cfg.AutoMerge is true, otherwise "manual" (fallback)
func (g *ReviewGate) ResolveMode(reqID string, cfg config.MergeConfig) string {
	events, err := g.events.List(state.EventFilter{Type: state.EventReviewModeSet})
	if err == nil {
		// Scan in reverse so we pick up the most-recent event first.
		for i := len(events) - 1; i >= 0; i-- {
			payload := state.DecodePayload(events[i].Payload)
			if id, _ := payload["req_id"].(string); id == reqID {
				if mode, _ := payload["mode"].(string); mode != "" {
					return mode
				}
			}
		}
	}

	if cfg.ReviewMode != "" {
		return cfg.ReviewMode
	}

	if cfg.AutoMerge {
		return "auto"
	}

	return "manual"
}

// PlanApproved returns true if a PLAN_APPROVED event has been recorded for
// the given requirement ID.
func (g *ReviewGate) PlanApproved(reqID string) bool {
	events, err := g.events.List(state.EventFilter{Type: state.EventPlanApproved})
	if err != nil {
		return false
	}

	for _, e := range events {
		payload := state.DecodePayload(e.Payload)
		if id, _ := payload["req_id"].(string); id == reqID {
			return true
		}
	}

	return false
}

// StoryApproved returns true if a STORY_APPROVED event has been recorded for
// the given story ID.
func (g *ReviewGate) StoryApproved(storyID string) bool {
	events, err := g.events.List(state.EventFilter{
		Type:    state.EventStoryApproved,
		StoryID: storyID,
	})
	if err != nil {
		return false
	}

	return len(events) > 0
}

// PendingApprovals returns all stories for reqID that are currently in the
// "awaiting_approval" status.
func (g *ReviewGate) PendingApprovals(reqID string, ps state.ProjectionStore) ([]state.Story, error) {
	stories, err := ps.ListStories(state.StoryFilter{ReqID: reqID})
	if err != nil {
		return nil, err
	}

	var pending []state.Story
	for _, s := range stories {
		if s.Status == "awaiting_approval" {
			pending = append(pending, s)
		}
	}

	return pending, nil
}
