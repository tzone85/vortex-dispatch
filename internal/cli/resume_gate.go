package cli

import (
	"errors"
	"fmt"
	"io"
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
//     during the run reaches the gate too, and SIGTERM there used to kill the
//     process with a non-zero status, which resumeSignals turned into a clean
//     exit.
//   - interrupted with agents still running: a detach, not a failure. The
//     agents keep working in tmux and `vxd resume` picks them up again, so
//     the exit code stays 0.
//   - anything else: the monitor returned with stories unfinished and nothing
//     left running — a stalled pipeline (escalated, failed, paused, or a
//     dependency that never arrived). ErrIncomplete: the gate never ran.
func resumeOutcome(out io.Writer, r resumeResult) error {
	switch {
	case r.status == "completed":
		if r.summary != nil {
			if summary, err := r.summary(); err == nil {
				fmt.Fprint(out, summary)
			}
		}
		return nil
	case r.status == "blocked":
		return fmt.Errorf("%w: requirement %s is blocked — see .vxd-fix-gaps.md in %s; fix the gaps, commit and push them, then run: vxd resume %s",
			ErrRequirementBlocked, r.reqID, r.repoDir, r.reqID)
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
// Every story is complete (merged, PR submitted, awaiting approval or split —
// engine.IsStoryComplete) but the requirement has no verdict, or a red one the
// operator has since fixed (#138). There is nothing to dispatch, so resume
// goes to the monitor with no agents to track and its first tick runs the
// gate.
//
// gateWired is false under --dry-run and qa.disable_completion_gate: no gate
// is built, dispatchNextWave falls through to the advisory verification, and
// that path emits REQ_COMPLETED whatever it found. Re-running it would turn a
// red REQ_BLOCKED into a completed requirement on a flag that promises to
// simulate, so the run stops at the status it found and reports it.
func prepareGateOnly(out io.Writer, s stores, reqID string, allComplete, gateWired, dryRun bool, repoDir string) (gateOnly, done bool, err error) {
	if !allComplete {
		return false, false, nil
	}
	req, err := s.Proj.GetRequirement(reqID)
	if err != nil {
		return false, true, fmt.Errorf("read requirement %s: %w", reqID, err)
	}
	if requirementSettled(req.Status) {
		fmt.Fprintf(out, "All stories are complete.\n")
		return false, true, nil
	}
	if !gateWired && req.Status == "blocked" {
		// The advisory path completes a requirement whatever it finds, so
		// re-running it here would turn a red verdict into a green one.
		fmt.Fprintf(out, "%s is blocked and the completion gate is not active (--dry-run or qa.disable_completion_gate): nothing can re-verify it.\n", reqID)
		return false, true, fmt.Errorf("%w: requirement %s is blocked — see .vxd-fix-gaps.md in %s; fix the gaps, commit and push them, then run vxd resume %s with the gate enabled",
			ErrRequirementBlocked, reqID, repoDir, reqID)
	}
	if !gateWired && dryRun {
		// Nothing is simulated into the event log: --dry-run must not write a
		// verdict, and the gate it would re-run does not exist in this run.
		fmt.Fprintf(out, "All stories are complete, but the completion gate is not active under --dry-run: %s stays %s.\n", reqID, req.Status)
		return false, true, fmt.Errorf("%w: the completion gate is not active under --dry-run, so nothing verified %s", ErrNoVerdict, reqID)
	}
	// A disabled gate still settles the requirement: dispatchNextWave's
	// advisory branch verifies, writes .vxd-fix-gaps.md on red and completes
	// it either way, which is what qa.disable_completion_gate documents.
	fmt.Fprintf(out, "All stories are complete; re-running the completion gate for %s (status %s).\n", reqID, req.Status)
	warnDirtyTree(out, repoDir)
	return true, false, nil
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
	evt := state.NewEvent(state.EventReqResumed, "", "", map[string]any{"id": reqID})
	if err := s.Events.Append(evt); err != nil {
		return fmt.Errorf("append resume event: %w", err)
	}
	if err := s.Proj.Project(evt); err != nil {
		return fmt.Errorf("project resume event: %w", err)
	}
	fmt.Fprintf(out, "Unblocked %s for the completion gate.\n", reqID)
	return nil
}

// warnDirtyTree says so when the gate is about to verify uncommitted work.
// The gate pulls the base branch with a stash-pop around it, so the tree it
// verifies is the operator's working copy — green there says nothing about
// what is on origin.
func warnDirtyTree(out io.Writer, repoDir string) {
	if repoDir == "" {
		return
	}
	// VXD's own artefacts do not count: the gaps file is there by definition
	// on the flow this note exists for, and the pull appends to .gitignore.
	args := []string{"status", "--porcelain", "--", "."}
	for _, artifact := range engine.DirtyTreeExclusions() {
		args = append(args, ":(exclude)"+artifact)
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = repoDir
	status, err := cmd.Output()
	if err != nil || len(strings.TrimSpace(string(status))) == 0 {
		return
	}
	fmt.Fprintf(out, "Note: %s has uncommitted changes — the gate verifies the working tree, not what is on the base branch. Commit and push before relying on a green verdict.\n", repoDir)
}

// allStoriesComplete re-reads the requirement's stories: whether the gate was
// reached is a fact about the state after the run, not about the flag the run
// started with (a wave that merges during the run reaches it too).
func allStoriesComplete(proj *state.SQLiteStore, reqID string) bool {
	stories, err := proj.ListStories(state.StoryFilter{ReqID: reqID})
	if err != nil || len(stories) == 0 {
		return false
	}
	for _, story := range stories {
		if !engine.IsStoryComplete(story.Status) {
			return false
		}
	}
	return true
}
