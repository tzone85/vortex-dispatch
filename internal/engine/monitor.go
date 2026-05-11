package engine

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/artifact"
	"github.com/tzone85/vortex-dispatch/internal/codegraph"
	"github.com/tzone85/vortex-dispatch/internal/config"
	vxdgit "github.com/tzone85/vortex-dispatch/internal/git"
	"github.com/tzone85/vortex-dispatch/internal/graph"
	"github.com/tzone85/vortex-dispatch/internal/llm"
	"github.com/tzone85/vortex-dispatch/internal/notify"
	"github.com/tzone85/vortex-dispatch/internal/runtime"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// Monitor polls running agents and progresses completed stories through
// review, QA, and merge.
type Monitor struct {
	registry         *runtime.Registry
	watchdog         *Watchdog
	reviewer         *Reviewer
	qa               *QA
	merger           *Merger
	conflictResolver *ConflictResolver
	config           config.Config
	eventStore       state.EventStore
	projStore        state.ProjectionStore
	escalation       *EscalationMachine
	manager          *Manager

	// artifactStore persists per-story artifacts (diffs, review results,
	// QA results) for post-mortem inspection.
	artifactStore *artifact.Store

	// checkpointPath is the file path for writing crash recovery checkpoints.
	// When set, the monitor writes phase-transition checkpoints before and
	// after the merge step so that resume can detect interrupted merges.
	checkpointPath string

	// reviewGate resolves the human-review mode for a requirement and
	// controls whether stories pause for approval before merging.
	reviewGate *ReviewGate

	// codeGraph enables blast-radius analysis before code review.
	// When set, the monitor runs detect-changes and passes the impact
	// analysis to the reviewer for richer context.
	codeGraph *codegraph.Runner

	// planner enables tier-3 (tech lead) re-planning. When set, the
	// monitor can decompose failing stories into smaller replacements.
	planner *Planner

	// dispatcher + executor allow the monitor to automatically spawn the
	// next wave of stories after merges complete, removing the need for
	// the user to manually run "vxd resume" between waves.
	dispatcher *Dispatcher
	executor   *Executor

	// docClient and docModel are used by the documentation generator
	// to create/update README.md after all stories are merged.
	docClient llm.Client
	docModel  string

	// dryRun causes the post-execution pipeline to simulate a successful
	// agent diff instead of checking the real worktree. This prevents
	// infinite retry loops when --dry-run agents produce no real changes.
	dryRun bool

	// mergeMu serializes the rebase-push-merge cycle so that each story
	// rebases onto the latest main before merging, preventing conflicts
	// when parallel agents touch the same files.
	mergeMu sync.Mutex

	// dagMu serializes DAG mutations (e.g. story splits) so that
	// concurrent pipelines don't corrupt the graph.
	dagMu sync.Mutex

	// slaStartTimes caches story start times (from STORY_STARTED event)
	// to avoid re-querying the event log on every poll cycle.
	slaStartTimes map[string]time.Time
	// slaBreachedSet tracks which stories have already emitted
	// STORY_SLA_BREACHED so we don't spam the event log.
	slaBreachedSet map[string]bool
	// slaMu protects the SLA tracking maps.
	slaMu sync.Mutex

	// notifier sends webhook notifications on SLA breaches and other
	// significant events. Defaults to NoopNotifier if not set.
	notifier notify.Notifier
}

// SetNotifier configures the outbound webhook notifier (Slack, Discord, etc.).
// If not called, notifications are silently dropped via NoopNotifier.
func (m *Monitor) SetNotifier(n notify.Notifier) { m.notifier = n }

// SetDryRun enables or disables dry-run mode on the monitor.
func (m *Monitor) SetDryRun(v bool) { m.dryRun = v }

// NewMonitor creates a Monitor wired to all pipeline components.
func NewMonitor(
	reg *runtime.Registry,
	wd *Watchdog,
	rev *Reviewer,
	qa *QA,
	merger *Merger,
	cfg config.Config,
	es state.EventStore,
	ps state.ProjectionStore,
) *Monitor {
	return &Monitor{
		registry:       reg,
		watchdog:       wd,
		reviewer:       rev,
		qa:             qa,
		merger:         merger,
		config:         cfg,
		eventStore:     es,
		projStore:      ps,
		escalation:     NewEscalationMachine(es, cfg.Routing),
		slaStartTimes:  make(map[string]time.Time),
		slaBreachedSet: make(map[string]bool),
	}
}

// SetArtifactStore enables per-story artifact persistence (diffs, reviews, QA).
func (m *Monitor) SetArtifactStore(store *artifact.Store) {
	m.artifactStore = store
}

// SetConflictResolver enables LLM-based automatic conflict resolution during
// rebase. Without this, rebase conflicts cause the story to be reset to draft.
func (m *Monitor) SetConflictResolver(cr *ConflictResolver) {
	m.conflictResolver = cr
}

// SetDocGenerator enables automatic README generation/update when all
// stories are merged. Uses the provided LLM client and model to generate
// documentation based on the implemented features.
func (m *Monitor) SetDocGenerator(client llm.Client, model string) {
	m.docClient = client
	m.docModel = model
}

// SetAutoResume enables automatic dispatch of the next wave when stories
// complete. Without this, the monitor exits after one wave and the user
// must manually run "vxd resume".
func (m *Monitor) SetAutoResume(d *Dispatcher, e *Executor) {
	m.dispatcher = d
	m.executor = e
}

// SetManager enables tier-2 (manager) escalation handling. When set, the
// monitor intercepts tier-2 stories before dispatch and routes them through
// the Manager for LLM-powered failure diagnosis and corrective actions.
func (m *Monitor) SetManager(mgr *Manager) {
	m.manager = mgr
}

// SetReviewGate enables human review gates. When set, the monitor checks
// the review mode for each story's requirement and pauses the pipeline
// for manual approval when the mode is "manual".
func (m *Monitor) SetReviewGate(rg *ReviewGate) {
	m.reviewGate = rg
}

// SetCodeGraph enables blast-radius analysis before code review.
// When set, the monitor runs detect-changes on the worktree and passes
// the impact analysis to the reviewer as additional context.
func (m *Monitor) SetCodeGraph(cg *codegraph.Runner) {
	m.codeGraph = cg
}

// SetCheckpointPath enables checkpoint writes for crash recovery.
func (m *Monitor) SetCheckpointPath(path string) {
	m.checkpointPath = path
}

// SetPlanner enables tier-3 (tech lead) re-planning. When set, the monitor
// can decompose failing stories into smaller replacement stories via the
// Planner's RePlan method.
func (m *Monitor) SetPlanner(p *Planner) {
	m.planner = p
}

// RunContext carries the state needed for auto-resume across waves.
type RunContext struct {
	ReqID          string
	PlannedStories []PlannedStory
	DAG            *graph.DAG
	WaveNumber     int
}

// Run polls active agents at the configured interval until all are done
// or the context is cancelled. When all agents finish naturally, Run waits
// for their post-execution pipelines (review, QA, merge) to complete.
// If auto-resume is enabled (SetAutoResume was called), Run then dispatches
// the next wave of ready stories and continues monitoring. This repeats
// until all stories are complete or context is cancelled.
func (m *Monitor) Run(ctx context.Context, agents []ActiveAgent, repoDir string) error {
	return m.RunWithContext(ctx, agents, repoDir, nil)
}

// RunWithContext is like Run but accepts a RunContext for auto-resume.
func (m *Monitor) RunWithContext(ctx context.Context, agents []ActiveAgent, repoDir string, rc *RunContext) error {
	pollInterval := time.Duration(m.config.Monitor.PollIntervalMs) * time.Millisecond
	if pollInterval == 0 {
		pollInterval = 10 * time.Second
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	var pipelineWG sync.WaitGroup

	active := make(map[string]ActiveAgent, len(agents))
	for _, a := range agents {
		active[a.Assignment.SessionName] = a
	}

	log.Printf("[monitor] tracking %d agents, polling every %s", len(active), pollInterval)

	for {
		select {
		case <-ctx.Done():
			log.Printf("[monitor] context cancelled, %d agents still active", len(active))
			return nil // graceful detach, agents continue in tmux
		case <-ticker.C:
			if len(active) == 0 {
				log.Printf("[monitor] all agents finished, waiting for post-execution pipelines")
				pipelineWG.Wait()
				log.Printf("[monitor] all pipelines complete")

				// Auto-resume: dispatch next wave if possible.
				if rc != nil && m.dispatcher != nil && m.executor != nil {
					newAgents := m.dispatchNextWave(ctx, rc, repoDir)
					if len(newAgents) > 0 {
						for _, a := range newAgents {
							active[a.Assignment.SessionName] = a
						}
						log.Printf("[monitor] auto-resumed: tracking %d new agents", len(newAgents))
						continue
					}
				}

				return nil
			}

			m.pollOnce(ctx, &pipelineWG, active, repoDir)
		}
	}
}

// pollOnce performs a single pass over active agents, checking status and
// kicking off post-execution pipelines for any that have finished.
func (m *Monitor) pollOnce(ctx context.Context, wg *sync.WaitGroup, active map[string]ActiveAgent, repoDir string) {
	for sessionName, ag := range active {
		rt, err := m.registry.Get(ag.RuntimeName)
		if err != nil {
			continue
		}

		// Watchdog check (handles permission prompts, stuck detection)
		m.watchdog.Check(sessionName, rt)

		// SLA check (observational — emits STORY_SLA_BREACHED once if elapsed > max)
		m.checkSLA(ag)

		// Check if agent is done
		status, err := rt.DetectStatus(sessionName)
		if err != nil {
			log.Printf("[monitor] %s status check error: %v", ag.Assignment.StoryID, err)
			continue
		}

		log.Printf("[monitor] %s: %s", ag.Assignment.StoryID, status)

		if status != runtime.StatusDone && status != runtime.StatusTerminated {
			continue
		}

		log.Printf("[monitor] agent %s finished (status: %s)", ag.Assignment.AgentID, status)

		// Emit story completed
		completedEvt := state.NewEvent(
			state.EventStoryCompleted,
			ag.Assignment.AgentID,
			ag.Assignment.StoryID,
			map[string]any{
				"status": status.String(),
			},
		)
		if err := m.eventStore.Append(completedEvt); err != nil {
			log.Printf("[monitor] failed to append completed event for %s: %v", ag.Assignment.StoryID, err)
		}
		if err := m.projStore.Project(completedEvt); err != nil {
			log.Printf("[monitor] failed to project completed event for %s: %v", ag.Assignment.StoryID, err)
		}

		// Drive post-execution pipeline
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.postExecutionPipeline(ctx, ag, repoDir)
		}()

		// Remove from active tracking
		m.watchdog.ClearFingerprint(sessionName)
		delete(active, sessionName)
		m.clearSLATracking(ag.Assignment.StoryID)

		log.Printf("[monitor] %d agents remaining", len(active))
	}
}

// clearSLATracking removes per-story SLA state. Called when a story finishes
// (success, failure, or terminated) to prevent unbounded map growth in
// long-running monitor sessions.
func (m *Monitor) clearSLATracking(storyID string) {
	m.slaMu.Lock()
	defer m.slaMu.Unlock()
	delete(m.slaStartTimes, storyID)
	delete(m.slaBreachedSet, storyID)
}

// checkSLA checks if the given active agent's story has breached its SLA.
// On first call for a story, looks up start time from the event log and
// complexity from the projection store (cached in slaStartTimes).
// Emits STORY_SLA_BREACHED at most once per story.
func (m *Monitor) checkSLA(ag ActiveAgent) {
	storyID := ag.Assignment.StoryID

	m.slaMu.Lock()
	defer m.slaMu.Unlock()

	// Already breached and emitted — skip
	if m.slaBreachedSet[storyID] {
		return
	}

	// Resolve start time (cache on first lookup).
	// Use the LATEST STORY_STARTED event so that resumed stories
	// measure SLA from the most recent attempt, not the original dispatch.
	startTime, ok := m.slaStartTimes[storyID]
	if !ok {
		events, err := m.eventStore.List(state.EventFilter{
			Type:    state.EventStoryStarted,
			StoryID: storyID,
		})
		if err != nil || len(events) == 0 {
			return // can't check without start time
		}
		startTime = events[len(events)-1].Timestamp
		m.slaStartTimes[storyID] = startTime
	}

	// Look up complexity from projection
	story, err := m.projStore.GetStory(storyID)
	if err != nil {
		return
	}

	if !CheckSLA(m.config.SLA, story.Complexity, startTime) {
		return
	}

	// Breached — emit event once
	maxDur := MaxDurationFor(m.config.SLA, story.Complexity)
	elapsed := time.Since(startTime)
	log.Printf("[monitor] SLA BREACH: %s elapsed=%v max=%v complexity=%d",
		storyID, elapsed.Round(time.Second), maxDur, story.Complexity)

	evt := state.NewEvent(state.EventStorySLABreached, ag.Assignment.AgentID, storyID, map[string]any{
		"complexity":      story.Complexity,
		"started_at":      startTime,
		"elapsed_seconds": int(elapsed.Seconds()),
		"max_minutes":     int(maxDur.Minutes()),
	})
	if err := m.eventStore.Append(evt); err != nil {
		log.Printf("[monitor] append SLA breach event for %s: %v", storyID, err)
		return
	}
	m.slaBreachedSet[storyID] = true

	// Optional webhook notification — fire-and-forget, errors logged not surfaced
	if m.notifier != nil {
		go func() {
			notifyErr := m.notifier.Notify(context.Background(), notify.Message{
				Title:    fmt.Sprintf("SLA breach: %s", storyID),
				Body:     fmt.Sprintf("Story exceeded its %v SLA", maxDur),
				Severity: "warn",
				Fields: map[string]string{
					"story_id":   storyID,
					"complexity": fmt.Sprintf("%d", story.Complexity),
					"elapsed":    elapsed.Round(time.Second).String(),
					"max":        maxDur.String(),
				},
			})
			if notifyErr != nil {
				log.Printf("[monitor] notify SLA breach for %s: %v", storyID, notifyErr)
			}
		}()
	}

	// Optional auto-escalation (opt-in via config)
	if m.config.SLA.AutoEscalate {
		m.escalateOnSLABreach(storyID, ag.Assignment.AgentID, elapsed)
	}
}

// escalateOnSLABreach triggers tier escalation due to SLA breach when
// sla.auto_escalate is enabled. Caps at tier 3 (does not pause via SLA).
func (m *Monitor) escalateOnSLABreach(storyID, fromAgent string, elapsed time.Duration) {
	currentTier, err := m.escalation.CurrentTier(storyID)
	if err != nil {
		log.Printf("[monitor] SLA escalate: get current tier for %s: %v", storyID, err)
		return
	}
	nextTier := currentTier + 1
	if nextTier >= 4 {
		log.Printf("[monitor] SLA escalate: %s already at tier %d, not escalating to pause", storyID, currentTier)
		return
	}

	reason := fmt.Sprintf("SLA breach (elapsed %s) — auto-escalated to tier %d",
		elapsed.Round(time.Second), nextTier)
	log.Printf("[monitor] auto-escalating %s from tier %d to tier %d: %s",
		storyID, currentTier, nextTier, reason)

	escEvt := state.NewEvent(state.EventStoryEscalated, fromAgent, storyID, map[string]any{
		"from_tier": currentTier,
		"to_tier":   nextTier,
		"reason":    reason,
	})
	if err := m.eventStore.Append(escEvt); err != nil {
		log.Printf("[monitor] append SLA escalation: %v", err)
		return
	}
	if err := m.projStore.Project(escEvt); err != nil {
		log.Printf("[monitor] project SLA escalation: %v", err)
	}

	// Reset to draft so dispatcher picks it up at the new tier
	resetEvt := state.NewEvent(state.EventStoryReviewFailed, fromAgent, storyID, map[string]any{
		"reason": reason,
	})
	if err := m.eventStore.Append(resetEvt); err != nil {
		log.Printf("[monitor] append SLA reset: %v", err)
		return
	}
	if err := m.projStore.Project(resetEvt); err != nil {
		log.Printf("[monitor] project SLA reset: %v", err)
	}
}

// postExecutionPipeline runs review, QA, and merge for a completed story.
func (m *Monitor) postExecutionPipeline(ctx context.Context, ag ActiveAgent, repoDir string) {
	storyID := ag.Assignment.StoryID
	branch := ag.Assignment.Branch

	log.Printf("[pipeline] starting post-execution for %s", storyID)

	// Create a 5-minute timeout context for the entire pipeline
	pipelineCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	// Auto-commit any uncommitted work left by the agent.
	// Claude Code agents frequently exit without committing their changes,
	// especially in -p (prompt) mode. This safety net ensures we capture
	// the work before checking the diff.
	autoCommit(ag.WorktreePath, storyID)

	// Strip VXD artifacts from the branch so they don't appear in PRs.
	// Agents commit CLAUDE.md, WAVE_CONTEXT.md, .vxd-prompts/ etc. into
	// their worktree branch. If these reach the PR, they overwrite the
	// project's real files (e.g. CLAUDE.md gets replaced with agent directive).
	stripVXDArtifactsFromBranch(ag.WorktreePath, storyID)

	// Guardrail 1: Strip LLM hallucination preamble from committed files.
	// Agents sometimes prefix files with reasoning text like "Looking at..."
	// or "Here's the solution:". This scrubs those lines and amends the commit.
	if cleaned := scrubHallucinationsFromWorktree(ag.WorktreePath); cleaned > 0 {
		log.Printf("[pipeline] hallucination guardrail: cleaned %d file(s) for %s", cleaned, storyID)
	}

	// Guardrail 2: Check for unresolved merge conflict markers.
	if conflicts := validateNoConflictMarkers(ag.WorktreePath); len(conflicts) > 0 {
		log.Printf("[pipeline] conflict markers found in %d file(s) for %s: %v", len(conflicts), storyID, conflicts)
		m.resetStoryToDraft(storyID, "monitor", fmt.Sprintf("unresolved merge conflicts in: %s", strings.Join(conflicts, ", ")))
		return
	}

	// Guardrail 3: Validate the build to catch syntax errors early.
	if buildErr := validateBuild(ag.WorktreePath); buildErr != nil {
		log.Printf("[pipeline] build validation failed for %s: %v", storyID, buildErr)
		// Don't block — log the warning and let review/QA catch it.
		// A build failure here means the agent produced broken code,
		// which the reviewer should reject.
	}

	// In dry-run mode, simulate a successful agent by writing a placeholder
	// file and committing it. This allows the full pipeline (review → QA → merge)
	// to exercise without real agent output.
	if m.dryRun {
		simulateDryRunChanges(ag.WorktreePath, storyID)
	}

	// Check if agent produced any changes.
	// Distinguish between git infrastructure errors (which count toward
	// the retry limit) and genuinely empty diffs so that broken worktrees
	// don't loop forever.
	diff, err := gitDiffForBase(ag.WorktreePath, m.config.Merge.BaseBranch)
	if err != nil {
		log.Printf("[pipeline] git diff error for %s: %v", storyID, err)
		m.resetStoryToDraft(storyID, "monitor", fmt.Sprintf("git diff error: %v", err))
		return
	}
	if diff == "" {
		log.Printf("[pipeline] no changes produced for %s, resetting to draft for re-dispatch", storyID)
		m.resetStoryToDraft(storyID, "monitor", "agent produced no code changes")
		return
	}

	// Persist the diff as an artifact for post-mortem inspection.
	if m.artifactStore != nil {
		m.artifactStore.WriteRaw(storyID, artifact.TypeGitDiff, diff)
	}

	// 1. Code Review
	if m.reviewer != nil {
		// Look up story details for the reviewer
		storyTitle := storyID
		storyAC := ""
		if story, err := m.projStore.GetStory(storyID); err == nil {
			storyTitle = story.Title
			storyAC = story.AcceptanceCriteria
		}

		// Capture the file tree to give the reviewer context about files
		// that already exist (prevents hallucinations about "missing" files).
		fileTree := captureFileTree(ag.WorktreePath)

		// Run blast-radius analysis if codegraph is available.
		blastRadius := ""
		if m.codeGraph != nil && m.codeGraph.Available() {
			impact, cgErr := m.codeGraph.DetectChanges(ctx, ag.WorktreePath, "HEAD~1")
			if cgErr != nil {
				log.Printf("[pipeline] codegraph detect-changes warning for %s: %v", storyID, cgErr)
			} else if !impact.Empty() {
				blastRadius = impact.FormatMarkdown()
				log.Printf("[pipeline] codegraph: risk=%.2f, %d changed functions, %d test gaps for %s",
					impact.RiskScore, len(impact.ChangedFunctions), len(impact.TestGaps), storyID)
			}
		}

		result, err := m.reviewer.Review(pipelineCtx, storyID, storyTitle, storyAC, diff, fileTree, blastRadius)
		if err != nil {
			// Check for pipeline timeout
			if err == context.DeadlineExceeded {
				log.Printf("[pipeline] review timeout for %s: pipeline timeout exceeded", storyID)
				m.resetStoryToDraft(storyID, "reviewer", "pipeline timeout: context deadline exceeded")
				return
			}
			// Fatal API errors (auth failures, billing exhaustion,
			// permission denied) will never succeed on retry — pause
			// the entire requirement to stop the infinite loop.
			if llm.IsFatalAPIError(err) {
				log.Printf("[pipeline] FATAL: non-retryable API error — pausing requirement for %s: %v", storyID, err)
				m.pauseRequirement(storyID, fmt.Sprintf("fatal API error: %v", err))
				return
			}
			log.Printf("[pipeline] review error for %s: %v", storyID, err)
			m.resetStoryToDraft(storyID, "reviewer", fmt.Sprintf("review error: %v", err))
			return
		}
		if !result.Passed {
			m.resetStoryToDraft(storyID, "reviewer", fmt.Sprintf("review rejected: %s", result.Summary))
			return
		}
		// Persist review result as artifact.
		if m.artifactStore != nil {
			m.artifactStore.Write(storyID, artifact.TypeReviewResult, map[string]any{
				"passed":  result.Passed,
				"summary": result.Summary,
			})
		}

		log.Printf("[pipeline] review passed for %s", storyID)
	}

	// 2. QA
	if m.qa != nil {
		result, err := m.qa.Run(pipelineCtx, storyID, ag.WorktreePath)
		if err != nil {
			// Check for pipeline timeout
			if err == context.DeadlineExceeded {
				log.Printf("[pipeline] QA timeout for %s: pipeline timeout exceeded", storyID)
				m.resetStoryToDraft(storyID, "qa", "pipeline timeout: context deadline exceeded")
				return
			}
			log.Printf("[pipeline] QA error for %s: %v", storyID, err)
			m.resetStoryToDraft(storyID, "qa", fmt.Sprintf("QA error: %v", err))
			return
		}
		if !result.Passed {
			summary := result.FailureSummary()
			hint := AnalyzeFailure(summary)
			enhancedSummary := summary + "\n\n[Diagnostic Hint] " + hint
			log.Printf("[pipeline] QA failed for %s: %s", storyID, summary)
			m.resetStoryToDraft(storyID, "qa", enhancedSummary)
			return
		}
		// Persist QA result as artifact.
		if m.artifactStore != nil {
			m.artifactStore.Write(storyID, artifact.TypeQAResult, map[string]any{
				"passed": result.Passed,
				"checks": result.Checks,
			})
		}

		log.Printf("[pipeline] QA passed for %s", storyID)
	}

	// Write checkpoint before merge for crash recovery.
	if m.checkpointPath != "" {
		cp := Checkpoint{
			ReqID:        storyID,
			Phase:        PhaseMerging,
			MergingStory: storyID,
			Timestamp:    time.Now().UTC(),
			PID:          os.Getpid(),
		}
		if err := WriteCheckpoint(m.checkpointPath, cp); err != nil {
			log.Printf("[pipeline] checkpoint write error: %v", err)
		}
	}

	// 3. Merge (serialized: rebase onto latest main, then push + merge)
	if m.merger != nil {
		m.mergeMu.Lock()
		defer m.mergeMu.Unlock()

		// Check review gate: if mode is "manual", create PR but wait for
		// human approval before merging.
		if m.reviewGate != nil {
			story, err := m.projStore.GetStory(storyID)
			if err == nil {
				mode := m.reviewGate.ResolveMode(story.ReqID, m.config.Merge)
				if mode == "manual" {
					result, err := m.rebaseAndCreatePR(pipelineCtx, storyID, branch, repoDir, ag.WorktreePath)
					if err != nil {
						// Check for pipeline timeout
						if err == context.DeadlineExceeded {
							log.Printf("[pipeline] PR creation timeout for %s: pipeline timeout exceeded", storyID)
							m.resetStoryToDraft(storyID, "merger", "pipeline timeout: context deadline exceeded")
							return
						}
						if llm.IsFatalAPIError(err) {
							log.Printf("[pipeline] FATAL: non-retryable API error during PR creation for %s: %v", storyID, err)
							m.pauseRequirement(storyID, fmt.Sprintf("fatal API error during PR creation: %v", err))
							return
						}
						log.Printf("[pipeline] PR creation error for %s: %v", storyID, err)
						m.resetStoryToDraft(storyID, "merger", fmt.Sprintf("PR creation error: %v", err))
						return
					}

					// Emit awaiting approval event — pipeline pauses here.
					awaitEvt := state.NewEvent(state.EventStoryAwaitingApproval, "monitor", storyID, map[string]any{
						"pr_number": result.PRNumber,
						"pr_url":    result.PRURL,
					})
					if err := m.eventStore.Append(awaitEvt); err != nil {
						log.Printf("[pipeline] failed to emit awaiting approval for %s: %v", storyID, err)
					}
					if err := m.projStore.Project(awaitEvt); err != nil {
						log.Printf("[pipeline] failed to project awaiting approval for %s: %v", storyID, err)
					}

					log.Printf("[pipeline] %s -> PR #%d (%s) awaiting human approval",
						storyID, result.PRNumber, result.PRURL)
					return
				}
			}
		}

		result, err := m.rebaseAndMerge(pipelineCtx, storyID, branch, repoDir, ag.WorktreePath)

		if err != nil {
			// Check for pipeline timeout
			if err == context.DeadlineExceeded {
				log.Printf("[pipeline] merge timeout for %s: pipeline timeout exceeded", storyID)
				m.resetStoryToDraft(storyID, "merger", "pipeline timeout: context deadline exceeded")
				return
			}
			// Fatal API errors during conflict resolution (credits exhausted,
			// auth failure) must pause the requirement immediately.
			if llm.IsFatalAPIError(err) {
				log.Printf("[pipeline] FATAL: non-retryable API error during merge for %s: %v", storyID, err)
				m.pauseRequirement(storyID, fmt.Sprintf("fatal API error during merge: %v", err))
				return
			}
			log.Printf("[pipeline] merge error for %s: %v", storyID, err)
			m.resetStoryToDraft(storyID, "merger", fmt.Sprintf("merge/rebase error: %v", err))
			return
		}
		log.Printf("[pipeline] %s -> PR #%d (%s) merged=%v",
			storyID, result.PRNumber, result.PRURL, result.Merged)

		// Capture what this story built into the wave context document
		// BEFORE cleaning up the worktree — subsequent stories will read this.
		if result.Merged {
			storyTitle := storyID
			if s, err := m.projStore.GetStory(storyID); err == nil {
				storyTitle = s.Title
			}
			CaptureStoryContext(repoDir, storyID, storyTitle, branch)
		}

		// Clean up worktree and branches after successful merge.
		if result.Merged {
			if err := vxdgit.RemoveWorktree(repoDir, ag.WorktreePath, branch); err != nil {
				log.Printf("[pipeline] worktree cleanup for %s: %v", storyID, err)
			}
			if err := vxdgit.DeleteRemoteBranch(repoDir, branch); err != nil {
				log.Printf("[pipeline] remote branch cleanup for %s: %v", storyID, err)
			}
		}
	}

	// Clear merge checkpoint after successful merge.
	if m.checkpointPath != "" {
		cp := Checkpoint{
			ReqID:     storyID,
			Phase:     PhaseMonitoring,
			Timestamp: time.Now().UTC(),
			PID:       os.Getpid(),
		}
		if err := WriteCheckpoint(m.checkpointPath, cp); err != nil {
			log.Printf("[pipeline] checkpoint write error: %v", err)
		}
	}

	// 4. Check if requirement is paused before next wave dispatch
	if m.isRequirementPaused(storyID) {
		log.Printf("[pipeline] requirement for %s is paused, skipping next wave dispatch", storyID)
		return
	}

	log.Printf("[pipeline] post-execution complete for %s, next wave can be dispatched", storyID)
}

// rebaseAndMerge fetches the latest base branch, rebases the worktree onto
// it, then delegates to the merger for push + PR + auto-merge. This must be
// called while holding mergeMu so that each story sees the result of any
// prior merge before rebasing.
//
// If a ConflictResolver is configured, rebase conflicts are automatically
// resolved via LLM instead of failing immediately.
func (m *Monitor) rebaseAndMerge(ctx context.Context, storyID, branch, repoDir, worktreePath string) (MergeResult, error) {
	baseBranch := m.config.Merge.BaseBranch
	if baseBranch == "" {
		baseBranch = "main"
	}

	log.Printf("[pipeline] fetching %s and rebasing %s for %s", baseBranch, branch, storyID)

	// Safety net: commit any unstaged changes before rebasing.
	autoCommit(worktreePath, storyID)

	if err := vxdgit.FetchBranch(repoDir, baseBranch); err != nil {
		return MergeResult{}, fmt.Errorf("fetch %s: %w", baseBranch, err)
	}

	upstream := "origin/" + baseBranch

	if m.conflictResolver != nil {
		// Use LLM-powered conflict resolution during rebase.
		if err := m.conflictResolver.RebaseWithResolution(ctx, storyID, worktreePath, upstream); err != nil {
			return MergeResult{}, fmt.Errorf("rebase onto %s: %w", baseBranch, err)
		}
	} else {
		// Fall back to the original abort-on-conflict behavior.
		if err := vxdgit.RebaseOnto(worktreePath, upstream); err != nil {
			return MergeResult{}, fmt.Errorf("rebase onto %s: %w", baseBranch, err)
		}
	}

	log.Printf("[pipeline] rebase succeeded for %s, proceeding to merge", storyID)

	return m.merger.Merge(storyID, storyID, repoDir, branch)
}

// rebaseAndCreatePR fetches the latest base branch, rebases the worktree onto
// it, then pushes and creates a PR without auto-merging. This is used when the
// review gate is in "manual" mode: the PR is created for human review and the
// pipeline pauses until the user runs "vxd approve".
func (m *Monitor) rebaseAndCreatePR(ctx context.Context, storyID, branch, repoDir, worktreePath string) (MergeResult, error) {
	baseBranch := m.config.Merge.BaseBranch
	if baseBranch == "" {
		baseBranch = "main"
	}

	log.Printf("[pipeline] fetching %s and rebasing %s for %s (manual review)", baseBranch, branch, storyID)

	autoCommit(worktreePath, storyID)

	if err := vxdgit.FetchBranch(repoDir, baseBranch); err != nil {
		return MergeResult{}, fmt.Errorf("fetch %s: %w", baseBranch, err)
	}

	upstream := "origin/" + baseBranch

	if m.conflictResolver != nil {
		if err := m.conflictResolver.RebaseWithResolution(ctx, storyID, worktreePath, upstream); err != nil {
			return MergeResult{}, fmt.Errorf("rebase onto %s: %w", baseBranch, err)
		}
	} else {
		if err := vxdgit.RebaseOnto(worktreePath, upstream); err != nil {
			return MergeResult{}, fmt.Errorf("rebase onto %s: %w", baseBranch, err)
		}
	}

	log.Printf("[pipeline] rebase succeeded for %s, creating PR (no auto-merge)", storyID)

	storyTitle := storyID
	if s, err := m.projStore.GetStory(storyID); err == nil {
		storyTitle = s.Title
	}

	return m.merger.CreatePROnly(storyID, storyTitle, repoDir, branch)
}

// isRequirementPaused looks up the requirement for a story and returns true
// if it is in the "paused" state.
func (m *Monitor) isRequirementPaused(storyID string) bool {
	story, err := m.projStore.GetStory(storyID)
	if err != nil {
		log.Printf("[monitor] failed to get story %s for pause check: %v", storyID, err)
		return false
	}

	req, err := m.projStore.GetRequirement(story.ReqID)
	if err != nil {
		log.Printf("[monitor] failed to get requirement %s for pause check: %v", story.ReqID, err)
		return false
	}

	return req.Status == "paused"
}

// pauseRequirement pauses the entire requirement that owns the given story.
// This is used when a fatal, non-retryable error (e.g. billing exhaustion)
// makes further progress impossible. The user must resolve the issue and
// run "vxd resume" to continue.
func (m *Monitor) pauseRequirement(storyID, reason string) {
	story, err := m.projStore.GetStory(storyID)
	if err != nil {
		log.Printf("[pipeline] cannot pause: failed to look up story %s: %v", storyID, err)
		return
	}

	pauseEvt := state.NewEvent(state.EventReqPaused, "monitor", "", map[string]any{
		"id":     story.ReqID,
		"reason": reason,
	})
	if err := m.eventStore.Append(pauseEvt); err != nil {
		log.Printf("[pipeline] failed to append pause event for req %s: %v", story.ReqID, err)
	}
	if err := m.projStore.Project(pauseEvt); err != nil {
		log.Printf("[pipeline] failed to project pause event for req %s: %v", story.ReqID, err)
	}
	log.Printf("[pipeline] requirement %s paused: %s", story.ReqID, reason)
	log.Printf("[pipeline] top up your API credits and run 'vxd resume %s' to continue", story.ReqID)
}

// resetStoryToDraft uses the EscalationMachine to decide whether the story
// should be retried at the current tier, escalated to the next tier, or
// paused (all tiers exhausted). It emits the appropriate events so the
// dispatcher picks the story back up with the correct routing.
func (m *Monitor) resetStoryToDraft(storyID, fromAgent, reason string) {
	shouldEsc, nextTier, err := m.escalation.ShouldEscalate(storyID)
	if err != nil {
		log.Printf("[pipeline] escalation check error for %s: %v", storyID, err)
	}

	if shouldEsc {
		currentTier, _ := m.escalation.CurrentTier(storyID)
		if nextTier >= 4 {
			m.pauseRequirement(storyID, fmt.Sprintf(
				"story exhausted all escalation tiers (%d): %s", currentTier, reason,
			))
			return
		}
		log.Printf("[pipeline] escalating %s from tier %d to tier %d: %s", storyID, currentTier, nextTier, reason)
		escEvt := state.NewEvent(state.EventStoryEscalated, fromAgent, storyID, map[string]any{
			"from_tier": currentTier,
			"to_tier":   nextTier,
			"reason":    reason,
		})
		m.eventStore.Append(escEvt)
		m.projStore.Project(escEvt)
		// Also reset to draft so the dispatcher picks it up at the new tier.
		resetEvt := state.NewEvent(state.EventStoryReviewFailed, fromAgent, storyID, map[string]any{
			"reason": fmt.Sprintf("escalated to tier %d: %s", nextTier, reason),
		})
		m.eventStore.Append(resetEvt)
		m.projStore.Project(resetEvt)
		return
	}

	// Normal reset within current tier.
	retryCount, _ := m.escalation.RetryCountAtCurrentTier(storyID)
	currentTier, _ := m.escalation.CurrentTier(storyID)
	maxRetries := m.escalation.MaxRetriesForTier(currentTier)
	log.Printf("[pipeline] reset %s to draft (attempt %d/%d at tier %d): %s",
		storyID, retryCount+1, maxRetries, currentTier, reason)

	evt := state.NewEvent(state.EventStoryReviewFailed, fromAgent, storyID, map[string]any{
		"reason": reason,
	})
	m.eventStore.Append(evt)
	m.projStore.Project(evt)
}

// dispatchNextWave determines which stories are now ready (dependencies met)
// and dispatches a new wave of agents. Returns the newly spawned ActiveAgents.
func (m *Monitor) dispatchNextWave(ctx context.Context, rc *RunContext, repoDir string) []ActiveAgent {
	// Bail out immediately if the requirement has been paused (e.g. by
	// billing exhaustion in a prior pipeline). Without this check, the
	// monitor would re-dispatch the same story in an infinite loop.
	if req, err := m.projStore.GetRequirement(rc.ReqID); err == nil && req.Status == "paused" {
		log.Printf("[auto-resume] requirement %s is paused, stopping auto-resume", rc.ReqID)
		return nil
	}

	// Build completed set from the projection store.
	stories, err := m.projStore.ListStories(state.StoryFilter{ReqID: rc.ReqID})
	if err != nil {
		log.Printf("[auto-resume] failed to list stories: %v", err)
		return nil
	}

	completed := make(map[string]bool)
	allDone := true
	for _, s := range stories {
		if IsStoryComplete(s.Status) {
			completed[s.ID] = true
		} else {
			allDone = false
		}
	}

	if allDone {
		log.Printf("[auto-resume] all %d stories complete for requirement %s", len(stories), rc.ReqID)

		// Generate/update README documentation as the final step.
		if m.docClient != nil {
			storyTitles := make([]string, len(stories))
			for i, s := range stories {
				storyTitles[i] = fmt.Sprintf("- %s", s.Title)
			}
			reqTitle := rc.ReqID
			if req, reqErr := m.projStore.GetRequirement(rc.ReqID); reqErr == nil {
				reqTitle = req.Title
			}
			repoDir := "."
			if wd, err := os.Getwd(); err == nil {
				repoDir = wd
			}
			generateDocumentation(ctx, repoDir, reqTitle, storyTitles, m.docClient, m.docModel)
		}

		// Run verification loop (Cycle 1): check build, tests, hallucinations, artifacts.
		repoDir := "."
		if wd, err := os.Getwd(); err == nil {
			repoDir = wd
		}
		verifyResult := RunVerificationLoop(ctx, repoDir, 1)

		if ShouldRunFixCycle(verifyResult) {
			log.Printf("[verify] cycle 1 found %d gaps — generating fix requirement", len(verifyResult.Gaps))
			fixReq := GapsToRequirement(verifyResult.Gaps, filepath.Base(repoDir))
			if fixReq != "" {
				// Write the fix requirement for manual or auto re-dispatch
				fixPath := filepath.Join(repoDir, ".vxd-fix-gaps.md")
				os.WriteFile(fixPath, []byte(fixReq), 0644)
				log.Printf("[verify] fix requirement written to %s", fixPath)
				log.Printf("[verify] run 'vxd req --file .vxd-fix-gaps.md --godmode' to auto-fix gaps")
			}
		} else {
			log.Printf("[verify] cycle 1 clean — no critical gaps found")
		}

		// Pull merged changes into the local checkout so the repo
		// reflects all merged PRs. Without this, local files are stale
		// and tools that read the repo see pre-VXD state.
		// Note: repoDir here is the shadowed local (line 977), which
		// resolves to cwd — the actual project root where VXD was invoked.
		pullBaseAfterMerge(repoDir, m.config.Merge.BaseBranch)

		// Mark requirement complete.
		compEvt := state.NewEvent(state.EventReqCompleted, "monitor", "", map[string]any{"id": rc.ReqID})
		m.eventStore.Append(compEvt)
		m.projStore.Project(compEvt)
		return nil
	}

	// Pre-dispatch interception: handle tier 2+ stories inline before
	// they reach the dispatcher. Tier 2 goes to the Manager for LLM
	// diagnosis; tier 3 goes to the tech-lead re-plan path.
	if m.manager != nil {
		readyIDs := rc.DAG.ReadyNodes(completed)
		storyLookup := make(map[string]PlannedStory, len(rc.PlannedStories))
		for _, ps := range rc.PlannedStories {
			storyLookup[ps.ID] = ps
		}

		// Track stories handled by manager/tech-lead this wave so they
		// don't get re-dispatched in the same wave. We use a separate set
		// rather than marking them "completed" because completed=true tells
		// the DAG that the story's dependency is satisfied — which is wrong
		// when the manager resets it to draft for retry.
		handledThisWave := make(map[string]bool)

		for _, id := range readyIDs {
			if completed[id] {
				continue
			}
			tier, err := m.escalation.CurrentTier(id)
			if err != nil {
				log.Printf("[auto-resume] tier lookup error for %s: %v", id, err)
				continue
			}
			if tier < 2 {
				continue
			}

			story, ok := storyLookup[id]
			if !ok {
				log.Printf("[auto-resume] story %s not found in planned stories", id)
				continue
			}

			handledThisWave[id] = true

			switch tier {
			case 2:
				log.Printf("[auto-resume] intercepting tier-2 story %s for manager diagnosis", id)
				m.handleManagerEscalation(ctx, story, repoDir, rc)
			default: // tier 3+
				log.Printf("[auto-resume] intercepting tier-%d story %s for tech-lead escalation", tier, id)
				m.handleTechLeadEscalation(ctx, story, repoDir, rc)
			}
		}

		// Mark handled stories as completed for this wave only, so
		// DispatchWave skips them. This prevents re-dispatch in the same
		// cycle while still blocking dependents correctly.
		for id := range handledThisWave {
			completed[id] = true
		}
	}

	rc.WaveNumber++
	assignments, err := m.dispatcher.DispatchWave(rc.DAG, completed, rc.ReqID, rc.PlannedStories, rc.WaveNumber)
	if err != nil {
		log.Printf("[auto-resume] dispatch error: %v", err)
		return nil
	}
	if len(assignments) == 0 {
		// Stall detection: check if we're stuck (stories exist but none are dispatchable)
		pendingCount := 0
		for _, s := range stories {
			if !IsStoryComplete(s.Status) {
				pendingCount++
			}
		}
		if pendingCount > 0 {
			log.Printf("[STALL] requirement %s has %d unfinished stories but none are dispatchable — all escalation tiers exhausted or dependencies unmet", rc.ReqID, pendingCount)
			log.Printf("[STALL] run 'vxd status --req %s' to inspect, then 'vxd resume %s --godmode' to retry", rc.ReqID, rc.ReqID)

			// Emit a stall event so external monitors (Hermes cron) can detect it
			stallEvt := state.NewEvent("PIPELINE_STALLED", "monitor", "", map[string]any{
				"req_id":        rc.ReqID,
				"pending_count": pendingCount,
				"total_stories": len(stories),
				"reason":        "no dispatchable stories — escalation tiers exhausted",
			})
			m.eventStore.Append(stallEvt)

			// Notify via webhook if configured
			if m.notifier != nil {
				go m.notifier.Notify(ctx, notify.Message{
					Title:     fmt.Sprintf("VXD STALLED: %s", rc.ReqID),
					Body:      fmt.Sprintf("%d stories stuck, all escalation tiers exhausted.\nRun: vxd resume %s --godmode", pendingCount, rc.ReqID),
					Severity:  "error",
					EventType: "PIPELINE_STALLED",
				})
			}
		} else {
			log.Printf("[auto-resume] no stories ready for next wave (dependencies not met)")
		}
		return nil
	}

	log.Printf("[auto-resume] dispatching %d stories in next wave", len(assignments))

	storyMap := make(map[string]PlannedStory, len(rc.PlannedStories))
	for _, ps := range rc.PlannedStories {
		storyMap[ps.ID] = ps
	}

	results := m.executor.SpawnAll(repoDir, assignments, storyMap)

	var active []ActiveAgent
	for _, r := range results {
		if r.Error != nil {
			log.Printf("[auto-resume] spawn error for %s: %v", r.Assignment.StoryID, r.Error)
			continue
		}
		log.Printf("[auto-resume] spawned %s -> %s (session: %s)",
			r.Assignment.StoryID, r.RuntimeName, r.Assignment.SessionName)
		active = append(active, ActiveAgent{
			Assignment:   r.Assignment,
			WorktreePath: r.WorktreePath,
			RuntimeName:  r.RuntimeName,
		})
	}

	return active
}

// handleManagerEscalation runs the Manager LLM to diagnose a tier-2 story
// and executes the recommended corrective action (retry, rewrite, split,
// or escalate to tech lead).
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

	// Persist the diagnosis for post-mortem review.
	logPath := filepath.Join(logDir, storyID+"-manager.log")
	os.WriteFile(logPath, []byte(fmt.Sprintf("Diagnosis: %s\nCategory: %s\nAction: %s\n",
		action.Diagnosis, action.Category, action.Action)), 0o644)

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
		os.RemoveAll(worktreePath)
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
	m.eventStore.Append(evt)
	m.projStore.Project(evt)

	resetEvt := state.NewEvent(state.EventStoryReviewFailed, "manager", storyID, map[string]any{
		"reason": "manager retry with fixes",
	})
	m.eventStore.Append(resetEvt)
	m.projStore.Project(resetEvt)
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
	m.eventStore.Append(evt)
	m.projStore.Project(evt)
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
		m.eventStore.Append(childEvt)
		m.projStore.Project(childEvt)
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
	m.eventStore.Append(splitEvt)
	m.projStore.Project(splitEvt)

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
	m.eventStore.Append(evt)
	m.projStore.Project(evt)
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
		m.eventStore.Append(childEvt)
		m.projStore.Project(childEvt)
	}

	childIDs := make([]string, len(children))
	for i, c := range children {
		childIDs[i] = c.ID
	}
	splitEvt := state.NewEvent(state.EventStorySplit, "tech_lead", storyID, map[string]any{
		"child_story_ids": childIDs,
		"reason":          "tech lead re-plan",
	})
	m.eventStore.Append(splitEvt)
	m.projStore.Project(splitEvt)

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
func FindDependents(stories []PlannedStory, storyID string) []string {
	var deps []string
	for _, s := range stories {
		for _, d := range s.DependsOn {
			if d == storyID {
				deps = append(deps, s.ID)
				break
			}
		}
	}
	return deps
}

// IsStoryComplete reports whether a story status is terminal for DAG
// dependency-resolution purposes. A story in one of these states is treated
// as "done" when computing which downstream stories are ready to dispatch.
//
// "awaiting_approval" is included because the PR has been submitted and the
// work is complete — only the human merge gate remains. Downstream stories can
// proceed (each branches from main; rebases handle conflicts).
func IsStoryComplete(status string) bool {
	switch status {
	case "merged", "pr_submitted", "split", "awaiting_approval":
		return true
	default:
		return false
	}
}

// simulateDryRunChanges writes a placeholder file and commits it so the
// post-execution pipeline has a non-empty diff to work with. Without this,
// dry-run mode would retry forever because the agent produces no real output.
func simulateDryRunChanges(worktreePath, storyID string) {
	simFile := filepath.Join(worktreePath, "dry-run-simulation.txt")
	content := fmt.Sprintf("[DRY RUN] Simulated changes for story %s\nThis file would be replaced by real agent output.\n", storyID)
	if err := os.WriteFile(simFile, []byte(content), 0o644); err != nil {
		log.Printf("[dry-run] failed to write simulation file: %v", err)
		return
	}

	// Stage and commit
	addCmd := exec.Command("git", "add", "dry-run-simulation.txt")
	addCmd.Dir = worktreePath
	if err := addCmd.Run(); err != nil {
		log.Printf("[dry-run] git add failed: %v", err)
		return
	}

	commitCmd := exec.Command("git", "commit", "-m", fmt.Sprintf("[dry-run] simulated changes for %s", storyID))
	commitCmd.Dir = worktreePath
	commitCmd.Run() // ignore error (may already be committed)

	log.Printf("[dry-run] simulated changes committed for %s", storyID)
}

// autoCommit stages and commits any uncommitted changes in the worktree.
// This is a safety net for agents that produce code but exit without
// committing. VXD artifacts (.vxd-prompts, CLAUDE.md, .serena) are excluded.
func autoCommit(worktreePath, storyID string) {
	// Check for uncommitted changes (staged or unstaged).
	statusCmd := exec.Command("git", "status", "--porcelain")
	statusCmd.Dir = worktreePath
	statusOut, err := statusCmd.CombinedOutput()
	if err != nil || len(strings.TrimSpace(string(statusOut))) == 0 {
		return // nothing to commit
	}

	log.Printf("[pipeline] auto-committing uncommitted work for %s", storyID)

	// Ensure VXD artifacts are in .gitignore so they are never committed.
	ensureGitignorePatterns(worktreePath)

	// Stage all non-ignored changes.
	addCmd := exec.Command("git", "add", "-A")
	addCmd.Dir = worktreePath
	if out, err := addCmd.CombinedOutput(); err != nil {
		log.Printf("[pipeline] git add failed for %s: %v (%s)", storyID, err, strings.TrimSpace(string(out)))
		return
	}

	// Commit with a descriptive message.
	commitCmd := exec.Command("git", "commit", "-m",
		fmt.Sprintf("feat(%s): auto-commit agent work\n\nVXD auto-committed changes that the agent left uncommitted.", storyID))
	commitCmd.Dir = worktreePath
	if out, err := commitCmd.CombinedOutput(); err != nil {
		log.Printf("[pipeline] auto-commit failed for %s: %v (%s)", storyID, err, strings.TrimSpace(string(out)))
		return
	}

	log.Printf("[pipeline] auto-commit succeeded for %s", storyID)
}

// stripVXDArtifactsFromBranch removes VXD infrastructure files (CLAUDE.md,
// WAVE_CONTEXT.md, .vxd-prompts/, etc.) from the worktree branch via
// git rm --cached, then amends the commit. This prevents agent-committed
// artifacts from appearing in PRs, which would overwrite the project's
// real CLAUDE.md with the agent-directive version.
func stripVXDArtifactsFromBranch(worktreePath, storyID string) {
	artifacts := []string{
		"CLAUDE.md",
		"WAVE_CONTEXT.md",
		"REQUIREMENT.md",
		".vxd-prompts",
		".serena",
		".superpowers",
	}

	// Detect base branch for restoring artifacts to their original state.
	baseBranch := "main"
	for _, candidate := range []string{"origin/main", "origin/master"} {
		check := exec.Command("git", "rev-parse", "--verify", candidate)
		check.Dir = worktreePath
		if err := check.Run(); err == nil {
			baseBranch = candidate
			break
		}
	}

	var restored []string
	for _, art := range artifacts {
		artPath := filepath.Join(worktreePath, art)
		if _, err := os.Stat(artPath); err != nil {
			continue
		}

		// Check if this file exists on the base branch (i.e., it's a
		// project file the agent overwrote, like CLAUDE.md).
		checkBase := exec.Command("git", "cat-file", "-e", baseBranch+":"+art)
		checkBase.Dir = worktreePath
		existsOnBase := checkBase.Run() == nil

		if existsOnBase {
			// Restore the base branch version so the merge is a no-op
			// for this file. The agent's changes are discarded.
			restoreCmd := exec.Command("git", "checkout", baseBranch, "--", art)
			restoreCmd.Dir = worktreePath
			if out, err := restoreCmd.CombinedOutput(); err != nil {
				log.Printf("[pipeline] git checkout %s -- %s for %s: %v (%s)", baseBranch, art, storyID, err, strings.TrimSpace(string(out)))
				continue
			}
		} else {
			// File doesn't exist on base — it was created by VXD/agent.
			// Remove it completely so it doesn't appear in the PR.
			rmCmd := exec.Command("git", "rm", "-rf", art)
			rmCmd.Dir = worktreePath
			if out, err := rmCmd.CombinedOutput(); err != nil {
				log.Printf("[pipeline] git rm %s for %s: %v (%s)", art, storyID, err, strings.TrimSpace(string(out)))
				continue
			}
		}
		restored = append(restored, art)
	}

	if len(restored) == 0 {
		return
	}

	// Stage changes and amend the commit
	stageCmd := exec.Command("git", "add", "-A")
	stageCmd.Dir = worktreePath
	stageCmd.CombinedOutput()

	amendCmd := exec.Command("git", "commit", "--amend", "--no-edit")
	amendCmd.Dir = worktreePath
	if out, err := amendCmd.CombinedOutput(); err != nil {
		log.Printf("[pipeline] amend after artifact strip for %s: %v (%s)", storyID, err, strings.TrimSpace(string(out)))
	} else {
		log.Printf("[pipeline] stripped %d VXD artifact(s) from branch for %s: %v", len(restored), storyID, restored)
	}
}

// pullMainAfterMerge fetches and fast-forward merges the base branch into
// the local checkout after all PRs have been merged. This ensures the repo
// directory reflects the actual merged state so that subsequent tools
// (evaluators, linters, other agents) see the completed work.
func pullMainAfterMerge(repoDir string) {
	pullBaseAfterMerge(repoDir, "")
}

func pullBaseAfterMerge(repoDir, baseBranch string) {
	if repoDir == "" {
		return
	}

	// Clean up VXD artifacts from the repo root so evaluators and
	// other tools don't see stale context from this pipeline run.
	for _, artifact := range []string{
		"WAVE_CONTEXT.md",
		"REQUIREMENT.md",
		".vxd-fix-gaps.md",
	} {
		p := filepath.Join(repoDir, artifact)
		if _, err := os.Stat(p); err == nil {
			os.Remove(p)
			log.Printf("[auto-resume] cleaned up %s from repo root", artifact)
		}
	}

	// Ensure gitignore covers VXD artifacts for the main repo (not just worktrees).
	ensureGitignorePatterns(repoDir)

	branches := []string{baseBranch}
	if baseBranch == "" {
		branches = []string{"main", "master"}
	}
	for _, branch := range branches {
		if branch == "" {
			continue
		}
		cmd := exec.Command("git", "rev-parse", "--verify", "refs/heads/"+branch)
		cmd.Dir = repoDir
		if err := cmd.Run(); err == nil {
			pull := exec.Command("git", "pull", "--ff-only", "origin", branch)
			pull.Dir = repoDir
			if out, pullErr := pull.CombinedOutput(); pullErr != nil {
				log.Printf("[auto-resume] git pull %s failed (non-fatal): %v\n%s", branch, pullErr, string(out))
			} else {
				log.Printf("[auto-resume] pulled latest %s into local checkout", branch)
			}
			return
		}
	}
	log.Printf("[auto-resume] could not detect base branch for pull")
}

// ensureGitignorePatterns appends VXD artifact patterns to .gitignore if
// they are not already present, preventing CLAUDE.md, .vxd-prompts/,
// .serena/, and other tool artifacts from being committed by agents.
func ensureGitignorePatterns(worktreePath string) {
	vxdPatterns := []string{
		"CLAUDE.md",
		"WAVE_CONTEXT.md",
		"REQUIREMENT.md",
		"vxd.yaml",
		".vxd-prompts/",
		".serena/",
		"firebase-debug.log",
	}

	giPath := worktreePath + "/.gitignore"
	existing, _ := os.ReadFile(giPath)
	content := string(existing)

	var toAdd []string
	for _, pat := range vxdPatterns {
		if !strings.Contains(content, pat) {
			toAdd = append(toAdd, pat)
		}
	}
	if len(toAdd) == 0 {
		return
	}

	appendix := "\n# VXD agent artifacts (auto-added)\n" + strings.Join(toAdd, "\n") + "\n"
	os.WriteFile(giPath, append(existing, []byte(appendix)...), 0o644)
}

// captureFileTree returns a compact listing of tracked files in the worktree.
// This gives the reviewer context about what already exists so it doesn't
// hallucinate about "missing" files that weren't part of the diff.
func captureFileTree(worktreePath string) string {
	cmd := exec.Command("git", "ls-files")
	cmd.Dir = worktreePath
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// gitDiff returns the git diff for committed changes in a worktree.
// It compares HEAD against the merge-base with the base branch so that it
// captures all changes since the worktree branch diverged, rather than
// only the last commit (which misses truly-empty agent runs).
//
// Tries merge-base candidates in order: origin/<base>, local <base>, then
// falls back to the repo root commit so repos without a remote still work.
func gitDiff(worktreePath string) (string, error) {
	return gitDiffForBase(worktreePath, "")
}

func gitDiffForBase(worktreePath, baseBranch string) (string, error) {
	// Try merge-base candidates in order of preference.
	// Include both main and master to support repos using either convention.
	var candidates []string
	if baseBranch != "" {
		candidates = []string{"origin/" + baseBranch, baseBranch}
	} else {
		candidates = []string{"origin/main", "origin/master", "main", "master"}
	}
	var mbOut []byte
	var mbErr error
	for _, ref := range candidates {
		mbCmd := exec.Command("git", "merge-base", "HEAD", ref)
		mbCmd.Dir = worktreePath
		mbOut, mbErr = mbCmd.Output()
		if mbErr == nil {
			break
		}
	}
	if mbErr != nil {
		// No merge-base found — fall back to the root commit of the
		// current branch so we diff all changes since the initial commit.
		rootCmd := exec.Command("git", "rev-list", "--max-parents=0", "HEAD")
		rootCmd.Dir = worktreePath
		rootOut, rootErr := rootCmd.Output()
		if rootErr != nil {
			return "", fmt.Errorf("git diff: cannot find merge-base or root commit: %w", rootErr)
		}
		mbOut = rootOut
	}

	mergeBase := strings.TrimSpace(string(mbOut))
	cmd := exec.Command("git", "diff", mergeBase, "HEAD")
	cmd.Dir = worktreePath
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git diff: %w", err)
	}

	// Filter out diffs that only touch .gitignore (written by
	// ensureGitignorePatterns before this check). A diff limited to
	// .gitignore means the agent produced no real code changes.
	if isGitignoreOnlyDiff(worktreePath, mergeBase) {
		return "", nil
	}

	return string(out), nil
}

// vxdArtifactPatterns are files created by VXD infrastructure, not by the
// agent's actual work. A diff containing ONLY these files means the agent
// produced no real code changes.
var vxdArtifactPatterns = []string{
	".gitignore",
	"CLAUDE.md",
	".vxd-prompts/",
	".serena/",
	"dry-run-simulation.txt",
}

// isArtifactFile returns true if the file path matches a VXD infrastructure artifact.
func isArtifactFile(path string) bool {
	for _, pattern := range vxdArtifactPatterns {
		if path == pattern || strings.HasPrefix(path, pattern) {
			return true
		}
	}
	return false
}

// isGitignoreOnlyDiff returns true when the only files changed between
// mergeBase and HEAD are VXD infrastructure artifacts (not real code).
func isGitignoreOnlyDiff(worktreePath, mergeBase string) bool {
	cmd := exec.Command("git", "diff", "--name-only", mergeBase, "HEAD")
	cmd.Dir = worktreePath
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	files := strings.TrimSpace(string(out))
	if files == "" {
		return false // no files changed — caller already handles empty diff
	}
	for _, f := range strings.Split(files, "\n") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if !isArtifactFile(f) {
			return false
		}
	}
	return true
}
