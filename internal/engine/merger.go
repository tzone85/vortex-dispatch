package engine

import (
	"fmt"

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

	// Create PR
	prTitle := fmt.Sprintf("[VXD] %s", storyTitle)
	prBody := fmt.Sprintf("Automated PR for story %s\n\n%s", storyID, storyTitle)

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

	// Auto-merge if configured
	if m.config.AutoMerge && pr.Number > 0 {
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
