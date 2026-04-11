package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tzone85/vortex-dispatch/internal/agent"
	"github.com/tzone85/vortex-dispatch/internal/artifact"
	"github.com/tzone85/vortex-dispatch/internal/config"
	vxdgit "github.com/tzone85/vortex-dispatch/internal/git"
	"github.com/tzone85/vortex-dispatch/internal/runtime"
	"github.com/tzone85/vortex-dispatch/internal/scratchboard"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// ActiveAgent tracks a running agent session for the monitor.
type ActiveAgent struct {
	Assignment   Assignment
	WorktreePath string
	RuntimeName  string
}

// Executor spawns agents for dispatched assignments by creating git worktrees,
// launching tmux sessions with configured runtimes, and emitting lifecycle events.
type Executor struct {
	registry      *runtime.Registry
	config        config.Config
	eventStore    state.EventStore
	projStore     state.ProjectionStore
	artifactStore *artifact.Store
	scratchboard  *scratchboard.Scratchboard
}

// NewExecutor creates an Executor wired to the runtime registry, configuration,
// event store, and projection store.
func NewExecutor(reg *runtime.Registry, cfg config.Config, es state.EventStore, ps state.ProjectionStore) *Executor {
	return &Executor{
		registry:   reg,
		config:     cfg,
		eventStore: es,
		projStore:  ps,
	}
}

// SetArtifactStore sets the artifact store for persisting per-story artifacts.
func (e *Executor) SetArtifactStore(store *artifact.Store) {
	e.artifactStore = store
}

// SetScratchboard sets the shared scratchboard for cross-agent knowledge.
func (e *Executor) SetScratchboard(sb *scratchboard.Scratchboard) {
	e.scratchboard = sb
}

// SpawnResult holds the outcome of spawning an agent for one assignment.
type SpawnResult struct {
	Assignment   Assignment
	WorktreePath string
	RuntimeName  string
	Error        error
}

// SpawnAll creates worktrees and launches tmux sessions for each assignment.
func (e *Executor) SpawnAll(repoDir string, assignments []Assignment, stories map[string]PlannedStory) []SpawnResult {
	results := make([]SpawnResult, 0, len(assignments))
	for _, a := range assignments {
		result := e.spawn(repoDir, a, stories[a.StoryID])
		results = append(results, result)
	}
	return results
}

func (e *Executor) spawn(repoDir string, a Assignment, story PlannedStory) SpawnResult {
	result := SpawnResult{Assignment: a}

	// Determine worktree path
	worktreeBase := filepath.Join(execExpandHome(e.config.Workspace.StateDir), "worktrees")
	worktreePath := filepath.Join(worktreeBase, a.StoryID)
	result.WorktreePath = worktreePath

	// Create worktree with branch
	if err := vxdgit.CreateWorktree(repoDir, worktreePath, a.Branch); err != nil {
		result.Error = fmt.Errorf("create worktree for %s: %w", a.StoryID, err)
		return result
	}

	// Note: CLAUDE.md is written by the runtime's Spawn() method (see
	// registry.go) on every spawn so that reused worktrees always get
	// fresh content. We do NOT write it here to avoid a duplicate write
	// with conflicting content.

	// Resolve runtime for this role
	rtName := e.runtimeForRole(a.Role)
	result.RuntimeName = rtName

	rt, err := e.registry.Get(rtName)
	if err != nil {
		result.Error = fmt.Errorf("get runtime %s: %w", rtName, err)
		return result
	}

	// Build the agent prompt context.
	// If there's review/QA feedback from a previous attempt, enhance it with
	// smart retry analysis (error categorization + fix suggestions).
	feedback := e.latestReviewFeedback(a.StoryID)
	if feedback != "" {
		smartContext := BuildSmartRetryContext(feedback)
		if smartContext != "" {
			feedback = smartContext
		}
	}

	waveContext := ReadWaveContext(worktreePath)

	// Inject scratchboard snapshot into wave context so parallel agents
	// share discoveries without a native runtime tool integration.
	if e.scratchboard != nil {
		snap := e.scratchboard.Snapshot(10)
		if snap != "" {
			waveContext = waveContext + "\n" + snap
		}
	}

	isExisting := detectExistingCodebase(worktreePath)
	isBug := detectBugFix(story.Title, story.Description)
	isInfra := detectInfrastructure(story.Title, story.Description)

	promptCtx := agent.PromptContext{
		StoryID:            a.StoryID,
		StoryTitle:         story.Title,
		StoryDescription:   story.Description,
		AcceptanceCriteria: string(story.AcceptanceCriteria),
		RepoPath:           worktreePath,
		Complexity:         story.Complexity,
		ReviewFeedback:     feedback,
		WaveContext:        waveContext,
		IsExistingCodebase: isExisting,
		IsBugFix:           isBug,
		IsInfrastructure:   isInfra,
	}

	// If this is a retry (feedback exists from a prior attempt), enhance
	// the goal prompt with attempt history so the agent learns from failures.
	var goalPrompt string
	if feedback != "" {
		tracker := NewAttemptTracker(e.eventStore)
		attempts, _ := tracker.ListAttempts(a.StoryID)

		priorAttempts := make([]agent.AttemptSummary, 0, len(attempts))
		for _, att := range attempts {
			priorAttempts = append(priorAttempts, agent.AttemptSummary{
				Number:  att.Number,
				Role:    att.Role,
				Outcome: att.Outcome,
				Error:   att.Error,
			})
		}

		tmplCtx := agent.TemplateContext{
			StoryID:            a.StoryID,
			StoryTitle:         story.Title,
			StoryDescription:   story.Description,
			AcceptanceCriteria: string(story.AcceptanceCriteria),
			Complexity:         story.Complexity,
			RepoPath:           worktreePath,
			TechStack:          promptCtx.TechStack,
			LintCommand:        promptCtx.LintCommand,
			BuildCommand:       promptCtx.BuildCommand,
			TestCommand:        promptCtx.TestCommand,
			ReviewFeedback:     feedback,
			WaveContext:        waveContext,
			IsExistingCodebase: isExisting,
			IsBugFix:           isBug,
			IsInfrastructure:   isInfra,
			IsRetry:            true,
			RetryNumber:        len(attempts) + 1,
			PriorAttempts:      priorAttempts,
		}
		goalPrompt = agent.RenderGoalWithAttempts(tmplCtx)
	} else {
		goalPrompt = agent.GoalPrompt(a.Role, promptCtx)
	}

	// Resolve model for this role
	modelCfg := a.Role.ModelConfig(e.config.Models)

	// Build log path for post-mortem diagnosis
	logDir := filepath.Join(execExpandHome(e.config.Workspace.StateDir), "logs")
	os.MkdirAll(logDir, 0o755)
	logFile := filepath.Join(logDir, a.StoryID+".log")

	// Spawn the runtime session
	if err := rt.Spawn(runtime.SessionConfig{
		SessionName:  a.SessionName,
		WorkDir:      worktreePath,
		Model:        modelCfg.Model,
		Goal:         goalPrompt,
		SystemPrompt: agent.SystemPrompt(a.Role, promptCtx),
		LogFile:      logFile,
	}); err != nil {
		result.Error = fmt.Errorf("spawn runtime for %s: %w", a.StoryID, err)
		return result
	}

	// Write launch config artifact for reproducibility.
	if e.artifactStore != nil {
		e.artifactStore.Write(a.StoryID, artifact.TypeLaunchConfig, artifact.LaunchConfig{
			StoryID:   a.StoryID,
			Runtime:   rtName,
			Model:     modelCfg.Model,
			Prompt:    goalPrompt,
			WaveBrief: waveContext,
		})
	}

	// Emit STORY_STARTED event with tier and role so AttemptTracker can
	// reconstruct attempt history without reverse-engineering roles.
	startEvt := state.NewEvent(state.EventStoryStarted, a.AgentID, a.StoryID, map[string]any{
		"worktree_path": worktreePath,
		"runtime":       rtName,
		"session_name":  a.SessionName,
		"branch":        a.Branch,
		"tier":          tierForRole(a.Role),
		"role":          string(a.Role),
	})
	if err := e.eventStore.Append(startEvt); err != nil {
		result.Error = fmt.Errorf("emit story started: %w", err)
		return result
	}
	if err := e.projStore.Project(startEvt); err != nil {
		result.Error = fmt.Errorf("project story started: %w", err)
		return result
	}

	return result
}

// latestReviewFeedback queries the event store for the most recent
// STORY_REVIEW_FAILED event (emitted by "monitor") for the given story
// and extracts the "feedback" field from its payload. Returns an empty
// string if no feedback is found.
func (e *Executor) latestReviewFeedback(storyID string) string {
	// Check review failed events (emitted by the reviewer and the
	// monitor's resetStoryToDraft). The reviewer uses agentID="reviewer"
	// and puts feedback in "summary"; the monitor uses varying agentIDs
	// and puts it in "reason". Search without AgentID filter to catch both.
	events, err := e.eventStore.List(state.EventFilter{
		Type:    state.EventStoryReviewFailed,
		StoryID: storyID,
	})
	if err != nil || len(events) == 0 {
		return ""
	}

	// Take the most recent event (last in the list).
	latest := events[len(events)-1]
	if latest.Payload == nil {
		return ""
	}

	var payload map[string]any
	if err := json.Unmarshal(latest.Payload, &payload); err != nil {
		return ""
	}

	// Try "summary" (from reviewer) then "reason" (from monitor).
	if summary, ok := payload["summary"].(string); ok && summary != "" {
		return summary
	}
	reason, _ := payload["reason"].(string)
	return reason
}

// runtimeForRole selects the configured runtime whose CLI can serve the
// model provider assigned to the given role. It maps well-known providers
// (anthropic, openai, google/gemini) to their corresponding CLI runtimes.
func (e *Executor) runtimeForRole(role agent.Role) string {
	modelCfg := role.ModelConfig(e.config.Models)
	provider := strings.ToLower(modelCfg.Provider)

	// Well-known provider → runtime mappings
	providerRuntimes := map[string][]string{
		"anthropic": {"claude-code", "claude"},
		"openai":    {"codex", "openai"},
		"google":    {"gemini"},
		"gemini":    {"gemini"},
	}

	if candidates, ok := providerRuntimes[provider]; ok {
		for _, name := range candidates {
			if _, exists := e.config.Runtimes[name]; exists {
				return name
			}
		}
	}

	// Fallback: first available runtime
	for name := range e.config.Runtimes {
		return name
	}
	return "claude-code"
}

// tierForRole maps agent roles to escalation tier numbers. These values
// align with the 5-tier escalation chain: 0 = same-role retry (junior/
// intermediate), 1 = senior, 2 = manager diagnosis, 3 = tech_lead re-plan,
// 4 = pause. Roles that aren't part of the execution chain (qa, supervisor)
// default to tier 0.
func tierForRole(role agent.Role) int {
	switch role {
	case agent.RoleJunior, agent.RoleIntermediate:
		return 0
	case agent.RoleSenior:
		return 1
	case agent.RoleManager:
		return 2
	case agent.RoleTechLead:
		return 3
	default:
		return 0
	}
}

// execExpandHome replaces a leading ~ with the user's home directory.
func execExpandHome(path string) string {
	if len(path) == 0 || path[0] != '~' {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}
