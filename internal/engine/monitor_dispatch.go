package engine

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/tzone85/vortex-dispatch/internal/notify"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

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

		repoDir := "."
		if wd, err := os.Getwd(); err == nil {
			repoDir = wd
		}

		// Pull merged changes into the local checkout FIRST so verification
		// runs against the true composed mainline (all merged PRs), not a
		// stale pre-VXD checkout. Without this, the repo's local files lag
		// behind origin and the gate would verify the wrong tree.
		pullBaseAfterMerge(repoDir, m.config.Merge.BaseBranch)

		// Leave the workspace neat: remove dangling branches (and their open
		// PRs) from stories that never merged. Merged branches are already gone.
		m.cleanupDanglingBranches(rc.ReqID, repoDir)

		// Completion gate: verify the composed mainline (build + tests) and
		// auto-fix a red build up to a bounded number of cycles. Only emit
		// REQ_COMPLETED when verification is green; otherwise emit REQ_BLOCKED
		// so a requirement is never reported complete on code that does not
		// compile. Falls back to the legacy advisory verification when no gate
		// is wired (e.g. dry-run or no LLM client).
		if m.completionGate != nil {
			if m.completionGate.Run(ctx, rc.ReqID, repoDir) {
				m.emitRequirementOutcome(rc.ReqID, state.EventReqCompleted, "REQ_COMPLETED")
			} else {
				log.Printf("[gate] %s: completion blocked — see .vxd-fix-gaps.md; run 'vxd resume %s --godmode' after addressing the gaps", rc.ReqID, rc.ReqID)
				m.emitRequirementOutcome(rc.ReqID, state.EventReqBlocked, "REQ_BLOCKED")
			}
			return nil
		}

		// Legacy advisory verification (no gate wired): check build/tests and
		// write a fix-gaps file, but complete the requirement regardless.
		verifyResult := RunVerificationLoop(ctx, repoDir, 1)
		if ShouldRunFixCycle(verifyResult) {
			log.Printf("[verify] cycle 1 found %d gaps — generating fix requirement", len(verifyResult.Gaps))
			fixReq := GapsToRequirement(verifyResult.Gaps, filepath.Base(repoDir))
			if fixReq != "" {
				fixPath := filepath.Join(repoDir, ".vxd-fix-gaps.md")
				if err := os.WriteFile(fixPath, []byte(fixReq), 0o600); err != nil {
					log.Printf("[verify] failed to write fix requirement to %s: %v", fixPath, err)
				} else {
					log.Printf("[verify] fix requirement written to %s", fixPath)
					log.Printf("[verify] run 'vxd req --file .vxd-fix-gaps.md --godmode' to auto-fix gaps")
				}
			}
		} else {
			log.Printf("[verify] cycle 1 clean — no critical gaps found")
		}

		m.emitRequirementOutcome(rc.ReqID, state.EventReqCompleted, "REQ_COMPLETED")
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
			stallEvt := state.NewEvent(state.EventPipelineStalled, "monitor", "", map[string]any{
				"req_id":        rc.ReqID,
				"pending_count": pendingCount,
				"total_stories": len(stories),
				"reason":        "no dispatchable stories — escalation tiers exhausted",
			})
			if err := m.eventStore.Append(stallEvt); err != nil {
				log.Printf("[monitor] append PIPELINE_STALLED event: %v", err)
			}

			// Notify via webhook if configured
			if m.notifier != nil {
				go func() {
					if err := m.notifier.Notify(ctx, notify.Message{
						Title:     fmt.Sprintf("VXD STALLED: %s", rc.ReqID),
						Body:      fmt.Sprintf("%d stories stuck, all escalation tiers exhausted.\nRun: vxd resume %s --godmode", pendingCount, rc.ReqID),
						Severity:  "error",
						EventType: string(state.EventPipelineStalled),
					}); err != nil {
						log.Printf("[monitor] PIPELINE_STALLED notify failed: %v", err)
					}
				}()
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
			DB:           r.DB,
		})
	}

	return active
}

// handleManagerEscalation runs the Manager LLM to diagnose a tier-2 story
// and executes the recommended corrective action (retry, rewrite, split,
// or escalate to tech lead).

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
//
// The logic lives in state.IsStoryComplete to avoid import cycles in sqlite.go.
// This forwarder preserves the engine.IsStoryComplete public API.
func IsStoryComplete(status string) bool {
	return state.IsStoryComplete(status)
}

// emitRequirementOutcome appends and projects a terminal requirement event
// (REQ_COMPLETED or REQ_BLOCKED), logging any store error with context. label
// is the human-readable event name used in log lines.
func (m *Monitor) emitRequirementOutcome(reqID string, evtType state.EventType, label string) {
	evt := state.NewEvent(evtType, "monitor", "", map[string]any{"id": reqID})
	if appErr := m.eventStore.Append(evt); appErr != nil {
		log.Printf("[pipeline] append %s for %s: %v", label, reqID, appErr)
	}
	if projErr := m.projStore.Project(evt); projErr != nil {
		log.Printf("[pipeline] project %s for %s: %v", label, reqID, projErr)
	}
}

// simulateDryRunChanges writes a placeholder file and commits it so the
// post-execution pipeline has a non-empty diff to work with. Without this,
