package cli

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/tzone85/vortex-dispatch/internal/artifact"
	"github.com/tzone85/vortex-dispatch/internal/codegraph"
	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/devdb"
	"github.com/tzone85/vortex-dispatch/internal/devdb/docker"
	"github.com/tzone85/vortex-dispatch/internal/devdb/ghost"
	"github.com/tzone85/vortex-dispatch/internal/devdb/null"
	"github.com/tzone85/vortex-dispatch/internal/engine"
	vxdgit "github.com/tzone85/vortex-dispatch/internal/git"
	"github.com/tzone85/vortex-dispatch/internal/graph"
	"github.com/tzone85/vortex-dispatch/internal/llm"
	"github.com/tzone85/vortex-dispatch/internal/notify"
	"github.com/tzone85/vortex-dispatch/internal/repolearn"
	"github.com/tzone85/vortex-dispatch/internal/runtime"
	"github.com/tzone85/vortex-dispatch/internal/scratchboard"
	"github.com/tzone85/vortex-dispatch/internal/state"
	"github.com/tzone85/vortex-dispatch/internal/tmux"
)

func newResumeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resume [req-id]",
		Short: "Resume a paused requirement pipeline",
		Long:  "Loads existing state for a requirement, dispatches the next wave of ready stories, spawns agents in tmux sessions, and monitors progress through review, QA, and merge.\n\nIf req-id is omitted and only one active (non-archived, non-completed) requirement exists, it is selected automatically.",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runResume,
	}
	cmd.Flags().Bool("godmode", false, "skip per-tool permission prompts during agent execution (does NOT bypass review_mode plan gate or auto_merge PR gate — use review_mode=auto and auto_merge=true for fully unattended operation)")
	cmd.Flags().Bool("review", false, "Force manual review mode for this run")
	cmd.Flags().Bool("auto", false, "Force auto mode for this run (skip review gates)")
	cmd.Flags().Bool("force", false, "Force override of lock file if another instance appears stuck")
	cmd.Flags().Bool("dry-run", false, "Simulate LLM responses for pipeline testing (no API calls)")
	cmd.SilenceUsage = true
	return cmd
}

func runResume(cmd *cobra.Command, args []string) error {
	if err := runDispatchPreflight(cmd); err != nil {
		return err
	}

	s, err := loadStores(cmd)
	if err != nil {
		return err
	}
	defer s.Close()

	out := cmd.OutOrStdout()

	// Auto-select the requirement if only one active exists.
	var reqID string
	if len(args) > 0 {
		reqID = args[0]
	} else {
		reqs, listErr := s.Proj.ListRequirementsFiltered(state.ReqFilter{ExcludeArchived: true})
		if listErr != nil {
			return fmt.Errorf("list requirements: %w", listErr)
		}
		var active []state.Requirement
		for _, r := range reqs {
			if r.Status != "completed" && r.Status != "archived" {
				active = append(active, r)
			}
		}
		switch len(active) {
		case 0:
			return fmt.Errorf("no active requirements found — run 'vxd req' first")
		case 1:
			reqID = active[0].ID
			fmt.Fprintf(out, "Auto-selected requirement: %s\n", active[0].Title)
		default:
			fmt.Fprintf(out, "Multiple active requirements:\n")
			for _, r := range active {
				id := r.ID
				if len(id) > 8 {
					id = id[:8]
				}
				fmt.Fprintf(out, "  [%s] %s (%s)\n", id, r.Title, r.Status)
			}
			return fmt.Errorf("specify which requirement to resume: vxd resume <req-id>")
		}
	}

	runtimeCfg := projectRuntimeConfig(s)

	// Acquire advisory lock to prevent concurrent VXD runs for this project.
	stateDir := expandHome(runtimeCfg.Workspace.StateDir)
	lockPath := filepath.Join(stateDir, "vxd.lock")
	forceFlag, _ := cmd.Flags().GetBool("force")
	if forceFlag {
		if _, lockErr := engine.ForceAcquireLock(lockPath, reqID); lockErr != nil {
			return fmt.Errorf("force acquire lock: %w", lockErr)
		}
	} else {
		if _, lockErr := engine.AcquireLock(lockPath, reqID); lockErr != nil {
			return lockErr
		}
	}
	defer engine.ReleaseLock(lockPath)

	// Verify the requirement exists
	req, err := s.Proj.GetRequirement(reqID)
	if err != nil {
		return fmt.Errorf("requirement not found: %w", err)
	}

	// Resolve and persist review mode
	reviewFlag, _ := cmd.Flags().GetBool("review")
	autoFlag, _ := cmd.Flags().GetBool("auto")
	if reviewFlag && autoFlag {
		return fmt.Errorf("cannot use both --review and --auto")
	}

	reviewGate := engine.NewReviewGate(s.Events)
	if reviewFlag || autoFlag {
		mode := "manual"
		if autoFlag {
			mode = "auto"
		}
		modeEvt := state.NewEvent(state.EventReviewModeSet, "system", "", map[string]any{
			"req_id": reqID,
			"mode":   mode,
		})
		if err := s.Events.Append(modeEvt); err != nil {
			return fmt.Errorf("append review-mode event: %w", err)
		}
		if err := s.Proj.Project(modeEvt); err != nil {
			return fmt.Errorf("project review-mode event: %w", err)
		}
	}

	// Plan approval gate
	effectiveMode := reviewGate.ResolveMode(reqID, s.Config.Merge)
	if effectiveMode == "manual" || effectiveMode == "plan_only" {
		if !reviewGate.PlanApproved(reqID) {
			return fmt.Errorf("plan approval required. Run 'vxd approve-plan %s' first", reqID)
		}
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
		if engine.IsStoryComplete(story.Status) {
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
	orphanAgents := recoverOrphanedStories(stories, s.Proj, runtimeCfg)

	// Run consistency check for crash recovery.
	checkpointPath := filepath.Join(stateDir, "checkpoint.json")
	recoveryIssues := runConsistencyCheck(stories, runtimeCfg, stateDir)
	if len(recoveryIssues) > 0 {
		fmt.Fprintf(out, "\nRecovery: found %d inconsistent stories\n", len(recoveryIssues))
		for _, issue := range recoveryIssues {
			fmt.Fprintf(out, "  [RECOVERY] %s: %s\n", issue.StoryID, issue.Detail)
			if issue.Action == engine.ActionResetToDraft {
				evt := state.NewEvent(state.EventStoryReset, "recovery", issue.StoryID, map[string]any{
					"reason": issue.Detail,
				})
				if err := s.Events.Append(evt); err != nil {
					log.Printf("[resume] append story-reset event for %s: %v", issue.StoryID, err)
				}
				if err := s.Proj.Project(evt); err != nil {
					log.Printf("[resume] project story-reset event for %s: %v", issue.StoryID, err)
				}
			}
		}
		recoveryEvt := state.NewEvent(state.EventRecoveryCompleted, "system", "", map[string]any{
			"issues_found": len(recoveryIssues),
		})
		if err := s.Events.Append(recoveryEvt); err != nil {
			log.Printf("[resume] append recovery-completed event: %v", err)
		}
		if err := s.Proj.Project(recoveryEvt); err != nil {
			log.Printf("[resume] project recovery-completed event: %v", err)
		}
	}

	// Recover orphaned devdb instances left behind by previously crashed pipelines.
	runDevDBOrphanRecovery(out, runtimeCfg, stories)

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

	dispatcher := engine.NewDispatcher(runtimeCfg, s.Events, s.Proj)
	executor := engine.NewExecutor(reg, runtimeCfg, s.Events, s.Proj)

	// Initialize artifact store for per-story persistence.
	stateDir0 := expandHome(runtimeCfg.Workspace.StateDir)
	artifactDir := filepath.Join(stateDir0, "artifacts")
	artStore, artErr := artifact.NewStore(artifactDir)
	if artErr == nil {
		executor.SetArtifactStore(artStore)
	}

	// Initialize scratchboard for cross-agent knowledge sharing.
	sbPath := filepath.Join(stateDir0, "scratchboards", reqID+".jsonl")
	sb, sbErr := scratchboard.New(sbPath)
	if sbErr == nil {
		executor.SetScratchboard(sb)
	}

	// Wire project directory for RepoProfile loading (repo learning system).
	executor.SetProjectDir(s.ProjectDir)

	// Wire devdb lifecycle (no-op when devdb.provider is absent or "null").
	// The SAME instance is injected into both Executor and Monitor so that
	// Provision (in Executor) and Release (in Monitor) share config state.
	lifecycle := newDevDBLifecycle(runtimeCfg, s.Events)
	if lifecycle != nil {
		executor.SetDevDBLifecycle(lifecycle)
		log.Printf("[startup] devdb enabled: provider=%s template=%s",
			runtimeCfg.DevDB.Provider, runtimeCfg.DevDB.Template)
	}

	// Enable Adapter/Runner execution path for decoupled command building.
	// Prefer "claude-code" runtime; fall back to first available.
	// Go map iteration is non-deterministic, so explicit preference is needed.
	{
		rtName, rtCfg := pickRuntime(s.Config.Runtimes)
		if rtName != "" {
			adapter := runtime.NewCLIAdapter(rtName, rtCfg.Command, rtCfg.Args, rtCfg.Models)
			runner, runnerErr := runtime.NewRunnerFromConfig(rtCfg)
			if runnerErr != nil {
				return fmt.Errorf("build runner for runtime %s: %w", rtName, runnerErr)
			}
			executor.SetAdapterRunner(adapter, runner)
			log.Printf("[resume] runtime %s using %T", rtName, runner)
		}
	}

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
	llmClient, llmErr := buildLLMClient(s.Config.Models.Senior.Provider, nil, godmode)
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	if dryRun {
		llmClient = llm.NewDryRunClient(200 * time.Millisecond)
		llmErr = nil
		fmt.Fprintf(out, "[DRY RUN] Using simulated LLM responses\n")
	}
	if llmErr != nil {
		log.Printf("Warning: LLM client unavailable, skipping code review: %v", llmErr)
	} else {
		seniorModel := s.Config.Models.Senior
		reviewer = engine.NewReviewer(llmClient, seniorModel.Model, seniorModel.MaxTokens, s.Events, s.Proj)
	}
	if s.Config.Planning.DesignApproach != "" {
		reviewer.SetDesignApproach(s.Config.Planning.DesignApproach)
	}

	qaRunner := engine.NewQA(buildQAConfig(s.Config, s.ProjectDir, repoDir), &engine.ExecRunner{}, s.Events, s.Proj)

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

	monitor := engine.NewMonitor(reg, watchdog, reviewer, qaRunner, merger, runtimeCfg, s.Events, s.Proj)
	monitor.SetCheckpointPath(checkpointPath)
	if artStore != nil {
		monitor.SetArtifactStore(artStore)
	}
	if lifecycle != nil {
		monitor.SetDevDBLifecycle(lifecycle)
	}

	// Wire webhook notifier (Phase 2). Slack disabled if URL not configured.
	if s.Config.Notify.SlackWebhookURL != "" && s.Config.Notify.NotifyOnSLA {
		monitor.SetNotifier(notify.NewSlackNotifier(s.Config.Notify.SlackWebhookURL))
		log.Printf("[resume] slack notifications enabled for SLA breaches")
	} else {
		monitor.SetNotifier(notify.NewNoopNotifier())
	}

	// Enable blast-radius analysis via code-review-graph (optional).
	cg := codegraph.NewRunner()
	if cg.Available() {
		monitor.SetCodeGraph(cg)
		log.Printf("[resume] codegraph enabled: %s", cg.BinPath)
	}

	// Enable dry-run mode: simulates agent changes so the full pipeline
	// (review → QA → merge) exercises without real agent output.
	if dryRun {
		monitor.SetDryRun(true)
	}

	// Enable LLM-powered conflict resolution during rebase.
	// The Tech Lead client is used for escalated resolution (binary conflicts,
	// senior failure, or conflicts spanning >3 files). It carries full requirement
	// context (title, acceptance criteria, dependency DAG) to produce more accurate
	// merges for integration-level conflicts.
	if llmClient != nil {
		seniorModel := s.Config.Models.Senior
		var techLeadClient llm.Client
		var techLeadModelName string
		if !dryRun {
			if tlc, tlErr := buildPlanningClient(s.Config.Models.TechLead.Provider, godmode); tlErr == nil {
				techLeadClient = tlc
				techLeadModelName = s.Config.Models.TechLead.Model
			}
		}
		conflictResolver := engine.NewConflictResolver(
			llmClient, seniorModel.Model,
			techLeadClient, techLeadModelName,
			seniorModel.MaxTokens,
			s.Proj,
			s.Events,
		)
		monitor.SetConflictResolver(conflictResolver)
	}

	// Enable tier-2 (manager) escalation with LLM-powered diagnosis.
	if llmClient != nil {
		managerModel := s.Config.Models.Manager
		manager := engine.NewManager(llmClient, managerModel.Model, managerModel.MaxTokens, s.Events, s.Proj)
		monitor.SetManager(manager)
	}

	// Enable tier-3 (tech lead) re-planning for stories that fail after manager diagnosis.
	if llmClient != nil {
		var planningClient llm.Client
		if dryRun {
			planningClient = llm.NewDryRunClient(200 * time.Millisecond)
		} else {
			pc, planErr := buildPlanningClient(s.Config.Models.TechLead.Provider, godmode)
			if planErr == nil {
				planningClient = pc
			}
		}
		if planningClient != nil {
			rePlanner := engine.NewPlanner(planningClient, runtimeCfg, s.Events, s.Proj)
			rePlanner.SetProjectDir(s.ProjectDir)
			monitor.SetPlanner(rePlanner)
		}
	}

	// Enable auto-resume: when a wave completes, the monitor automatically
	// dispatches the next wave of ready stories instead of exiting.
	monitor.SetAutoResume(dispatcher, executor)
	monitor.SetReviewGate(reviewGate)

	// Enable auto-documentation: when all stories merge, the monitor
	// generates/updates README.md with the implemented features.
	if llmClient != nil {
		monitor.SetDocGenerator(llmClient, s.Config.Models.Senior.Model)
	}

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

// newDevDBLifecycle constructs a Lifecycle from the resolved config and event
// store. Returns nil when devdb is disabled (provider="" or "null") so callers
// can guard injection with a simple nil check. Errors are logged and treated as
// "disabled" so dispatch is not blocked by devdb misconfiguration.
func newDevDBLifecycle(cfg config.Config, events state.EventStore) *devdb.Lifecycle {
	if cfg.DevDB.Provider == "" || cfg.DevDB.Provider == "null" {
		return nil
	}
	p, err := newDevDBProvider(cfg)
	if err != nil {
		log.Printf("[startup] devdb disabled: %v", err)
		return nil
	}
	return devdb.NewLifecycle(p, events, devdb.Config{
		Provider:     cfg.DevDB.Provider,
		Template:     cfg.DevDB.Template,
		KeepDBOnFail: cfg.DevDB.OnFailure.KeepDB,
		RetainHours:  time.Duration(cfg.DevDB.OnFailure.RetainHours) * time.Hour,
	})
}

// newDevDBProvider returns a Provider for the configured devdb backend,
// or a null provider if devdb is disabled.
func newDevDBProvider(cfg config.Config) (devdb.Provider, error) {
	switch cfg.DevDB.Provider {
	case "", "null":
		return null.New(), nil
	case "docker":
		return docker.NewProvider(docker.Config{
			Image:          cfg.DevDB.Docker.Image,
			ContainerName:  cfg.DevDB.Docker.ContainerName,
			TemplateVolume: cfg.DevDB.Docker.TemplateVolume,
			Network:        cfg.DevDB.Docker.Network,
			HostPortRange:  cfg.DevDB.Docker.HostPortRange,
			Host:           cfg.DevDB.Docker.Host,
		}), nil
	case "ghost":
		apiKey, err := ghost.ResolveAPIKey(cfg.DevDB.Ghost.APIKeyEnv, "")
		if err != nil {
			return nil, err
		}
		return ghost.New(ghost.Config{
			APIKey:  apiKey,
			SpaceID: cfg.DevDB.Ghost.SpaceID,
		})
	default:
		return nil, fmt.Errorf("devdb.provider %q is not recognised", cfg.DevDB.Provider)
	}
}

// runDevDBOrphanRecovery scans for devdb instances left behind by previously
// crashed pipelines and releases the ones older than RetainHours. It is a
// best-effort operation: failures are logged but never block resume.
func runDevDBOrphanRecovery(out io.Writer, cfg config.Config, stories []state.Story) {
	if cfg.DevDB.Provider == "" || cfg.DevDB.Provider == "null" {
		return
	}
	p, err := newDevDBProvider(cfg)
	if err != nil {
		fmt.Fprintf(out, "DevDB recovery: skipped (provider init failed: %v)\n", err)
		return
	}
	if err := p.Ping(context.Background()); err != nil {
		fmt.Fprintf(out, "DevDB recovery: provider unreachable (%v) — skipping\n", err)
		return
	}

	active := make([]string, 0, len(stories))
	for _, s := range stories {
		if s.Status == "merged" || s.Status == "archived" {
			continue
		}
		active = append(active, s.ID)
	}

	orphans, err := devdb.FindOrphans(context.Background(), p, devdb.PrefixVXD, active)
	if err != nil {
		fmt.Fprintf(out, "DevDB recovery: FindOrphans failed: %v\n", err)
		return
	}
	if len(orphans) == 0 {
		return
	}

	retainHours := time.Duration(cfg.DevDB.OnFailure.RetainHours) * time.Hour
	if retainHours <= 0 {
		retainHours = 24 * time.Hour
	}
	deleted, kept, err := devdb.ReleaseOrphans(context.Background(), p, orphans, retainHours)
	fmt.Fprintf(out, "DevDB recovery: scanned %d orphans, deleted %d, kept %d (newer than %s)\n",
		len(orphans), len(deleted), len(kept), retainHours)
	if err != nil {
		fmt.Fprintf(out, "DevDB recovery: some deletes failed: %v\n", err)
	}
}

func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

func projectRuntimeConfig(s stores) config.Config {
	cfg := s.Config
	if s.ProjectDir != "" {
		cfg.Workspace.StateDir = s.ProjectDir
	}
	return cfg
}

func buildQAConfig(cfg config.Config, projectDir, repoDir string) engine.QAConfig {
	qaCfg := engine.QAConfig{}
	if projectDir != "" {
		if profile, err := repolearn.LoadProfile(projectDir); err == nil {
			qaCfg.LintCommand = profile.Build.LintCommand
			qaCfg.BuildCommand = profile.Build.BuildCommand
			qaCfg.TestCommand = profile.Test.TestCommand
		}
	}
	if qaCfg.LintCommand == "" && qaCfg.BuildCommand == "" && qaCfg.TestCommand == "" && repoDir != "" {
		if profile, err := repolearn.ScanStatic(repoDir); err == nil {
			qaCfg.LintCommand = profile.Build.LintCommand
			qaCfg.BuildCommand = profile.Build.BuildCommand
			qaCfg.TestCommand = profile.Test.TestCommand
		}
	}
	for _, sc := range cfg.QA.SuccessCriteria {
		qaCfg.SuccessCriteria = append(qaCfg.SuccessCriteria, engine.Criterion{
			Kind:    engine.CriterionKind(sc.Kind),
			Value:   sc.Value,
			Path:    sc.Path,
			Message: sc.Message,
		})
	}
	return qaCfg
}

func runConsistencyCheck(stories []state.Story, cfg config.Config, stateDir string) []engine.RecoveryIssue {
	worktreeBase := filepath.Join(stateDir, "worktrees")

	var cp *engine.Checkpoint
	checkpointPath := filepath.Join(stateDir, "checkpoint.json")
	if read, err := engine.ReadCheckpoint(checkpointPath); err == nil {
		cp = &read
	}

	var recoveryStories []engine.RecoveryStory
	for _, story := range stories {
		if story.Status != "in_progress" && story.Status != "merging" {
			continue
		}
		rs := engine.RecoveryStory{
			ID:          story.ID,
			Status:      story.Status,
			HasWorktree: dirExists(filepath.Join(worktreeBase, story.ID)),
		}
		if story.AgentID != "" {
			sessionName := fmt.Sprintf("vxd-%s", story.ID)
			rs.HasTmux = tmux.SessionExists(sessionName)
		}
		recoveryStories = append(recoveryStories, rs)
	}

	return engine.CheckConsistency(recoveryStories, cp)
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

// pickRuntime selects the best runtime from the config map.
// Prefers "claude-code" since Go map iteration is non-deterministic
// and randomly picking codex/gemini causes agent spawn failures.
func pickRuntime(runtimes map[string]config.RuntimeConfig) (string, config.RuntimeConfig) {
	if rt, ok := runtimes["claude-code"]; ok {
		return "claude-code", rt
	}
	for name, rt := range runtimes {
		return name, rt
	}
	return "", config.RuntimeConfig{}
}
