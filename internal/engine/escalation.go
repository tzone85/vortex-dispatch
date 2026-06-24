package engine

import (
	"fmt"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/graph"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// EscalationMachine tracks escalation tiers, counts retries per tier,
// validates split actions, and mutates the DAG when stories are split.
type EscalationMachine struct {
	eventStore state.EventStore
	routing    config.RoutingConfig
}

// NewEscalationMachine creates an EscalationMachine backed by the given
// event store and routing configuration.
func NewEscalationMachine(es state.EventStore, routing config.RoutingConfig) *EscalationMachine {
	return &EscalationMachine{eventStore: es, routing: routing}
}

// CurrentTier returns the highest to_tier from STORY_ESCALATED events for
// the given story. Returns 0 if no escalation events exist.
func (e *EscalationMachine) CurrentTier(storyID string) (int, error) {
	events, err := e.eventStore.List(state.EventFilter{
		Type:    state.EventStoryEscalated,
		StoryID: storyID,
	})
	if err != nil {
		return 0, err
	}

	maxTier := 0
	for _, evt := range events {
		payload := state.DecodePayload(evt.Payload)
		if toTier, ok := payload["to_tier"].(float64); ok && int(toTier) > maxTier {
			maxTier = int(toTier)
		}
	}
	return maxTier, nil
}

// lastEscalationTime returns the timestamp of the most recent
// STORY_ESCALATED event for the given story. Returns the zero time if
// no escalation events exist.
func (e *EscalationMachine) lastEscalationTime(storyID string) (time.Time, error) {
	events, err := e.eventStore.List(state.EventFilter{
		Type:    state.EventStoryEscalated,
		StoryID: storyID,
	})
	if err != nil {
		return time.Time{}, err
	}
	if len(events) == 0 {
		return time.Time{}, nil
	}

	latest := events[0].Timestamp
	for _, evt := range events[1:] {
		if evt.Timestamp.After(latest) {
			latest = evt.Timestamp
		}
	}
	return latest, nil
}

// RetryCountAtCurrentTier counts failure events (STORY_REVIEW_FAILED and
// STORY_QA_FAILED) that occurred after the most recent escalation. This
// scopes retry counts to the current tier so that failures from prior
// tiers are not double-counted.
func (e *EscalationMachine) RetryCountAtCurrentTier(storyID string) (int, error) {
	after, err := e.lastEscalationTime(storyID)
	if err != nil {
		return 0, fmt.Errorf("last escalation time: %w", err)
	}
	reviewCount, err := e.eventStore.Count(state.EventFilter{
		Type:    state.EventStoryReviewFailed,
		StoryID: storyID,
		After:   after,
	})
	if err != nil {
		return 0, err
	}
	qaCount, err := e.eventStore.Count(state.EventFilter{
		Type:    state.EventStoryQAFailed,
		StoryID: storyID,
		After:   after,
	})
	if err != nil {
		return 0, err
	}
	return reviewCount + qaCount, nil
}

// MaxRetriesForTier returns the retry limit for a given escalation tier.
//
//	Tier 0: config.MaxRetriesBeforeEscalation (same-role retry)
//	Tier 1: config.MaxSeniorRetries (senior retry)
//	Tier 2: config.MaxManagerAttempts (manager diagnosis)
//	Tier 3: 1 (tech_lead re-plan, single attempt)
//	Tier 4+: 0 (pause / human fallback)
func (e *EscalationMachine) MaxRetriesForTier(tier int) int {
	switch tier {
	case 0:
		return e.routing.MaxRetriesBeforeEscalation
	case 1:
		return e.routing.MaxSeniorRetries
	case 2:
		return e.routing.MaxManagerAttempts
	case 3:
		return 1
	default:
		return 0
	}
}

// ShouldEscalate returns whether the story should escalate to the next
// tier based on retry count vs. the tier limit. It also returns the
// target tier. When the current tier's retry limit is reached or
// exceeded, escalation is triggered.
func (e *EscalationMachine) ShouldEscalate(storyID string) (bool, int, error) {
	tier, err := e.CurrentTier(storyID)
	if err != nil {
		return false, 0, err
	}

	count, err := e.RetryCountAtCurrentTier(storyID)
	if err != nil {
		return false, 0, err
	}

	maxRetries := e.MaxRetriesForTier(tier)
	if count >= maxRetries {
		return true, tier + 1, nil
	}
	return false, tier, nil
}

// SplitChild holds data for one child story produced by a split action.
type SplitChild struct {
	ID                 string
	Suffix             string
	Title              string
	Description        string
	AcceptanceCriteria string
	Complexity         int
	OwnedFiles         []string
}

// maxSplitDepth is the maximum nesting depth for story splits.
const maxSplitDepth = 2

// ValidateSplit checks constraints on a proposed split action:
//   - The parent must not exceed the maximum split depth.
//   - No two children may share the same suffix (duplicate suffixes would
//     produce duplicate story IDs when concatenated with the parent ID).
//   - No two children may claim the same owned file.
//   - No child may exceed the given maximum complexity.
//
// It delegates to ValidateSplitWithEdges with an empty edge list.
func (e *EscalationMachine) ValidateSplit(parentSplitDepth int, children []SplitChild, maxComplexity int) error {
	return e.ValidateSplitWithEdges(parentSplitDepth, children, maxComplexity, nil)
}

// ValidateSplitWithEdges extends ValidateSplit with dependency-edge
// validation. In addition to the constraints checked by ValidateSplit it
// verifies:
//   - Every dependency edge must contain exactly 2 elements.
//   - Both endpoints of every edge must reference suffixes that exist among
//     the children (no dangling edges that an LLM could hallucinate).
func (e *EscalationMachine) ValidateSplitWithEdges(parentSplitDepth int, children []SplitChild, maxComplexity int, edges [][]string) error {
	if parentSplitDepth >= maxSplitDepth {
		return fmt.Errorf("max split depth (%d) reached", maxSplitDepth)
	}

	// Build suffix set first so it can be reused for both duplicate detection
	// and edge validation.
	suffixSet := make(map[string]bool, len(children))
	for _, child := range children {
		// The suffix is LLM-generated (Manager/Tech-Lead split output) and is
		// concatenated into the child story ID, which later flows into
		// filepath.Join(stateDir, "worktrees", storyID). A suffix containing
		// "/", "..", spaces, or shell metacharacters would escape the worktree
		// root. DispatchWave validates story IDs, but the child story is created
		// in the event store BEFORE dispatch — so split creation is the correct
		// defence-in-depth boundary.
		if !safeStoryIDPattern.MatchString(child.Suffix) {
			return fmt.Errorf("child suffix %q contains unsafe characters (allowed: alphanumeric . _ -)", child.Suffix)
		}
		if suffixSet[child.Suffix] {
			return fmt.Errorf("duplicate child suffix: %q", child.Suffix)
		}
		suffixSet[child.Suffix] = true
	}

	// Validate edges structurally and build a directed dependency graph.
	// edge = [after, before]: "after" depends on (runs strictly after) "before".
	adj := make(map[string][]string, len(edges))
	for _, edge := range edges {
		if len(edge) != 2 {
			return fmt.Errorf("dependency edge must have exactly 2 elements, got %d", len(edge))
		}
		if !suffixSet[edge[0]] || !suffixSet[edge[1]] {
			return fmt.Errorf("dependency edge references unknown suffix: %v", edge)
		}
		adj[edge[0]] = append(adj[edge[0]], edge[1])
	}

	// orderedByEdges reports whether two child suffixes are sequenced relative
	// to each other — one transitively depends on the other, so they never run
	// in parallel. Overlapping owned files are a conflict only between PARALLEL
	// children; sequential children touching the same file is safe. The
	// tech-lead split path chains all children, making every pair ordered — so
	// rejecting their shared files hard-paused real builds for no reason.
	reaches := func(from, to string) bool {
		seen := map[string]bool{from: true}
		stack := []string{from}
		for len(stack) > 0 {
			n := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			for _, m := range adj[n] {
				if m == to {
					return true
				}
				if !seen[m] {
					seen[m] = true
					stack = append(stack, m)
				}
			}
		}
		return false
	}
	orderedByEdges := func(x, y string) bool {
		return x == y || reaches(x, y) || reaches(y, x)
	}

	// Edge-aware overlap check: a file may be claimed by multiple children only
	// if every pair of claimants is ordered (sequential), never parallel.
	fileOwners := make(map[string][]string)
	for _, child := range children {
		for _, f := range child.OwnedFiles {
			for _, prev := range fileOwners[f] {
				if !orderedByEdges(child.Suffix, prev) {
					return fmt.Errorf("overlapping owned file: %s", f)
				}
			}
			fileOwners[f] = append(fileOwners[f], child.Suffix)
		}
		if child.Complexity > maxComplexity {
			return fmt.Errorf("child complexity %d exceeds max %d", child.Complexity, maxComplexity)
		}
	}

	return nil
}

// ApplySplit mutates the DAG and RunContext for a split action. Each child
// node is added to the DAG with edges to the parent's original
// dependencies, and any nodes that depended on the parent now depend on
// all children. The caller must hold any applicable DAG mutex.
func (e *EscalationMachine) ApplySplit(
	dag *graph.DAG,
	rc *RunContext,
	parentID string,
	children []SplitChild,
	depEdges [][]string,
	parentDeps []string,
	dependents []string,
) {
	for _, child := range children {
		dag.AddNode(child.ID)

		// Inherit parent's original dependencies.
		for _, dep := range parentDeps {
			dag.AddEdge(child.ID, dep)
		}

		// Add the child to the planned stories list.
		rc.PlannedStories = append(rc.PlannedStories, PlannedStory{
			ID:                 child.ID,
			Title:              child.Title,
			Description:        child.Description,
			AcceptanceCriteria: FlexibleString(child.AcceptanceCriteria),
			Complexity:         child.Complexity,
			OwnedFiles:         child.OwnedFiles,
		})
	}

	// Add explicit inter-child dependency edges.
	for _, edge := range depEdges {
		if len(edge) == 2 {
			dag.AddEdge(edge[0], edge[1])
		}
	}

	// Anything that depended on the parent now depends on all children.
	for _, depID := range dependents {
		for _, child := range children {
			dag.AddEdge(depID, child.ID)
		}
	}
}
