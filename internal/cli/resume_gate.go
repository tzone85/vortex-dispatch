package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/tzone85/vortex-dispatch/internal/engine"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// The completion gate's side of `vxd resume`: which signals end the run, when
// there is nothing to dispatch and only the gate to re-run, and what the
// command reports and exits with afterwards.

// ErrRequirementBlocked and ErrNoVerdict are what resume ends with when the
// completion gate did not certify the mainline: a red verdict (the gaps are
// in .vxd-fix-gaps.md) or none (the run was interrupted before one). Both
// exit 1, so `vxd resume X && ./deploy.sh` cannot deploy a mainline the gate
// did not pass.
var (
	ErrRequirementBlocked = errors.New("requirement blocked")
	ErrNoVerdict          = errors.New("no completion verdict")
	// ErrIncomplete: the monitor returned with stories still unfinished and
	// nothing running — escalated, failed, paused, or waiting on a dependency
	// that never arrived. The gate never ran, so this is not a pass either.
	ErrIncomplete = errors.New("stories incomplete")
)

// resumeSignals: every signal that can end resume cancels the monitor ctx —
// the completion gate's test runner lives in its own process group and is
// only killed through that ctx. SIGHUP is skipped when the process started
// with it ignored (nohup): signal.Notify would re-enable it, and closing the
// terminal would stop a run that was meant to survive it.
func resumeSignals() []os.Signal {
	sigs := []os.Signal{os.Interrupt, syscall.SIGTERM}
	if !signal.Ignored(syscall.SIGHUP) {
		sigs = append(sigs, syscall.SIGHUP)
	}
	return sigs
}

// requirementSettled reports whether a requirement whose stories are all
// complete (merged, PR submitted, awaiting approval or split) needs nothing
// more from resume: completed or archived. "blocked" is not settled — the
// operator fixes the gaps and resume re-runs the gate — and neither is a
// requirement the gate never reached a verdict on.
func requirementSettled(status string) bool {
	return status == "completed" || status == "archived"
}

// resumeResult is the state a finished run is judged on: what the projection
// says now, whether every story reached the gate, and whether a signal ended
// the run. summary renders the completion summary (a seam for the test).
type resumeResult struct {
	reqID       string
	repoDir     string
	status      string
	allComplete bool
	interrupted bool
	summary     func() (string, error)
}

// resumeOutcome prints what happened and returns the command's error.
//
//   - completed: the summary, exit 0.
//   - blocked: ErrRequirementBlocked — the gaps are on file and the mainline
//     is red.
//   - interrupted with every story complete: ErrNoVerdict. The gate was
//     running or about to; nothing certified the mainline. This does not
//     depend on the run having started gate-only — a last wave that merges
//     during the run reaches the gate too.
//   - interrupted with agents still running: a detach, not a failure. The
//     agents keep working in tmux and `vxd resume` picks them up again, so
//     the exit code stays 0.
//   - paused: dispatchNextWave returns early, so the monitor can come back
//     having run nothing. Exit 0 — `vxd resume` is the answer, not a fix.
//   - anything else: the monitor returned with stories unfinished and nothing
//     left running — a stalled pipeline (escalated, failed, or a dependency
//     that never arrived). ErrIncomplete: the gate never ran.
func resumeOutcome(out io.Writer, r resumeResult) error {
	switch {
	case r.status == "completed":
		if r.summary != nil {
			if summary, err := r.summary(); err == nil {
				fmt.Fprint(out, summary)
			} else {
				// The requirement did complete; only the report failed.
				log.Printf("[resume] could not generate the summary for %s: %v", r.reqID, err)
			}
		}
		return nil
	case r.status == "blocked":
		return fmt.Errorf("%w: requirement %s is blocked — see .vxd-fix-gaps.md in %s; fix the gaps, commit and push them, then run: vxd resume %s",
			ErrRequirementBlocked, r.reqID, r.repoDir, r.reqID)
	case r.status == "paused":
		// dispatchNextWave returns early on a paused requirement (billing
		// exhaustion), so the monitor can come back having run nothing. That
		// is not a verdict the gate failed to reach, and the answer is to
		// resume once whatever paused it is resolved.
		fmt.Fprintf(out, "%s is paused — nothing ran. `vxd resume %s` continues it once whatever paused it is resolved.\n", r.reqID, r.reqID)
		return nil
	case r.interrupted && r.allComplete:
		return fmt.Errorf("%w: the completion gate for %s was interrupted — run: vxd resume %s",
			ErrNoVerdict, r.reqID, r.reqID)
	case r.interrupted:
		fmt.Fprintf(out, "Detached from %s — the agents keep running; `vxd resume %s` picks them up again.\n", r.reqID, r.reqID)
		return nil
	case r.allComplete:
		return fmt.Errorf("%w: completion gate recorded no verdict for %s — run: vxd resume %s",
			ErrNoVerdict, r.reqID, r.reqID)
	}
	return fmt.Errorf("%w: %s has stories that are not complete and nothing left running — see `vxd status %s`",
		ErrIncomplete, r.reqID, r.reqID)
}

// prepareGateOnly decides whether this run exists only to re-run the
// completion gate, and reports when resume should stop instead.
//
// Every story is merged or split (gateOnlyReady — not the wider
// engine.IsStoryComplete, which counts an open PR) and the requirement has no
// verdict, or a red one the operator has since fixed (#138). There is nothing
// to dispatch, so resume goes to the monitor with no agents to track and its
// first tick runs the gate.
//
// With a story still awaiting its pull request the mainline is not composed
// and there is nothing honest to verify, so the gate does not run — but the
// exit code is still the one this command promises: 1 for a blocked
// requirement or one with no verdict. Stopping is not passing.
//
// A dirty working tree stops it as well: the gate pulls the base branch with
// a stash-pop around the operator's changes, so it would verify them, and a
// red verdict sends a fix agent into that same checkout.
//
// gateWired is false under --dry-run and qa.disable_completion_gate: no gate
// is built, dispatchNextWave falls through to the advisory verification, and
// that path emits REQ_COMPLETED whatever it found. Re-running it would turn a
// red REQ_BLOCKED into a completed requirement on a flag that promises to
// simulate, so the run stops at the status it found and reports it.
func prepareGateOnly(out io.Writer, s stores, in gateOnlyInput) (gateOnly, done bool, err error) {
	if !in.allComplete {
		return false, false, nil
	}
	// The status is read first: every stop below reports it, and a stop that
	// did not would exit 0 on a blocked requirement.
	req, err := s.Proj.GetRequirement(in.reqID)
	if err != nil {
		return false, true, fmt.Errorf("read requirement %s: %w", in.reqID, err)
	}
	if requirementSettled(req.Status) {
		fmt.Fprintf(out, "All stories are complete.\n")
		return false, true, nil
	}
	if !in.mainlineComposed {
		// Every story is "complete" only because an open PR counts as one.
		// `vxd approve` is the one command that moves a story on from here:
		// STORY_MERGED is emitted by Merger alone, so a pull request merged
		// on the forge is not noticed, and auto_merge does not go back for
		// one already submitted. Saying so beats advice that loops.
		fmt.Fprintf(out, "All stories are complete, but some are still awaiting their pull request — the mainline does not have their work yet, so the completion gate has nothing to verify. Run `vxd approve <story>` for one awaiting approval; a pull request merged on the forge is not detected, so a story left at pr_submitted keeps %s here until VXD merges it.\n", in.reqID)
		if req.Status == "blocked" {
			return false, true, fmt.Errorf("%w: requirement %s is blocked — see .vxd-fix-gaps.md in %s; its open pull requests have to merge before the gate can re-verify it",
				ErrRequirementBlocked, in.reqID, in.repoDir)
		}
		return false, true, fmt.Errorf("%w: %s has open pull requests and no completion verdict", ErrNoVerdict, in.reqID)
	}
	if !in.gateWired && req.Status == "blocked" {
		// The advisory path completes a requirement whatever it finds, so
		// re-running it here would turn a red verdict into a green one.
		fmt.Fprintf(out, "%s is blocked and the completion gate is not active (--dry-run or qa.disable_completion_gate): nothing can re-verify it.\n", in.reqID)
		return false, true, fmt.Errorf("%w: requirement %s is blocked — see .vxd-fix-gaps.md in %s; fix the gaps, commit and push them, then run vxd resume %s with the gate enabled",
			ErrRequirementBlocked, in.reqID, in.repoDir, in.reqID)
	}
	if in.dryRun {
		// gateWired is false whenever dryRun is, so this is the dry-run half
		// of the branch above, not a second condition. Nothing is simulated
		// into the event log: --dry-run must not write a verdict, and the gate
		// it would re-run does not exist in this run.
		fmt.Fprintf(out, "All stories are complete, but the completion gate is not active under --dry-run: %s stays %s.\n", in.reqID, req.Status)
		return false, true, fmt.Errorf("%w: the completion gate is not active under --dry-run, so nothing verified %s", ErrNoVerdict, in.reqID)
	}
	if dirtyTree(in.repoDir) {
		fmt.Fprintf(out, "%s has uncommitted changes — the gate would verify them, and the fix agent it starts on a red verdict commits and pushes what it finds in that checkout. Commit and push, or stash, then run: vxd resume %s\n", in.repoDir, in.reqID)
		if req.Status == "blocked" {
			return false, true, fmt.Errorf("%w: requirement %s is blocked; the working tree in %s is dirty, so nothing was verified",
				ErrRequirementBlocked, in.reqID, in.repoDir)
		}
		return false, true, fmt.Errorf("%w: the working tree in %s is dirty, so nothing verified %s", ErrNoVerdict, in.repoDir, in.reqID)
	}
	// A disabled gate still settles the requirement: dispatchNextWave's
	// advisory branch verifies, writes .vxd-fix-gaps.md on red and completes
	// it either way, which is what qa.disable_completion_gate documents.
	fmt.Fprintf(out, "All stories are complete; re-running the completion gate for %s (status %s).\n", in.reqID, req.Status)
	return true, false, nil
}

// gateOnlyInput is what the decision reads. A struct rather than five
// positional bools, so a call site says which is which — and gateWired is
// computed once by the caller and passed here, so "a real gate will run"
// cannot mean one thing to this decision and another to the wiring that
// builds the gate.
type gateOnlyInput struct {
	reqID       string
	repoDir     string
	allComplete bool
	// mainlineComposed: every story merged or split, so the base branch
	// actually carries their work (gateOnlyReady).
	mainlineComposed bool
	gateWired        bool
	dryRun           bool
}

// gateOnlyReady reports a requirement whose mainline is actually composed:
// every story merged, or split into the ones that were. IsStoryComplete is
// wider — with auto_merge: false it counts pr_submitted and awaiting_approval
// — and gate-only on those would verify a mainline that is missing exactly
// the work the operator gated behind review, then write into the repo on red.
//
// It governs the gate-only path only. The monitor's own all-done branch
// (dispatchNextWave) still reaches the gate on an auto_merge: false run that
// completes its last wave; narrowing that is a change to the dispatch loop.
func gateOnlyReady(stories []state.Story) bool {
	for _, story := range stories {
		if story.Status != "merged" && story.Status != "split" {
			return false
		}
	}
	return len(stories) > 0
}

// unblockForGate emits REQ_RESUMED for a blocked requirement that is about to
// have its gate re-run: the status must not read "blocked" while it runs — the
// pull removes .vxd-fix-gaps.md before the gate starts, so a Ctrl-C would
// leave "blocked" pointing at a file that is gone, and `vxd watch` treats
// blocked as terminal. It is called once the rest of the run is set up, so a
// failure on the way there leaves the verdict and the gaps file alone.
func unblockForGate(out io.Writer, s stores, reqID string, gateOnly bool) error {
	if !gateOnly {
		return nil
	}
	req, err := s.Proj.GetRequirement(reqID)
	if err != nil {
		return fmt.Errorf("read requirement %s: %w", reqID, err)
	}
	if req.Status != "blocked" {
		return nil
	}
	if err := emitReqResumed(s, reqID); err != nil {
		return err
	}
	fmt.Fprintf(out, "Unblocked %s for the completion gate.\n", reqID)
	return nil
}

// emitReqResumed appends REQ_RESUMED and projects it. Two paths emit it —
// the pause path in runResume and the gate-only unblock — and an event
// appended without being projected leaves the watermark behind the log, so
// the pair lives in one place rather than in two that can drift.
func emitReqResumed(s stores, reqID string) error {
	evt := state.NewEvent(state.EventReqResumed, "", "", map[string]any{"id": reqID})
	if err := s.Events.Append(evt); err != nil {
		return fmt.Errorf("append resume event: %w", err)
	}
	if err := s.Proj.Project(evt); err != nil {
		return fmt.Errorf("project resume event: %w", err)
	}
	return nil
}

// dirtyTree reports uncommitted work in the checkout the gate would verify.
// gitPullWithStash stashes, pulls the base branch and pops, so the tree the
// gate verifies is the operator's working copy: green there certifies code
// that is not on origin, and red starts a fix agent whose brief is to commit
// and push what it finds. Gate-only resume refuses on it rather than verify
// it.
//
// VXD's own artefacts do not count: the gaps file is there by definition on
// the flow this check exists for, and the pull appends to .gitignore.
// A repoDir that git cannot read (not a work tree, no git on PATH) is not
// evidence of a dirty tree; the pull that follows reports it properly.
func dirtyTree(repoDir string) bool {
	if repoDir == "" {
		return false
	}
	args := []string{"status", "--porcelain", "--", "."}
	for _, artifact := range engine.DirtyTreeExclusions() {
		args = append(args, ":(exclude)"+artifact)
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = repoDir
	status, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(status)) != ""
}

// allStoriesComplete re-reads the requirement's stories: whether the gate was
// reached is a fact about the state after the run, not about the flag the run
// started with (a wave that merges during the run reaches it too). A read
// that fails is returned rather than reported as "not complete", which would
// send the operator to `vxd status` for a stalled pipeline that is not there.
// A requirement with no stories is not complete.
func allStoriesComplete(proj *state.SQLiteStore, reqID string) (bool, error) {
	stories, err := proj.ListStories(state.StoryFilter{ReqID: reqID})
	if err != nil {
		return false, fmt.Errorf("list stories for %s: %w", reqID, err)
	}
	if len(stories) == 0 {
		return false, nil
	}
	for _, story := range stories {
		if !engine.IsStoryComplete(story.Status) {
			return false, nil
		}
	}
	return true, nil
}

// finishResume is the end of a run: read the outcome the monitor left behind
// and turn it into what the command prints and exits with. It lives here with
// resumeOutcome rather than at the bottom of runResume, which is long enough.
func finishResume(ctx context.Context, out io.Writer, s stores, reqID, repoDir string) error {
	// The [gate] lines go to the log only, so a run that ends at the gate
	// would otherwise say nothing at all.
	req, reqErr := s.Proj.GetRequirement(reqID)
	if reqErr != nil {
		// Not knowing whether the gate passed is not a pass.
		return fmt.Errorf("%w: could not read the outcome for %s (%v) — check `vxd status`", ErrNoVerdict, reqID, reqErr)
	}
	allComplete, completeErr := allStoriesComplete(s.Proj, reqID)
	if completeErr != nil {
		// Not knowing how the run ended is not a stalled pipeline.
		return fmt.Errorf("%w: could not read the outcome for %s (%v) — check `vxd status`", ErrNoVerdict, reqID, completeErr)
	}
	return resumeOutcome(out, resumeResult{
		reqID:       reqID,
		repoDir:     repoDir,
		status:      req.Status,
		allComplete: allComplete,
		interrupted: ctx.Err() != nil,
		summary:     func() (string, error) { return engine.GenerateSummary(s.Events, s.Proj, reqID) },
	})
}
