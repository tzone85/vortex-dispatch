package state

// ScopedView contains projection and event data constrained to the visible
// workspace requirements defined by a ReqFilter.
type ScopedView struct {
	Requirements []Requirement
	Stories      []Story
	Agents       []Agent
	Events       []Event
	Escalations  []Escalation
}

// LoadScopedView loads requirements and their related state, applying the
// given requirement filter consistently across projections and events.
func LoadScopedView(es EventStore, ps *SQLiteStore, filter ReqFilter, eventLimit int) (ScopedView, error) {
	view := ScopedView{}

	reqs, err := ps.ListRequirementsFiltered(filter)
	if err != nil {
		return view, err
	}
	view.Requirements = reqs

	reqIDs := make(map[string]struct{}, len(reqs))
	for _, req := range reqs {
		reqIDs[req.ID] = struct{}{}
	}

	allStories, err := ps.ListStories(StoryFilter{})
	if err != nil {
		return view, err
	}
	view.Stories = filterStoriesByRequirementIDs(allStories, reqIDs)

	storyIDs := make(map[string]struct{}, len(view.Stories))
	for _, story := range view.Stories {
		storyIDs[story.ID] = struct{}{}
	}

	allAgents, err := ps.ListAgents(AgentFilter{})
	if err != nil {
		return view, err
	}
	scoped := hasReqFilter(filter)
	if scoped {
		view.Agents = filterAgentsByStoryIDs(allAgents, storyIDs)
	} else {
		view.Agents = allAgents
	}

	agentIDs := make(map[string]struct{}, len(view.Agents))
	for _, agent := range view.Agents {
		agentIDs[agent.ID] = struct{}{}
	}

	allEscalations, err := ps.ListEscalations()
	if err != nil {
		return view, err
	}
	if scoped {
		view.Escalations = filterEscalationsByStoryIDs(allEscalations, storyIDs)
	} else {
		view.Escalations = allEscalations
	}

	if es == nil {
		return view, nil
	}

	allEvents, err := es.List(EventFilter{})
	if err != nil {
		return view, err
	}
	if scoped {
		view.Events = filterEventsByVisibility(allEvents, reqIDs, storyIDs, agentIDs)
	} else {
		view.Events = allEvents
	}
	view.Events = keepMostRecentEvents(view.Events, eventLimit)

	return view, nil
}

func hasReqFilter(filter ReqFilter) bool {
	return filter.RepoPath != "" || filter.ExcludeArchived
}

func filterStoriesByRequirementIDs(stories []Story, reqIDs map[string]struct{}) []Story {
	filtered := make([]Story, 0, len(stories))
	for _, story := range stories {
		if _, ok := reqIDs[story.ReqID]; ok {
			filtered = append(filtered, story)
		}
	}
	return filtered
}

func filterAgentsByStoryIDs(agents []Agent, storyIDs map[string]struct{}) []Agent {
	filtered := make([]Agent, 0, len(agents))
	for _, agent := range agents {
		if _, ok := storyIDs[agent.CurrentStoryID]; ok {
			filtered = append(filtered, agent)
		}
	}
	return filtered
}

func filterEscalationsByStoryIDs(escalations []Escalation, storyIDs map[string]struct{}) []Escalation {
	filtered := make([]Escalation, 0, len(escalations))
	for _, escalation := range escalations {
		if _, ok := storyIDs[escalation.StoryID]; ok {
			filtered = append(filtered, escalation)
		}
	}
	return filtered
}

func filterEventsByVisibility(events []Event, reqIDs, storyIDs, agentIDs map[string]struct{}) []Event {
	filtered := make([]Event, 0, len(events))
	for _, evt := range events {
		if eventVisible(evt, reqIDs, storyIDs, agentIDs) {
			filtered = append(filtered, evt)
		}
	}
	return filtered
}

func eventVisible(evt Event, reqIDs, storyIDs, agentIDs map[string]struct{}) bool {
	if evt.StoryID != "" {
		if _, ok := storyIDs[evt.StoryID]; ok {
			return true
		}
	}
	if evt.AgentID != "" {
		if _, ok := agentIDs[evt.AgentID]; ok {
			return true
		}
	}
	reqID := eventRequirementID(evt)
	if reqID == "" {
		return false
	}
	_, ok := reqIDs[reqID]
	return ok
}

func eventRequirementID(evt Event) string {
	payload := DecodePayload(evt.Payload)
	if reqID := payloadStr(payload, "req_id"); reqID != "" {
		return reqID
	}

	switch evt.Type {
	case EventReqSubmitted,
		EventReqAnalyzed,
		EventReqPlanned,
		EventReqPaused,
		EventReqResumed,
		EventReqCompleted,
		EventReqEstimated:
		return payloadStr(payload, "id")
	default:
		return ""
	}
}

func keepMostRecentEvents(events []Event, limit int) []Event {
	if limit <= 0 || len(events) <= limit {
		return events
	}
	return events[len(events)-limit:]
}
