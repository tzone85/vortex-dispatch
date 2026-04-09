package engine

import (
	"github.com/tzone85/vortex-dispatch/internal/agent"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// ComputeReputationFromEvents reads QA events for an agent and computes
// reputation. This function lives in the engine package (not agent) to avoid
// a circular import between agent and state.
func ComputeReputationFromEvents(events []state.Event) agent.AgentReputation {
	if len(events) == 0 {
		return agent.AgentReputation{}
	}

	scores := make([]agent.Score, 0, len(events))
	for _, evt := range events {
		payload := state.DecodePayload(evt.Payload)
		quality, _ := payload["quality_score"].(float64)
		duration, _ := payload["duration_s"].(float64)

		reliability := 5 // default good; reduce if story was escalated
		if _, ok := payload["was_escalated"]; ok {
			reliability = 2
		}

		scores = append(scores, agent.Score{
			AgentID:     evt.AgentID,
			StoryID:     evt.StoryID,
			Quality:     int(quality),
			Reliability: reliability,
			DurationS:   int(duration),
		})
	}
	return agent.ComputeReputation(scores)
}

// AgentReputations computes reputation for every agent that has QA events in
// the event store. Returns a map of agentID -> AgentReputation.
func AgentReputations(es state.EventStore) (map[string]agent.AgentReputation, error) {
	passedEvents, err := es.List(state.EventFilter{Type: state.EventStoryQAPassed})
	if err != nil {
		return nil, err
	}
	failedEvents, err := es.List(state.EventFilter{Type: state.EventStoryQAFailed})
	if err != nil {
		return nil, err
	}

	allEvents := append(passedEvents, failedEvents...)

	// Group events by agent ID
	grouped := make(map[string][]state.Event)
	for _, evt := range allEvents {
		grouped[evt.AgentID] = append(grouped[evt.AgentID], evt)
	}

	reps := make(map[string]agent.AgentReputation, len(grouped))
	for agentID, events := range grouped {
		rep := ComputeReputationFromEvents(events)
		rep.AgentID = agentID
		reps[agentID] = rep
	}
	return reps, nil
}

// BestAgentForRole returns the agent ID with the highest overall reputation
// score among agents that have the given role prefix. Returns empty string if
// no reputation data exists.
func BestAgentForRole(reps map[string]agent.AgentReputation, rolePrefix string) string {
	bestID := ""
	bestScore := 0.0
	for id, rep := range reps {
		if len(id) < len(rolePrefix) {
			continue
		}
		if id[:len(rolePrefix)] != rolePrefix {
			continue
		}
		score := rep.OverallScore()
		if score > bestScore {
			bestScore = score
			bestID = id
		}
	}
	return bestID
}
