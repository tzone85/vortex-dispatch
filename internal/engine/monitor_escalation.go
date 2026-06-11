package engine

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/tzone85/vortex-dispatch/internal/llm"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

func (m *Monitor) handleManagerEscalation(ctx context.Context, story PlannedStory, repoDir string, rc *RunContext) {
	storyID := story.ID
	stateDir := execExpandHome(m.config.Workspace.StateDir)
	worktreePath := filepath.Join(stateDir, "worktrees", storyID)
	logDir := filepath.Join(stateDir, "logs")

	dc, err := m.manager.BuildDiagnosticContext(storyID, worktreePath, logDir)
	if err != nil {
		log.Printf("[manager] context build error for %s: %v", storyID, err)
		m.resetStoryToDraft(storyID, "manager", fmt.Sprintf("context build error: %v", err))
		return
	}

	action, err := m.manager.Diagnose(ctx, dc)
	if err != nil {
		log.Printf("[manager] diagnosis failed for %s: %v", storyID, err)
		if llm.IsFatalAPIError(err) {
			m.pauseRequirement(storyID, fmt.Sprintf("fatal API error in manager: %v", err))
			return
		}
		m.resetStoryToDraft(storyID, "manager", fmt.Sprintf("diagnosis error: %v", err))
		return
	}

	log.Printf("[manager] %s: diagnosis=%q action=%s", storyID, action.Diagnosis, action.Action)

	// Persist the diagnosis for post-mortem review. A write failure here
	// shouldn't block the escalation flow (the diagnosis is also in the
	// log via the line above), but operators want to know if the file
	// path is broken (read-only mount, no logDir, etc.).
	logPath := filepath.Join(logDir, storyID+"-manager.log")
	if err := os.WriteFile(logPath, fmt.Appendf(nil, "Diagnosis: %s\nCategory: %s\nAction: %s\n",
		action.Diagnosis, action.Category, action.Action), 0o644); err != nil {
		log.Printf("[manager] persist diagnosis for %s to %s: %v", storyID, logPath, err)
	}

	switch action.Action {
	case "retry":
		m.executeRetryAction(storyID, action, worktreePath)
	case "rewrite":
		m.executeRewriteAction(storyID, action)
	case "split":
		m.executeSplitAction(ctx, storyID, action, rc, story)
	case "escalate_to_techlead":
		m.escalateToTier(storyID, 3, "manager escalated: "+action.Diagnosis)
	default:
		m.resetStoryToDraft(storyID, "manager", "unknown action: "+action.Action)
	}
}

// executeRetryAction resets a story to a lower tier for re-dispatch,
// optionally removing the worktree for a clean start.
func (m *Monitor) executeRetryAction(storyID string, action ManagerAction, worktreePath string) {
	if action.RetryConfig != nil && action.RetryConfig.WorktreeReset {
		if err := os.RemoveAll(worktreePath); err != nil {
			log.Printf("[manager] worktree-reset cleanup for %s: %v", storyID, err)
		}
	}

	resetTier := 0
	if action.RetryConfig != nil {
		resetTier = action.RetryConfig.ResetTier
	}

	evt := state.NewEvent(state.EventStoryEscalated, "manager", storyID, map[string]any{
		"from_tier": 2,
		"to_tier":   resetTier,
		"reason":    "manager retry: " + action.Diagnosis,
	})
	if err := m.eventStore.Append(evt); err != nil {
		log.Printf("[manager] append retry-escalation event for %s: %v", storyID, err)
	}
	if err := m.projStore.Project(evt); err != nil {
		log.Printf("[manager] project retry-escalation event for %s: %v", storyID, err)
	}

	resetEvt := state.NewEvent(state.EventStoryReviewFailed, "manager", storyID, map[string]any{
		"reason": "manager retry with fixes",
	})
	if err := m.eventStore.Append(resetEvt); err != nil {
		log.Printf("[manager] append retry-reset event for %s: %v", storyID, err)
	}
	if err := m.projStore.Project(resetEvt); err != nil {
		log.Printf("[manager] project retry-reset event for %s: %v", storyID, err)
	}
}

// executeRewriteAction emits a STORY_REWRITTEN event to update the story
// definition with the Manager's revised title, description, acceptance
// criteria, and/or complexity.
func (m *Monitor) executeRewriteAction(storyID string, action ManagerAction) {
	if action.RewriteConfig == nil {
		m.resetStoryToDraft(storyID, "manager", "rewrite action with no config")
		return
	}

	changes := map[string]any{}
	if action.RewriteConfig.Title != "" {
		changes["title"] = action.RewriteConfig.Title
	}
	if action.RewriteConfig.Description != "" {
		changes["description"] = action.RewriteConfig.Description
	}
	if action.RewriteConfig.AcceptanceCriteria != "" {
		changes["acceptance_criteria"] = action.RewriteConfig.AcceptanceCriteria
	}
	if action.RewriteConfig.Complexity > 0 {
		changes["complexity"] = action.RewriteConfig.Complexity
	}

	evt := state.NewEvent(state.EventStoryRewritten, "manager", storyID, map[string]any{
		"changes": changes,
		"reason":  action.Diagnosis,
	})
	if err := m.eventStore.Append(evt); err != nil {
		log.Printf("[manager] append rewrite event for %s: %v", storyID, err)
	}
	if err := m.projStore.Project(evt); err != nil {
		log.Printf("[manager] project rewrite event for %s: %v", storyID, err)
	}
}

// executeSplitAction validates and applies a split, creating child stories
// in the event store and mutating the DAG.
func (m *Monitor) executeSplitAction(ctx context.Context, storyID string, action ManagerAction, rc *RunContext, story PlannedStory) {
	if action.SplitConfig == nil || len(action.SplitConfig.Children) == 0 {
		m.resetStoryToDraft(storyID, "manager", "split with no children")
		return
	}

	storyData, err := m.projStore.GetStory(storyID)
	if err != nil {
		m.resetStoryToDraft(storyID, "manager", fmt.Sprintf("cannot look up story for split: %v", err))
		return
	}

	children := make([]SplitChild, 0, len(action.SplitConfig.Children))
	for _, c := range action.SplitConfig.Children {
		children = append(children, SplitChild{
			ID:                 storyID + "-" + c.Suffix,
			Suffix:             c.Suffix,
			Title:              c.Title,
			Description:        c.Description,
			AcceptanceCriteria: c.AcceptanceCriteria,
			Complexity:         c.Complexity,
			OwnedFiles:         c.OwnedFiles,
		})
	}

	if err := m.escalation.ValidateSplit(storyData.SplitDepth, children, m.config.Planning.MaxStoryComplexity); err != nil {
		log.Printf("[manager] split validation failed for %s: %v", storyID, err)
		m.resetStoryToDraft(storyID, "manager", fmt.Sprintf("invalid split: %v", err))
		return
	}

	m.dagMu.Lock()
	defer m.dagMu.Unlock()

	// Create child stories in the event store.
	for _, child := range children {
		childEvt := state.NewEvent(state.EventStoryCreated, "manager", child.ID, map[string]any{
			"id":                  child.ID,
			"req_id":              rc.ReqID,
			"title":               child.Title,
			"description":         child.Description,
			"acceptance_criteria": child.AcceptanceCriteria,
			"complexity":          child.Complexity,
			"owned_files":         child.OwnedFiles,
			"split_depth":         storyData.SplitDepth + 1,
		})
		if err := m.eventStore.Append(childEvt); err != nil {
			log.Printf("[manager] append child-created event for %s: %v", child.ID, err)
		}
		if err := m.projStore.Project(childEvt); err != nil {
			log.Printf("[manager] project child-created event for %s: %v", child.ID, err)
		}
	}

	// Emit STORY_SPLIT for the parent.
	childIDs := make([]string, len(children))
	for i, c := range children {
		childIDs[i] = c.ID
	}
	splitEvt := state.NewEvent(state.EventStorySplit, "manager", storyID, map[string]any{
		"child_story_ids": childIDs,
		"reason":          action.Diagnosis,
	})
	if err := m.eventStore.Append(splitEvt); err != nil {
		log.Printf("[manager] append split event for %s: %v", storyID, err)
	}
	if err := m.projStore.Project(splitEvt); err != nil {
		log.Printf("[manager] project split event for %s: %v", storyID, err)
	}

	// Mutate the DAG to replace the parent with children.
	m.escalation.ApplySplit(
		rc.DAG, rc, storyID, children,
		action.SplitConfig.DependencyEdges,
		story.DependsOn,
		FindDependents(rc.PlannedStories, storyID),
	)
}

// escalateToTier emits a STORY_ESCALATED event moving the story to the
// specified tier.
func (m *Monitor) escalateToTier(storyID string, tier int, reason string) {
	currentTier, _ := m.escalation.CurrentTier(storyID)
	evt := state.NewEvent(state.EventStoryEscalated, "monitor", storyID, map[string]any{
		"from_tier": currentTier,
		"to_tier":   tier,
		"reason":    reason,
	})
	if err := m.eventStore.Append(evt); err != nil {
		log.Printf("[monitor] append tier-escalation event for %s: %v", storyID, err)
	}
	if err := m.projStore.Project(evt); err != nil {
		log.Printf("[monitor] project tier-escalation event for %s: %v", storyID, err)
	}
}

// handleTechLeadEscalation handles tier-3 stories by calling the Planner's
// RePlan method to decompose the failing story into smaller replacements,
// then emitting STORY_SPLIT and mutating the DAG via ApplySplit.
func (m *Monitor) handleTechLeadEscalation(ctx context.Context, story PlannedStory, repoDir string, rc *RunContext) {
	storyID := story.ID
	stateDir := execExpandHome(m.config.Workspace.StateDir)
	logDir := filepath.Join(stateDir, "logs")

	// Build failure context from events and logs.
	events, _ := m.eventStore.List(state.EventFilter{StoryID: storyID})
	var failureContext strings.Builder
	for _, evt := range events {
		fmt.Fprintf(&failureContext, "%s %s (agent: %s)\n", evt.Type, evt.Timestamp.Format("15:04:05"), evt.AgentID)
	}
	logPath := filepath.Join(logDir, storyID+".log")
	if data, err := os.ReadFile(logPath); err == nil {
		failureContext.WriteString("\nAgent log:\n")
		failureContext.Write(data)
	}

	// Check if planner is available.
	if m.planner == nil {
		log.Printf("[tech-lead] no planner available for %s, pausing", storyID)
		m.pauseRequirement(storyID, "tech lead escalation: no planner configured")
		return
	}

	// Call RePlan to get replacement stories.
	replacements, err := m.planner.RePlan(ctx, storyID, rc.ReqID, failureContext.String())
	if err != nil {
		log.Printf("[tech-lead] re-plan failed for %s: %v", storyID, err)
		m.pauseRequirement(storyID, fmt.Sprintf("tech lead re-plan failed: %v", err))
		return
	}

	if len(replacements) == 0 {
		log.Printf("[tech-lead] re-plan produced no stories for %s", storyID)
		m.pauseRequirement(storyID, "tech lead re-plan produced no replacement stories")
		return
	}

	// Build SplitChild list from replacements.
	// Derive Suffix from child ID: if the child ID starts with the parent ID,
	// the suffix is everything after the parent prefix + "-". Otherwise use
	// the full child ID as the suffix (guarantees uniqueness).
	storyData, _ := m.projStore.GetStory(storyID)
	children := make([]SplitChild, 0, len(replacements))
	for _, r := range replacements {
		suffix := r.ID
		if strings.HasPrefix(r.ID, storyID+"-") {
			suffix = r.ID[len(storyID)+1:]
		}
		children = append(children, SplitChild{
			ID:                 r.ID,
			Suffix:             suffix,
			Title:              r.Title,
			Description:        r.Description,
			AcceptanceCriteria: string(r.AcceptanceCriteria),
			Complexity:         r.Complexity,
			OwnedFiles:         r.OwnedFiles,
		})
	}

	// Validate split constraints before mutating.
	if err := m.escalation.ValidateSplit(storyData.SplitDepth, children, m.config.Planning.MaxStoryComplexity); err != nil {
		log.Printf("[tech-lead] split validation failed for %s: %v", storyID, err)
		m.pauseRequirement(storyID, fmt.Sprintf("tech lead split invalid: %v", err))
		return
	}

	// Emit STORY_SPLIT + mutate DAG (same pattern as executeSplitAction).
	m.dagMu.Lock()
	defer m.dagMu.Unlock()

	// Create child stories in the event store (with split_depth).
	for _, child := range children {
		childEvt := state.NewEvent(state.EventStoryCreated, "tech_lead", child.ID, map[string]any{
			"id":                  child.ID,
			"req_id":              rc.ReqID,
			"title":               child.Title,
			"description":         child.Description,
			"acceptance_criteria": child.AcceptanceCriteria,
			"complexity":          child.Complexity,
			"owned_files":         child.OwnedFiles,
			"split_depth":         storyData.SplitDepth + 1,
		})
		if err := m.eventStore.Append(childEvt); err != nil {
			log.Printf("[tech-lead] append child-created event for %s: %v", child.ID, err)
		}
		if err := m.projStore.Project(childEvt); err != nil {
			log.Printf("[tech-lead] project child-created event for %s: %v", child.ID, err)
		}
	}

	childIDs := make([]string, len(children))
	for i, c := range children {
		childIDs[i] = c.ID
	}
	splitEvt := state.NewEvent(state.EventStorySplit, "tech_lead", storyID, map[string]any{
		"child_story_ids": childIDs,
		"reason":          "tech lead re-plan",
	})
	if err := m.eventStore.Append(splitEvt); err != nil {
		log.Printf("[tech-lead] append split event for %s: %v", storyID, err)
	}
	if err := m.projStore.Project(splitEvt); err != nil {
		log.Printf("[tech-lead] project split event for %s: %v", storyID, err)
	}

	// Build sequential dependency edges for re-planned stories.
	var depEdges [][]string
	for i := 1; i < len(children); i++ {
		depEdges = append(depEdges, []string{children[i].ID, children[i-1].ID})
	}

	m.escalation.ApplySplit(rc.DAG, rc, storyID, children, depEdges,
		story.DependsOn, FindDependents(rc.PlannedStories, storyID))

	log.Printf("[tech-lead] re-planned %s into %d replacement stories", storyID, len(children))
}

// FindDependents returns the IDs of stories that depend on the given storyID.
