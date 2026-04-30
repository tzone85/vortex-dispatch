package engine

import (
	"fmt"
	"log"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tzone85/vortex-dispatch/internal/agent"
	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/graph"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// safeStoryIDPattern defines the regex pattern for valid story IDs to prevent
// shell injection and invalid branch names. NXD cross-port: Safe story ID pattern to validate against before dispatch
var safeStoryIDPattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// Assignment represents a story routed to a specific agent role with session
// and branch metadata.
type Assignment struct {
	StoryID      string
	Role         agent.Role
	AgentID      string
	SessionName  string
	WorktreePath string
	Branch       string
}

// Dispatcher routes ready stories to agent roles based on complexity and
// emits assignment events.
type Dispatcher struct {
	config     config.Config
	eventStore state.EventStore
	projStore  state.ProjectionStore
}

// NewDispatcher creates a Dispatcher wired to the given configuration, event
// store, and projection store.
func NewDispatcher(cfg config.Config, es state.EventStore, ps state.ProjectionStore) *Dispatcher {
	return &Dispatcher{
		config:     cfg,
		eventStore: es,
		projStore:  ps,
	}
}

// DispatchWave identifies stories ready for execution (all dependencies
// satisfied) and assigns each to an agent role based on complexity. It returns
// assignments for all dispatchable stories and emits AGENT_SPAWNED and
// STORY_ASSIGNED events. The waveNumber tags each dispatched story so that
// completion summaries can group stories by dispatch wave.
//
// Sequential-first ordering: if any ready stories have WaveHint=="sequential",
// only one sequential story is dispatched. Otherwise, parallel stories are
// dispatched with overlap filtering to prevent file conflicts.
func (d *Dispatcher) DispatchWave(dag *graph.DAG, completed map[string]bool, reqID string, stories []PlannedStory, waveNumber int) ([]Assignment, error) {
	readyIDs := dag.ReadyNodes(completed)
	if len(readyIDs) == 0 {
		return nil, nil
	}

	storyMap := make(map[string]PlannedStory, len(stories))
	for _, s := range stories {
		storyMap[s.ID] = s
	}

	// Collect ready stories and auto-tag wave hints
	readyStories := make([]PlannedStory, 0, len(readyIDs))
	for _, id := range readyIDs {
		if !d.storyDispatchable(id) {
			continue
		}
		if s, ok := storyMap[id]; ok {
			readyStories = append(readyStories, s)
		}
	}
	d.autoTagWaveHints(readyStories)

	// Validate story IDs for security (shell injection and branch name safety)
	for _, story := range readyStories {
		if !safeStoryIDPattern.MatchString(story.ID) {
			return nil, fmt.Errorf("unsafe story ID %q: contains invalid characters (must match: ^[a-zA-Z0-9._-]+$)", story.ID)
		}
	}

	// Determine which stories to dispatch this wave
	dispatchable := d.selectDispatchable(readyStories)

	// Enforce concurrent agent limit
	if max := d.config.Routing.MaxConcurrentAgents; max > 0 && len(dispatchable) > max {
		log.Printf("[dispatcher] capping wave from %d to %d stories (max_concurrent_agents)", len(dispatchable), max)
		dispatchable = dispatchable[:max]
	}

	// Load agent reputations for preferential routing (best-effort).
	reps, repErr := AgentReputations(d.eventStore)
	if repErr != nil {
		log.Printf("[dispatcher] reputation load (non-fatal): %v", repErr)
		reps = nil
	}

	assignments := make([]Assignment, 0, len(dispatchable))
	agentCounter := 0

	for _, story := range dispatchable {
		role := d.routeStory(story)
		agentCounter++

		// Prefer highest-reputation agent for this role; fall back to counter-based ID.
		agentID := preferredAgentID(reps, string(role), reqID, agentCounter)

		sessionName := fmt.Sprintf("vxd-%s-%s-%d", reqID, role, agentCounter)
		branch := fmt.Sprintf("vxd/%s", story.ID)

		assignment := Assignment{
			StoryID:     story.ID,
			Role:        role,
			AgentID:     agentID,
			SessionName: sessionName,
			Branch:      branch,
		}
		assignments = append(assignments, assignment)

		// Emit spawn event
		spawnEvt := state.NewEvent(state.EventAgentSpawned, agentID, story.ID, map[string]any{
			"role":         string(role),
			"session_name": sessionName,
			"runtime":      d.runtimeForRole(role),
		})
		if err := d.eventStore.Append(spawnEvt); err != nil {
			return nil, fmt.Errorf("emit agent spawned: %w", err)
		}
		if err := d.projStore.Project(spawnEvt); err != nil {
			return nil, fmt.Errorf("project agent spawned: %w", err)
		}

		// Emit assignment event
		assignEvt := state.NewEvent(state.EventStoryAssigned, agentID, story.ID, map[string]any{
			"agent_id": agentID,
			"wave":     waveNumber,
			"branch":   branch,
		})
		if err := d.eventStore.Append(assignEvt); err != nil {
			return nil, fmt.Errorf("emit story assigned: %w", err)
		}
		if err := d.projStore.Project(assignEvt); err != nil {
			return nil, fmt.Errorf("project story assigned: %w", err)
		}
	}

	return assignments, nil
}

func (d *Dispatcher) storyDispatchable(storyID string) bool {
	story, err := d.projStore.GetStory(storyID)
	if err != nil {
		return true
	}
	switch story.Status {
	case "draft", "estimated":
		return true
	default:
		return false
	}
}

// autoTagWaveHints assigns wave hints to stories that don't already have one
// by checking if any owned file matches a sequential file pattern from config.
func (d *Dispatcher) autoTagWaveHints(stories []PlannedStory) {
	for i := range stories {
		if stories[i].WaveHint != "" {
			continue
		}
		for _, f := range stories[i].OwnedFiles {
			if d.matchesSequentialPattern(f) {
				stories[i].WaveHint = "sequential"
				break
			}
		}
		if stories[i].WaveHint == "" {
			stories[i].WaveHint = "parallel"
		}
	}
}

// matchesSequentialPattern checks if a file path matches any of the configured
// sequential file patterns using filepath.Match.
func (d *Dispatcher) matchesSequentialPattern(file string) bool {
	for _, pattern := range d.config.Planning.SequentialFilePatterns {
		// Match against the full path
		if matched, _ := filepath.Match(pattern, file); matched {
			return true
		}
		// Also match against just the filename for patterns like "package.json"
		if matched, _ := filepath.Match(pattern, filepath.Base(file)); matched {
			return true
		}
	}
	return false
}

// selectDispatchable applies sequential-first ordering and overlap filtering
// to determine which ready stories should be dispatched in this wave.
func (d *Dispatcher) selectDispatchable(readyStories []PlannedStory) []PlannedStory {
	// Check if any ready stories are sequential
	var sequential []PlannedStory
	var parallel []PlannedStory
	for _, s := range readyStories {
		if s.WaveHint == "sequential" {
			sequential = append(sequential, s)
		} else {
			parallel = append(parallel, s)
		}
	}

	// Sequential-first: if any sequential stories are ready, dispatch only
	// one of them (they modify shared/core files and must run alone).
	if len(sequential) > 0 {
		return []PlannedStory{sequential[0]}
	}

	// For parallel stories, filter out those that share owned files with
	// stories already selected for this wave.
	return d.filterOverlapping(parallel)
}

// filterOverlapping removes stories whose owned files overlap with
// already-selected stories in this wave.
func (d *Dispatcher) filterOverlapping(stories []PlannedStory) []PlannedStory {
	dispatched := make([]PlannedStory, 0, len(stories))
	claimedFiles := make(map[string]bool)

	for _, s := range stories {
		if d.hasFileConflict(s, claimedFiles) {
			continue
		}
		// Claim this story's files
		for _, f := range s.OwnedFiles {
			claimedFiles[f] = true
		}
		dispatched = append(dispatched, s)
	}

	return dispatched
}

// hasFileConflict checks if any of the story's owned files are already claimed.
func (d *Dispatcher) hasFileConflict(story PlannedStory, claimed map[string]bool) bool {
	for _, f := range story.OwnedFiles {
		if claimed[f] {
			return true
		}
	}
	return false
}

// routeStory determines the agent role for a story.
// It reads STORY_ESCALATED events to find the highest escalation tier reached:
//   - Tier 0 (no escalation): route by complexity via RouteByComplexity
//   - Tier 1: route to RoleSenior
//   - Tier 2+: defensive fallback to RoleSenior with a warning (these should
//     be intercepted by the monitor before reaching the dispatcher)
func (d *Dispatcher) routeStory(story PlannedStory) agent.Role {
	events, err := d.eventStore.List(state.EventFilter{
		Type:    state.EventStoryEscalated,
		StoryID: story.ID,
	})
	if err == nil && len(events) > 0 {
		maxTier := 0
		for _, evt := range events {
			payload := state.DecodePayload(evt.Payload)
			if toTier, ok := payload["to_tier"].(float64); ok && int(toTier) > maxTier {
				maxTier = int(toTier)
			}
		}
		switch {
		case maxTier >= 2:
			log.Printf("[dispatcher] WARNING: story %s at tier %d reached routeStory, expected monitor interception", story.ID, maxTier)
			return agent.RoleSenior
		case maxTier == 1:
			return agent.RoleSenior
		}
	}
	return agent.RouteByComplexity(story.Complexity, d.config.Routing)
}

func (d *Dispatcher) runtimeForRole(role agent.Role) string {
	modelCfg := role.ModelConfig(d.config.Models)
	provider := strings.ToLower(modelCfg.Provider)

	providerRuntimes := map[string][]string{
		"anthropic": {"claude-code", "claude"},
		"openai":    {"codex", "openai"},
		"google":    {"gemini"},
		"gemini":    {"gemini"},
	}

	if candidates, ok := providerRuntimes[provider]; ok {
		for _, name := range candidates {
			if _, exists := d.config.Runtimes[name]; exists {
				return name
			}
		}
	}
	for name := range d.config.Runtimes {
		return name
	}
	return "claude-code"
}

// preferredAgentID returns the highest-reputation agent ID for a given role,
// falling back to a counter-based ID when no reputation data exists.
func preferredAgentID(reps map[string]agent.AgentReputation, role, reqID string, counter int) string {
	fallback := fmt.Sprintf("%s-%s-%d", role, reqID, counter)
	if reps == nil {
		return fallback
	}
	bestID := BestAgentForRole(reps, role)
	if bestID == "" {
		return fallback
	}
	if rep, ok := reps[bestID]; ok {
		log.Printf("[dispatcher] reputation-routed %s → %s (score=%.1f, stories=%d)",
			role, bestID, rep.OverallScore(), rep.TotalStories)
		return bestID
	}
	return fallback
}
