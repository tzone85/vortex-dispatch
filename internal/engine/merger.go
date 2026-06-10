package engine

import (
	"fmt"
	"strings"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// GitHubOps abstracts GitHub operations for testability.
type GitHubOps interface {
	PushBranch(repoDir, branch string) error
	CreatePR(repoDir, title, body, baseBranch, headBranch string) (PRCreationResult, error)
	MergePR(repoDir string, prNumber int) error
}

// PRCreationResult holds the output of PR creation.
type PRCreationResult struct {
	Number int
	URL    string
}

// MergeResult holds the outcome of the merge pipeline for a story.
type MergeResult struct {
	PRNumber int
	PRURL    string
	Merged   bool
}

// Merger handles pushing branches, creating PRs, and optionally auto-merging
// completed stories.
type Merger struct {
	config     config.MergeConfig
	ghOps      GitHubOps
	eventStore state.EventStore
	projStore  state.ProjectionStore
}

// NewMerger creates a Merger wired to the given configuration, GitHub
// operations, event store, and projection store.
func NewMerger(cfg config.MergeConfig, ghOps GitHubOps, es state.EventStore, ps state.ProjectionStore) *Merger {
	return &Merger{
		config:     cfg,
		ghOps:      ghOps,
		eventStore: es,
		projStore:  ps,
	}
}

// Merge pushes a branch, creates a PR, and optionally auto-merges it.
// It emits STORY_PR_CREATED and (if auto-merge is on) STORY_MERGED events.
func (m *Merger) Merge(storyID, storyTitle, repoDir, branch string) (MergeResult, error) {
	// Push branch
	if err := m.ghOps.PushBranch(repoDir, branch); err != nil {
		return MergeResult{}, fmt.Errorf("push branch %s: %w", branch, err)
	}

	// Create PR using the configured template when available.
	prTitle := fmt.Sprintf("[VXD] %s", storyTitle)
	prBody := m.buildPRBody(storyID, storyTitle)

	pr, err := m.ghOps.CreatePR(repoDir, prTitle, prBody, m.config.BaseBranch, branch)
	if err != nil {
		return MergeResult{}, fmt.Errorf("create PR for %s: %w", storyID, err)
	}

	// Emit PR created event
	prEvt := state.NewEvent(state.EventStoryPRCreated, "merger", storyID, map[string]any{
		"pr_number": pr.Number,
		"pr_url":    pr.URL,
		"branch":    branch,
	})
	if err := m.eventStore.Append(prEvt); err != nil {
		return MergeResult{}, fmt.Errorf("emit pr created: %w", err)
	}
	if err := m.projStore.Project(prEvt); err != nil {
		return MergeResult{}, fmt.Errorf("project pr created: %w", err)
	}

	result := MergeResult{
		PRNumber: pr.Number,
		PRURL:    pr.URL,
		Merged:   false,
	}

	// Auto-merge if configured. A zero PR number means CreatePR did not return
	// a usable PR — surface that as an error rather than silently reporting a
	// non-merge as success, which would let dependents dispatch against work
	// that never merged.
	if m.config.AutoMerge {
		if pr.Number == 0 {
			return result, fmt.Errorf("auto-merge requested but no PR number was returned for story %s", storyID)
		}
		if err := m.ghOps.MergePR(repoDir, pr.Number); err != nil {
			return result, fmt.Errorf("auto-merge PR #%d: %w", pr.Number, err)
		}

		mergeEvt := state.NewEvent(state.EventStoryMerged, "merger", storyID, map[string]any{
			"pr_number": pr.Number,
			"branch":    branch,
		})
		if err := m.eventStore.Append(mergeEvt); err != nil {
			return result, fmt.Errorf("emit merged: %w", err)
		}
		if err := m.projStore.Project(mergeEvt); err != nil {
			return result, fmt.Errorf("project merged: %w", err)
		}

		result.Merged = true
	}

	return result, nil
}

// CreatePROnly pushes a branch and creates a PR without auto-merging,
// regardless of the AutoMerge config. This is used when the review gate
// is in "manual" mode — the PR waits for human approval.
func (m *Merger) CreatePROnly(storyID, storyTitle, repoDir, branch string) (MergeResult, error) {
	if err := m.ghOps.PushBranch(repoDir, branch); err != nil {
		return MergeResult{}, fmt.Errorf("push branch %s: %w", branch, err)
	}

	prTitle := fmt.Sprintf("[VXD] %s", storyTitle)
	prBody := m.buildPRBody(storyID, storyTitle)

	pr, err := m.ghOps.CreatePR(repoDir, prTitle, prBody, m.config.BaseBranch, branch)
	if err != nil {
		return MergeResult{}, fmt.Errorf("create PR for %s: %w", storyID, err)
	}

	evt := state.NewEvent(state.EventStoryPRCreated, "merger", storyID, map[string]any{
		"pr_number": pr.Number,
		"pr_url":    pr.URL,
		"branch":    branch,
	})
	if err := m.eventStore.Append(evt); err != nil {
		return MergeResult{}, fmt.Errorf("emit pr created: %w", err)
	}
	if err := m.projStore.Project(evt); err != nil {
		return MergeResult{}, fmt.Errorf("project pr created: %w", err)
	}

	return MergeResult{PRNumber: pr.Number, PRURL: pr.URL, Merged: false}, nil
}

// MergeExistingPR merges an already-created PR for a story. This is called
// by the "vxd approve" command after a human reviews and approves the PR.
func (m *Merger) MergeExistingPR(storyID, repoDir string) error {
	if m.ghOps == nil {
		return fmt.Errorf("github operations are not configured")
	}
	story, err := m.projStore.GetStory(storyID)
	if err != nil {
		return fmt.Errorf("get story %s: %w", storyID, err)
	}
	if story.PRNumber == 0 {
		return fmt.Errorf("story %s has no PR", storyID)
	}

	if err := m.ghOps.MergePR(repoDir, story.PRNumber); err != nil {
		return fmt.Errorf("merge PR #%d: %w", story.PRNumber, err)
	}

	evt := state.NewEvent(state.EventStoryMerged, "merger", storyID, map[string]any{
		"pr_number": story.PRNumber,
		"pr_url":    story.PRUrl,
	})
	if err := m.eventStore.Append(evt); err != nil {
		return fmt.Errorf("emit merged: %w", err)
	}
	if err := m.projStore.Project(evt); err != nil {
		return fmt.Errorf("project merged: %w", err)
	}

	return nil
}

// buildPRBody interpolates the configured PR template with story data.
// Falls back to a simple default if no template is configured.
func (m *Merger) buildPRBody(storyID, storyTitle string) string {
	tmpl := m.config.PRTemplate
	if tmpl == "" {
		return fmt.Sprintf("Automated PR for story %s\n\n%s", storyID, storyTitle)
	}

	description := storyTitle
	ac := ""

	// Look up richer story data from projection store if available.
	if m.projStore != nil {
		if story, err := m.projStore.GetStory(storyID); err == nil {
			if story.Description != "" {
				description = story.Description
			}
			ac = story.AcceptanceCriteria
		}
	}

	r := strings.NewReplacer(
		"{story_id}", storyID,
		"{description}", description,
		"{acceptance_criteria}", ac,
	)
	return r.Replace(tmpl)
}
