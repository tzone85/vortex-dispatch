package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/agent"
	"github.com/tzone85/vortex-dispatch/internal/artifact"
	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/devdb"
	vxdgit "github.com/tzone85/vortex-dispatch/internal/git"
	"github.com/tzone85/vortex-dispatch/internal/repolearn"
	"github.com/tzone85/vortex-dispatch/internal/runtime"
	"github.com/tzone85/vortex-dispatch/internal/sanitize"
	"github.com/tzone85/vortex-dispatch/internal/scratchboard"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// ActiveAgent tracks a running agent session for the monitor.
type ActiveAgent struct {
	Assignment   Assignment
	WorktreePath string
	RuntimeName  string
	// DB is the ephemeral database provisioned for this story.
	// Zero-value means no database was provisioned (lifecycle not configured
	// or provisioning failed). Used by the monitor to release the DB after
	// a successful merge.
	DB devdb.DB
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
	projectDir    string // path to project state dir (for loading RepoProfile)
	lifecycle     *devdb.Lifecycle

	// Adapter/Runner for decoupled execution. When both are set, spawn()
	// uses Adapter.Prepare() + Runner.Run() instead of the monolithic
	// Runtime.Spawn(). Nil means fall back to legacy path.
	adapter *runtime.CLIAdapter
	runner  runtime.Runner
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

// SetProjectDir sets the project state directory for loading RepoProfile.
func (e *Executor) SetProjectDir(dir string) {
	e.projectDir = dir
}

// SetAdapterRunner enables the decoupled Adapter/Runner execution path.
// When set, spawn() uses Adapter.Prepare() + Runner.Run() instead of
// the monolithic Runtime.Spawn().
func (e *Executor) SetAdapterRunner(a *runtime.CLIAdapter, r runtime.Runner) {
	e.adapter = a
	e.runner = r
}

// SetDevDBLifecycle wires a devdb.Lifecycle into the executor so that every
// story spawn provisions an ephemeral database after the worktree is created.
// Pass nil to disable devdb provisioning (the default).
func (e *Executor) SetDevDBLifecycle(lc *devdb.Lifecycle) {
	e.lifecycle = lc
}

// HasDevDBLifecycle reports whether a devdb.Lifecycle has been configured.
// Used by tests to verify the lifecycle field is set correctly.
func (e *Executor) HasDevDBLifecycle() bool {
	return e.lifecycle != nil
}

// GetDevDBLifecycle returns the configured devdb.Lifecycle, or nil if none.
// Used by the Monitor to release ephemeral DBs after a successful merge.
func (e *Executor) GetDevDBLifecycle() *devdb.Lifecycle {
	return e.lifecycle
}

// SpawnResult holds the outcome of spawning an agent for one assignment.
type SpawnResult struct {
	Assignment   Assignment
	WorktreePath string
	RuntimeName  string
	Error        error
	// DB is the provisioned ephemeral database for this story.
	// Zero-value means no database was provisioned (lifecycle not configured
	// or provisioning failed with degraded fallback).
	DB devdb.DB
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

	// Provision ephemeral devdb for this story if a Lifecycle is configured.
	// Runs after worktree creation so WriteEnvFiles has a real directory.
	// Failure is non-fatal: a fallback notice is written and the spawn continues.
	// The 60s bound is the devdb provisioning SLA — without it, a hung
	// Docker daemon would block this goroutine (and the executor's pipeline
	// lock) indefinitely.
	if e.lifecycle != nil {
		project := filepath.Base(e.projectDir)
		if e.projectDir == "" || project == "." {
			project = "default"
		}
		provCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		provisioned, err := e.lifecycle.Provision(provCtx, a.StoryID, project, worktreePath)
		cancel()
		if err != nil {
			log.Printf("[executor] devdb provision failed for %s: %v", a.StoryID, err)
			_ = devdb.WriteFallbackNotice(worktreePath, err)
		} else {
			result.DB = provisioned
		}
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
	isFrontend := detectFrontend(story.Title, story.Description, story.OwnedFiles)

	// Frontend stories build against the pulled Figma design when one exists:
	// the markdown rides in the prompt and the rendered PNGs are copied into
	// the worktree so the agent can open them.
	designContext := ""
	if isFrontend {
		designContext = loadDesignContext(repoDir)
		if designContext != "" {
			copyDesignDir(repoDir, worktreePath)
		}
	}

	// Load RepoProfile if available to enrich prompts with pre-learned knowledge.
	var techStackStr, lintCmd, buildCmd, testCmd string
	if e.projectDir != "" {
		if profile, err := repolearn.LoadProfile(e.projectDir); err == nil && profile.TechStack.PrimaryLanguage != "" {
			techStackStr = profile.Summary()
			lintCmd = profile.Build.LintCommand
			buildCmd = profile.Build.BuildCommand
			testCmd = profile.Test.TestCommand
		}
	}
	// Fallback to shallow git.ScanRepo if no profile
	if techStackStr == "" {
		stack := vxdgit.ScanRepo(worktreePath)
		techStackStr = fmt.Sprintf("%s (%s)", stack.Language, stack.BuildTool)
	}

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
		IsFrontend:         isFrontend,
		DesignContext:      designContext,
		TechStack:          techStackStr,
		LintCommand:        lintCmd,
		BuildCommand:       buildCmd,
		TestCommand:        testCmd,
		DesignApproach:     e.config.Planning.DesignApproach,
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
			IsFrontend:         isFrontend,
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
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		result.Error = fmt.Errorf("create log dir %s: %w", logDir, err)
		return result
	}
	logFile := filepath.Join(logDir, a.StoryID+".log")

	// Spawn the runtime session — use Adapter/Runner if configured,
	// fall back to monolithic Runtime.Spawn().
	sessionCfg := runtime.SessionConfig{
		SessionName:  a.SessionName,
		WorkDir:      worktreePath,
		Model:        modelCfg.Model,
		Goal:         goalPrompt,
		SystemPrompt: agent.SystemPrompt(a.Role, promptCtx),
		LogFile:      logFile,
	}

	if e.adapter != nil && e.runner != nil {
		prepared, err := e.adapter.Prepare(sessionCfg)
		if err != nil {
			result.Error = fmt.Errorf("prepare execution for %s: %w", a.StoryID, err)
			return result
		}
		if err := e.runner.Run(prepared); err != nil {
			result.Error = fmt.Errorf("run execution for %s: %w", a.StoryID, err)
			return result
		}
	} else {
		if err := rt.Spawn(sessionCfg); err != nil {
			result.Error = fmt.Errorf("spawn runtime for %s: %w", a.StoryID, err)
			return result
		}
	}

	// Write launch config artifact for reproducibility.
	if e.artifactStore != nil {
		if err := e.artifactStore.Write(a.StoryID, artifact.TypeLaunchConfig, artifact.LaunchConfig{
			StoryID:   a.StoryID,
			Runtime:   rtName,
			Model:     modelCfg.Model,
			Prompt:    goalPrompt,
			WaveBrief: waveContext,
		}); err != nil {
			log.Printf("[executor] persist launch-config artifact for %s: %v", a.StoryID, err)
		}
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
	var feedback string
	if summary, ok := payload["summary"].(string); ok && summary != "" {
		feedback = summary
	} else {
		feedback, _ = payload["reason"].(string)
	}

	// Sanitize: strip prompt injection patterns from reviewer LLM output
	// before it's re-injected into retry agent prompts. A hallucinating
	// reviewer could emit "ignore previous instructions" in its summary,
	// which would then hijack the retry agent.
	if sanitize.DetectPromptInjection(feedback) {
		log.Printf("[executor] stripped prompt injection from review feedback for %s", storyID)
		return "[Review feedback redacted — contained prompt injection pattern]"
	}
	return feedback
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
