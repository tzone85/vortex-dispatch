package engine

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/artifact"
	"github.com/tzone85/vortex-dispatch/internal/devdb"
	vxdgit "github.com/tzone85/vortex-dispatch/internal/git"
	"github.com/tzone85/vortex-dispatch/internal/llm"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// postExecutionPipeline runs review, QA, and merge for a completed story.
func (m *Monitor) postExecutionPipeline(ctx context.Context, ag ActiveAgent, repoDir string) {
	storyID := ag.Assignment.StoryID
	branch := ag.Assignment.Branch

	// Release the story's devdb (if any) on every exit path. The success path
	// overrides outcomeForRelease to OutcomeSuccess; pause paths set it to
	// OutcomePaused. Anything that returns without updating it uses the default
	// OutcomeFailed, so the DB is released (or retained per KeepDBOnFail) on
	// every failing retry.
	outcomeForRelease := devdb.OutcomeFailed
	defer func() {
		if m.lifecycle == nil || ag.DB.ID == "" {
			return
		}
		// pipelineCtx is already cancelled by the time this defer runs (LIFO),
		// so use a fresh context — but bound it so a hung devdb provider can't
		// block this goroutine (and the WaitGroup it belongs to) indefinitely.
		rctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := m.lifecycle.Release(rctx, ag.DB, outcomeForRelease); err != nil {
			log.Printf("[pipeline] devdb release failed for %s (outcome=%s): %v (will GC later)",
				storyID, outcomeForRelease.String(), err)
		}
	}()

	log.Printf("[pipeline] starting post-execution for %s", storyID)

	// Bound the whole post-execution pipeline (review + QA + merge). Configurable
	// because slow LLM reviewers (Codex agent loop) + conflict resolution under
	// concurrent builds can exceed a tight limit. Falls back to 15m when unset.
	pipelineTimeout := time.Duration(m.config.Monitor.PipelineTimeoutS) * time.Second
	if pipelineTimeout <= 0 {
		pipelineTimeout = 15 * time.Minute
	}
	pipelineCtx, cancel := context.WithTimeout(ctx, pipelineTimeout)
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

	// Strip compiled binaries from the branch. Agents occasionally commit
	// compiled binaries (e.g. ./server, ./app) which bloat PRs, can trigger
	// conflict-resolver "prompt too long" errors, and pollute the repository.
	stripBinariesFromBranch(ag.WorktreePath, storyID)

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
		// A session/rate-limited agent also produces an empty diff. Distinguish
		// it from a genuinely unproductive agent by inspecting its session log:
		// if it was capacity-limited, pause cleanly instead of burning an
		// escalation attempt on a failure the agent never had a chance to avoid.
		if m.agentLogHasCapacityError(storyID) {
			log.Printf("[pipeline] no changes for %s but agent was capacity/session-limited — pausing without escalation", storyID)
			outcomeForRelease = devdb.OutcomePaused
			m.pauseRequirement(storyID, capacityPauseReason("agent execution", fmt.Errorf("session limit hit; agent produced no code")))
			return
		}
		log.Printf("[pipeline] no changes produced for %s, resetting to draft for re-dispatch", storyID)
		m.resetStoryToDraft(storyID, "monitor", "agent produced no code changes")
		return
	}

	// Persist the diff as an artifact for post-mortem inspection.
	if m.artifactStore != nil {
		if err := m.artifactStore.WriteRaw(storyID, artifact.TypeGitDiff, diff); err != nil {
			log.Printf("[pipeline] persist git-diff artifact for %s: %v", storyID, err)
		}
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
			if errors.Is(err, context.DeadlineExceeded) {
				log.Printf("[pipeline] review timeout for %s: pipeline timeout exceeded", storyID)
				m.resetStoryToDraft(storyID, "reviewer", "pipeline timeout: context deadline exceeded")
				return
			}
			// Fatal API errors (auth failures, billing exhaustion,
			// permission denied) will never succeed on retry — pause
			// the entire requirement to stop the infinite loop.
			if llm.IsFatalAPIError(err) {
				log.Printf("[pipeline] FATAL: non-retryable API error — pausing requirement for %s: %v", storyID, err)
				outcomeForRelease = devdb.OutcomePaused
				m.pauseRequirement(storyID, fmt.Sprintf("fatal API error: %v", err))
				return
			}
			// Transient capacity/session limit in the reviewer — pause cleanly
			// (resume after reset) rather than reject the story as low-quality.
			if llm.IsCapacityError(err) {
				outcomeForRelease = devdb.OutcomePaused
				m.pauseRequirement(storyID, capacityPauseReason("code review", err))
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
			if err := m.artifactStore.Write(storyID, artifact.TypeReviewResult, map[string]any{
				"passed":  result.Passed,
				"summary": result.Summary,
			}); err != nil {
				log.Printf("[pipeline] persist review artifact for %s: %v", storyID, err)
			}
		}

		log.Printf("[pipeline] review passed for %s", storyID)
	}

	// 2. QA
	if m.qa != nil {
		result, err := m.qa.Run(pipelineCtx, storyID, ag.WorktreePath)
		if err != nil {
			// Check for pipeline timeout
			if errors.Is(err, context.DeadlineExceeded) {
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
			if err := m.artifactStore.Write(storyID, artifact.TypeQAResult, map[string]any{
				"passed": result.Passed,
				"checks": result.Checks,
			}); err != nil {
				log.Printf("[pipeline] persist QA artifact for %s: %v", storyID, err)
			}
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
		// Hold mergeMu for the WHOLE remainder of the function (rebase, merge,
		// worktree/branch cleanup, post-merge integration build, checkpoint
		// clear). This is intentional, not just "serializing the merge":
		// every step below mutates the SHARED repoDir git index (fetch, rebase,
		// merge, `git worktree remove`, the integration build's checkout).
		// Two post-execution goroutines finishing at once would otherwise race
		// the git index lock and corrupt each other's operations. The lock is
		// the serialization boundary for all repoDir-mutating work.
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
						if errors.Is(err, context.DeadlineExceeded) {
							log.Printf("[pipeline] PR creation timeout for %s: pipeline timeout exceeded", storyID)
							m.resetStoryToDraft(storyID, "merger", "pipeline timeout: context deadline exceeded")
							return
						}
						if llm.IsFatalAPIError(err) {
							log.Printf("[pipeline] FATAL: non-retryable API error during PR creation for %s: %v", storyID, err)
							outcomeForRelease = devdb.OutcomePaused
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
			if errors.Is(err, context.DeadlineExceeded) {
				log.Printf("[pipeline] merge timeout for %s: pipeline timeout exceeded", storyID)
				m.resetStoryToDraft(storyID, "merger", "pipeline timeout: context deadline exceeded")
				return
			}
			// Fatal API errors during conflict resolution (credits exhausted,
			// auth failure) must pause the requirement immediately.
			if llm.IsFatalAPIError(err) {
				log.Printf("[pipeline] FATAL: non-retryable API error during merge for %s: %v", storyID, err)
				outcomeForRelease = devdb.OutcomePaused
				m.pauseRequirement(storyID, fmt.Sprintf("fatal API error during merge: %v", err))
				return
			}
			// Capacity/session limit during conflict resolution — pause cleanly
			// instead of resetting to draft (which would burn an escalation
			// attempt and eventually exhaust all tiers on a transient limit).
			if m.pauseIfCapacity(storyID, "merge conflict resolution", err) {
				outcomeForRelease = devdb.OutcomePaused
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

			// Signal the deferred Release at the top of this function to
			// use OutcomeSuccess. The defer handles the actual Release call
			// so we don't double-release here.
			outcomeForRelease = devdb.OutcomeSuccess

			// Post-merge integration build: validate that main still compiles
			// after squash-merging this story's branch. This catches cross-story
			// incompatibilities that per-story QA (run in the worktree) cannot
			// detect — e.g. story A exposes an interface, story B calls a method
			// that doesn't exist yet on that interface.
			if m.techLeadFixer != nil {
				if buildErr := runIntegrationBuild(repoDir); buildErr != nil {
					log.Printf("[pipeline] POST-MERGE BUILD FAILED for %s on main: %v", storyID, buildErr)
					integFail := state.NewEvent(state.EventStoryIntegrationFailed, "monitor", storyID, map[string]any{
						"error": buildErr.Error(),
					})
					if appErr := m.eventStore.Append(integFail); appErr != nil {
						log.Printf("[pipeline] append STORY_INTEGRATION_FAILED for %s: %v", storyID, appErr)
					}
					if projErr := m.projStore.Project(integFail); projErr != nil {
						log.Printf("[pipeline] project STORY_INTEGRATION_FAILED for %s: %v", storyID, projErr)
					}
					m.techLeadFixer.DispatchIntegrationFix(pipelineCtx, storyID, repoDir, buildErr.Error())
				}
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

	// Pre-merge gate: keep the base branch green. If this story's rebased state
	// turns a green base red, block the merge so the failure is fixed on the
	// branch instead of poisoning every later story's repo-wide QA.
	if err := m.verifyRebasedQA(ctx, storyID, branch, worktreePath); err != nil {
		return MergeResult{}, err
	}

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
	if hint := pauseResumeHint(reason); hint != "" {
		log.Printf("[pipeline] likely cause: %s", hint)
	}
	log.Printf("[pipeline] resolve the cause above, then run 'vxd resume %s' to continue", story.ReqID)
}

// pauseResumeHint inspects a pause reason and returns a tailored operator hint,
// or "" when the cause is unclear. Replaces a hardcoded "top up your API
// credits" line that misfired on timeouts, model-ID 404s, and provider outages.
func pauseResumeHint(reason string) string {
	r := strings.ToLower(reason)
	switch {
	// Capacity/session limit first: it co-occurs with words like "limit" that
	// the billing branch also matches, so it must win to give correct guidance.
	case strings.Contains(r, "capacity/session limit") || strings.Contains(r, "session limit") ||
		strings.Contains(r, "rate limit") || strings.Contains(r, "too many requests"):
		return "LLM capacity/session limit reached — wait for the stated reset time, then 'vxd resume'"
	case strings.Contains(r, "credit") || strings.Contains(r, "billing") ||
		strings.Contains(r, "quota") || strings.Contains(r, "insufficient"):
		return "LLM credit/billing limit — top up credits or check your subscription"
	case strings.Contains(r, "deadline exceeded") || strings.Contains(r, "timeout"):
		return "LLM call timed out — retry; if persistent, raise monitor.pipeline_timeout_s"
	case strings.Contains(r, "500") || strings.Contains(r, "server error") ||
		strings.Contains(r, "overloaded") || strings.Contains(r, "unavailable") ||
		strings.Contains(r, "connection refused"):
		return "provider outage/overload — retry shortly (check the provider status page)"
	case strings.Contains(r, "model") && (strings.Contains(r, "not exist") ||
		strings.Contains(r, "404") || strings.Contains(r, "access") || strings.Contains(r, "not supported")):
		return "model ID invalid or no access — verify the configured model (undated aliases)"
	case strings.Contains(r, "authentication") || strings.Contains(r, "unauthorized") || strings.Contains(r, "401"):
		return "auth failure — check API key / CLI login"
	default:
		return ""
	}
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
		if err := m.eventStore.Append(escEvt); err != nil {
			log.Printf("[pipeline] append escalation event for %s: %v", storyID, err)
		}
		if err := m.projStore.Project(escEvt); err != nil {
			log.Printf("[pipeline] project escalation event for %s: %v", storyID, err)
		}
		// Also reset to draft so the dispatcher picks it up at the new tier.
		resetEvt := state.NewEvent(state.EventStoryReviewFailed, fromAgent, storyID, map[string]any{
			"reason": fmt.Sprintf("escalated to tier %d: %s", nextTier, reason),
		})
		if err := m.eventStore.Append(resetEvt); err != nil {
			log.Printf("[pipeline] append escalation-reset event for %s: %v", storyID, err)
		}
		if err := m.projStore.Project(resetEvt); err != nil {
			log.Printf("[pipeline] project escalation-reset event for %s: %v", storyID, err)
		}
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
	if err := m.eventStore.Append(evt); err != nil {
		log.Printf("[pipeline] append reset event for %s: %v", storyID, err)
	}
	if err := m.projStore.Project(evt); err != nil {
		log.Printf("[pipeline] project reset event for %s: %v", storyID, err)
	}
}

// dispatchNextWave determines which stories are now ready (dependencies met)
