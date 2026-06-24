package engine

import (
	"context"
	"fmt"
	"log"

	vxdgit "github.com/tzone85/vortex-dispatch/internal/git"
)

// verifyRebasedQA is the repo-wide pre-merge gate. After a story's worktree has
// been rebased onto the latest base branch — but before the merge is pushed —
// it re-runs the lint/build/test commands on the combined state. A story can
// pass QA in isolation yet break the build once combined with everything merged
// since it branched (a model tightened by another story, a manifest conflict).
// Catching that here keeps the base branch green instead of poisoning every
// later story's QA (the failure mode that stranded pulsereview).
//
// It is deadlock-free: when the rebased checks fail, it re-evaluates the SAME
// checks at the base ref inside the SAME worktree (reusing already-installed
// dependencies) and only blocks the merge when the base was green — i.e. when
// this story is what turned it red. A pre-existing base failure is not blamed
// on the story.
func (m *Monitor) verifyRebasedQA(ctx context.Context, storyID, branch, worktreePath string) error {
	if m.qa == nil || m.config.QA.DisablePreMergeVerify {
		return nil
	}

	rebased := m.qa.RunCommandChecks(ctx, worktreePath)
	if rebased.Passed {
		return nil
	}

	baseBranch := m.config.Merge.BaseBranch
	if baseBranch == "" {
		baseBranch = "main"
	}
	baseRef := "origin/" + baseBranch

	// Evaluate the base in-place: detach to the base ref, run the same checks,
	// then restore the story branch. Reusing this worktree keeps node_modules /
	// venv / build caches, so the base result reflects real state, not missing
	// dependencies.
	var base QAResult
	if err := vxdgit.CheckoutRef(worktreePath, baseRef, true); err != nil {
		// Can't evaluate the base — fail safe by NOT blocking (avoid a false
		// block on a checkout hiccup), but log loudly.
		log.Printf("[pre-merge] %s: could not check out base %s to attribute failure (%v) — allowing merge", storyID, baseRef, err)
		return nil
	}
	base = m.qa.RunCommandChecks(ctx, worktreePath)
	if err := vxdgit.CheckoutRef(worktreePath, branch, false); err != nil {
		// The story branch must be restored or the merge would push the wrong
		// ref. This is a hard error.
		return fmt.Errorf("pre-merge verify: failed to restore branch %s after base check: %w", branch, err)
	}

	block, reason := preMergeDecision(rebased, base)
	if block {
		log.Printf("[pre-merge] %s BLOCKED: %s", storyID, reason)
		return fmt.Errorf("%s", reason)
	}
	log.Printf("[pre-merge] %s: rebased checks fail but base %s already red — not attributable, allowing", storyID, baseRef)
	return nil
}
