package cli

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/engine"
	vxdgit "github.com/tzone85/vortex-dispatch/internal/git"
	"github.com/tzone85/vortex-dispatch/internal/graph"
	"github.com/tzone85/vortex-dispatch/internal/runtime"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

func newResumeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resume <req-id>",
		Short: "Resume a paused requirement pipeline",
		Long:  "Loads existing state for a requirement, dispatches the next wave of ready stories, spawns agents in tmux sessions, and monitors progress through review, QA, and merge.",
		Args:  cobra.ExactArgs(1),
		RunE:  runResume,
	}
	cmd.Flags().Bool("godmode", false, "skip permission prompts on LLM calls (fully autonomous)")
	cmd.SilenceUsage = true
	return cmd
}

func runResume(cmd *cobra.Command, args []string) error {
	reqID := args[0]

	cfgPath, _ := cmd.Flags().GetString("config")
	s, err := loadStores(cfgPath)
	if err != nil {
		return err
	}
	defer s.Close()

	out := cmd.OutOrStdout()

	// Verify the requirement exists
	req, err := s.Proj.GetRequirement(reqID)
	if err != nil {
		return fmt.Errorf("requirement not found: %w", err)
	}

	// If paused, emit REQ_RESUMED event to transition back to planned
	if req.Status == "paused" {
		resumeEvt := state.NewEvent(state.EventReqResumed, "", "", map[string]any{
			"id": reqID,
		})
		if err := s.Events.Append(resumeEvt); err != nil {
			return fmt.Errorf("append resume event: %w", err)
		}
		if err := s.Proj.Project(resumeEvt); err != nil {
			return fmt.Errorf("project resume event: %w", err)
		}
		fmt.Fprintf(out, "Unpaused requirement: %s\n", req.Title)
	}

	fmt.Fprintf(out, "Resuming requirement: %s (%s)\n", req.Title, req.Status)

	// Load all stories for this requirement
	stories, err := s.Proj.ListStories(state.StoryFilter{ReqID: reqID})
	if err != nil {
		return fmt.Errorf("list stories: %w", err)
	}
	if len(stories) == 0 {
		fmt.Fprintf(out, "No stories found for this requirement.\n")
		return nil
	}

	// Rebuild the dependency graph from story_deps table
	dag, plannedStories, err := rebuildDAG(s.Proj, reqID, stories)
	if err != nil {
		return fmt.Errorf("rebuild dependency graph: %w", err)
	}

	// Determine completed stories and max wave number.
	completed := make(map[string]bool)
	maxWave := 0
	for _, story := range stories {
		if story.Status == "merged" || story.Status == "pr_submitted" || story.Status == "split" {
			completed[story.ID] = true
		}
		if story.Wave > maxWave {
			maxWave = story.Wave
		}
	}
	fmt.Fprintf(out, "Stories: %d total, %d completed\n", len(stories), len(completed))

	if len(completed) == len(stories) {
		fmt.Fprintf(out, "All stories are complete.\n")
		return nil
	}

	// Recover orphaned in-progress stories whose agent sessions have ended
	// but whose worktrees still contain committed work. The monitor will
	// detect these as StatusTerminated and run postExecutionPipeline.
	orphanAgents := recoverOrphanedStories(stories, s.Proj, s.Config)

	// Build story map for executor
	storyMap := make(map[string]engine.PlannedStory, len(plannedStories))
	for _, ps := range plannedStories {
		storyMap[ps.ID] = ps
	}

	// Set up runtime registry
	reg, err := runtime.NewRegistry(s.Config.Runtimes)
	if err != nil {
		return fmt.Errorf("init runtime registry: %w", err)
	}

	// Detect repo path
	repoDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	// Verify the repo has at least one commit (worktrees require a base commit)
	if !vxdgit.HasCommits(repoDir) {
		return fmt.Errorf("repository has no commits — run 'git add . && git commit -m \"initial commit\"' first")
	}

	dispatcher := engine.NewDispatcher(s.Config, s.Events, s.Proj)
	executor := engine.NewExecutor(reg, s.Config, s.Events, s.Proj)

	var activeAgents []engine.ActiveAgent

	if len(orphanAgents) > 0 {
		// Process orphaned stories first through the post-execution pipeline
		// (review → QA → merge). Auto-resume dispatches the next wave after.
		fmt.Fprintf(out, "\nRecovering %d orphaned stories (agent sessions ended, work exists)...\n", len(orphanAgents))
		for _, oa := range orphanAgents {
			fmt.Fprintf(out, "  [ORPHAN] %s (branch: %s)\n", oa.Assignment.StoryID, oa.Assignment.Branch)
		}
		activeAgents = orphanAgents
	} else {
		// Normal path: dispatch next wave of ready stories.
		waveNumber := maxWave + 1
		assignments, err := dispatcher.DispatchWave(dag, completed, reqID, plannedStories, waveNumber)
		if err != nil {
			return fmt.Errorf("dispatch wave: %w", err)
		}
		if len(assignments) == 0 {
			fmt.Fprintf(out, "No stories ready for dispatch (dependencies not yet met).\n")
			return nil
		}

		fmt.Fprintf(out, "\nWave: dispatching %d stories\n\n", len(assignments))

		results := executor.SpawnAll(repoDir, assignments, storyMap)

		for _, r := range results {
			if r.Error != nil {
				fmt.Fprintf(out, "  [FAIL] %s: %v\n", r.Assignment.StoryID, r.Error)
				continue
			}
			fmt.Fprintf(out, "  [%s] %s -> %s (session: %s, branch: %s)\n",
				r.Assignment.Role, r.Assignment.StoryID, r.RuntimeName,
				r.Assignment.SessionName, r.Assignment.Branch)
			activeAgents = append(activeAgents, engine.ActiveAgent{
				Assignment:   r.Assignment,
				WorktreePath: r.WorktreePath,
				RuntimeName:  r.RuntimeName,
			})
		}
	}

	if len(activeAgents) == 0 {
		return fmt.Errorf("no agents to process")
	}

	fmt.Fprintf(out, "\n%d agents working. Monitoring progress...\n", len(activeAgents))
	fmt.Fprintf(out, "Use 'vxd dashboard' in another terminal to watch progress.\n")
	fmt.Fprintf(out, "Press Ctrl+C to detach (agents continue in tmux).\n\n")

	// Build pipeline components for post-execution
	godmode, _ := cmd.Flags().GetBool("godmode")
	if !godmode {
		godmode = s.Config.Planning.Godmode
	}

	var reviewer *engine.Reviewer
	llmClient, llmErr := buildLLMClient(s.Config.Models.Senior.Provider, godmode)
	if llmErr != nil {
		log.Printf("Warning: LLM client unavailable, skipping code review: %v", llmErr)
	} else {
		seniorModel := s.Config.Models.Senior
		reviewer = engine.NewReviewer(llmClient, seniorModel.Model, seniorModel.MaxTokens, s.Events, s.Proj)
	}

	qaRunner := engine.NewQA(engine.QAConfig{}, &engine.ExecRunner{}, s.Events, s.Proj)

	var merger *engine.Merger
	if vxdgit.GHAvailable() {
		merger = engine.NewMerger(s.Config.Merge, &ghOpsAdapter{}, s.Events, s.Proj)
	}

	watchdog := engine.NewWatchdog(engine.WatchdogConfig{
		StuckThresholdS: s.Config.Monitor.StuckThresholdS,
	}, s.Events)

	// Start monitoring loop (Ctrl+C detaches cleanly, agents keep running)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	monitor := engine.NewMonitor(reg, watchdog, reviewer, qaRunner, merger, s.Config, s.Events, s.Proj)

	// Enable LLM-powered conflict resolution during rebase.
	if llmClient != nil {
		seniorModel := s.Config.Models.Senior
		conflictResolver := engine.NewConflictResolver(llmClient, seniorModel.Model, seniorModel.MaxTokens, s.Events)
		monitor.SetConflictResolver(conflictResolver)
	}

	// Enable auto-resume: when a wave completes, the monitor automatically
	// dispatches the next wave of ready stories instead of exiting.
	monitor.SetAutoResume(dispatcher, executor)

	rc := &engine.RunContext{
		ReqID:          reqID,
		PlannedStories: plannedStories,
		DAG:            dag,
		WaveNumber:     maxWave + 1,
	}

	if err := monitor.RunWithContext(ctx, activeAgents, repoDir, rc); err != nil {
		return err
	}

	// Print completion summary if the requirement finished.
	req, reqErr := s.Proj.GetRequirement(reqID)
	if reqErr == nil && req.Status == "completed" {
		summary, sumErr := engine.GenerateSummary(s.Events, s.Proj, reqID)
		if sumErr == nil {
			fmt.Fprint(out, summary)
		}
	}

	return nil
}

// rebuildDAG reconstructs the dependency graph from the story_deps table
// and builds PlannedStory entries from existing stories.
func rebuildDAG(proj *state.SQLiteStore, reqID string, stories []state.Story) (*graph.DAG, []engine.PlannedStory, error) {
	dag := graph.New()

	planned := make([]engine.PlannedStory, 0, len(stories))
	for _, story := range stories {
		dag.AddNode(story.ID)
		planned = append(planned, engine.PlannedStory{
			ID:                 story.ID,
			Title:              story.Title,
			Description:        story.Description,
			AcceptanceCriteria: engine.FlexibleString(story.AcceptanceCriteria),
			Complexity:         story.Complexity,
		})
	}

	// Reconstruct edges from story_deps table
	deps, err := proj.ListStoryDeps(reqID)
	if err != nil {
		return nil, nil, fmt.Errorf("list story deps: %w", err)
	}
	for _, dep := range deps {
		dag.AddEdge(dep.StoryID, dep.DependsOnID)
	}

	return dag, planned, nil
}

// ghOpsAdapter wraps the git package functions to satisfy the engine.GitHubOps interface.
type ghOpsAdapter struct{}

func (g *ghOpsAdapter) PushBranch(repoDir, branch string) error {
	return vxdgit.PushBranch(repoDir, branch)
}

func (g *ghOpsAdapter) CreatePR(repoDir, title, body, baseBranch, headBranch string) (engine.PRCreationResult, error) {
	pr, err := vxdgit.CreatePR(repoDir, title, body, baseBranch, headBranch)
	if err != nil {
		return engine.PRCreationResult{}, err
	}
	return engine.PRCreationResult{Number: pr.Number, URL: pr.URL}, nil
}

func (g *ghOpsAdapter) MergePR(repoDir string, prNumber int) error {
	return vxdgit.MergePR(repoDir, prNumber)
}

// recoverOrphanedStories finds stories stuck in "in_progress" with no live
// tmux session and a worktree containing committed work. It returns ActiveAgent
// entries that the monitor will immediately detect as terminated, routing them
// through postExecutionPipeline (review → QA → merge).
func recoverOrphanedStories(stories []state.Story, proj *state.SQLiteStore, cfg config.Config) []engine.ActiveAgent {
	worktreeBase := filepath.Join(expandHome(cfg.Workspace.StateDir), "worktrees")

	// Load all agents into a map for fast lookup.
	allAgents, _ := proj.ListAgents(state.AgentFilter{})
	agentByID := make(map[string]state.Agent, len(allAgents))
	for _, a := range allAgents {
		agentByID[a.ID] = a
	}

	// Default runtime fallback.
	var defaultRuntime string
	for name := range cfg.Runtimes {
		defaultRuntime = name
		break
	}

	var orphans []engine.ActiveAgent
	for _, story := range stories {
		if story.Status != "in_progress" {
			continue
		}

		worktreePath := filepath.Join(worktreeBase, story.ID)
		if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
			continue
		}

		// Use the original session name and runtime from the agent record,
		// falling back to synthetic values. A dead tmux session returns
		// StatusTerminated, which is exactly what we need.
		sessionName := fmt.Sprintf("vxd-orphan-%s", story.ID)
		rtName := defaultRuntime

		if ag, ok := agentByID[story.AgentID]; ok {
			if ag.SessionName != "" {
				sessionName = ag.SessionName
			}
			if ag.Runtime != "" {
				rtName = ag.Runtime
			}
		}

		branch := fmt.Sprintf("vxd/%s", story.ID)

		log.Printf("[resume] recovering orphaned story %s (session: %s, runtime: %s)", story.ID, sessionName, rtName)

		orphans = append(orphans, engine.ActiveAgent{
			Assignment: engine.Assignment{
				StoryID:     story.ID,
				AgentID:     story.AgentID,
				SessionName: sessionName,
				Branch:      branch,
			},
			WorktreePath: worktreePath,
			RuntimeName:  rtName,
		})
	}

	return orphans
}
