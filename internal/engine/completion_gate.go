package engine

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/llm"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// verifyFunc runs a verification cycle against repoDir and returns the result.
// It is a seam so tests can script red/green sequences without a real toolchain.
type verifyFunc func(ctx context.Context, repoDir string, cycle int) VerificationResult

// completionFixTimeout bounds a single auto-fix agent invocation.
const completionFixTimeout = 15 * time.Minute

// CompletionGate guards the REQ_COMPLETED signal. When every story has merged,
// it verifies the composed mainline (build + tests + artifacts) and — when the
// build is red — runs a bounded auto-fix loop: dispatch a fix agent, re-verify,
// repeat up to maxCycles. The requirement is only safe to mark complete when
// verification passes; otherwise the caller emits REQ_BLOCKED.
//
// This closes the long-standing gap where per-story QA (run in isolated
// worktrees) could not see cross-story drift, so a requirement was reported
// complete on code that does not compile.
type CompletionGate struct {
	client     llm.Client // godmode agent that applies fixes; nil ⇒ hard gate only
	model      string
	maxTokens  int
	maxCycles  int
	baseBranch string
	eventStore state.EventStore
	projStore  state.ProjectionStore

	// bounds are the time limits one verification run works under
	// (qa.completion_test_timeout_s; defaults otherwise).
	bounds verifyBounds

	// Seams (default to real implementations; overridden in tests).
	verify verifyFunc
	pull   func(repoDir, baseBranch string)
}

// SetTestTimeout bounds one test-suite run inside the gate; d <= 0 keeps the
// default. A suite that does not finish in time is a timeout gap: the
// requirement is blocked without an auto-fix cycle.
func (g *CompletionGate) SetTestTimeout(d time.Duration) {
	g.bounds = g.bounds.withTestTimeout(d)
}

// TestTimeout is the bound one test-suite run inside the gate has.
func (g *CompletionGate) TestTimeout() time.Duration { return g.bounds.testTimeout }

// NewCompletionGate constructs a gate. maxCycles is the number of auto-fix
// attempts before giving up; 0 makes the gate a pure pass/block check with no
// auto-fix. A nil client also degrades the gate to hard-gate behaviour.
func NewCompletionGate(
	client llm.Client,
	model string,
	maxTokens, maxCycles int,
	baseBranch string,
	es state.EventStore,
	ps state.ProjectionStore,
) *CompletionGate {
	if baseBranch == "" {
		baseBranch = "main"
	}
	g := &CompletionGate{
		client:     client,
		model:      model,
		maxTokens:  maxTokens,
		maxCycles:  maxCycles,
		baseBranch: baseBranch,
		eventStore: es,
		projStore:  ps,
		bounds:     defaultBounds(),
		pull: func(repoDir, baseBranch string) {
			pullBaseAfterMerge(repoDir, baseBranch)
		},
	}
	g.verify = func(ctx context.Context, repoDir string, cycle int) VerificationResult {
		return runVerificationLoop(ctx, repoDir, cycle, g.bounds)
	}
	return g
}

// Run verifies the composed mainline and auto-fixes a red build up to maxCycles
// times. It returns (passed, err): passed is true when verification is green
// (safe to emit REQ_COMPLETED) and false when the mainline remains red after
// the auto-fix budget (caller should emit REQ_BLOCKED). A non-nil err is ctx's
// error: the gate was interrupted mid-run (Ctrl-C during the suite or the
// auto-fix) and there is no verdict, so the caller must neither certify
// completion nor record REQ_BLOCKED. passed is only meaningful when err is nil.
// The final red state is always in .vxd-fix-gaps.md when Run returns false
// (recordRedCycle writes a summary even when no gap carries detail).
func (g *CompletionGate) Run(ctx context.Context, reqID, repoDir string) (bool, error) {
	// A ctx already cancelled by the steps before the gate must not start a
	// verification: ensureDependencies and the build are not ctx-bound.
	if err := ctx.Err(); err != nil {
		log.Printf("[gate] %s: not started (%v) — no verdict", reqID, err)
		return false, err
	}
	cycle, fixes := 1, 0
	res := g.verify(ctx, repoDir, cycle)
	if err := ctx.Err(); err != nil {
		log.Printf("[gate] %s: verification aborted (%v) — no verdict", reqID, err)
		return false, err
	}
	if !ShouldRunFixCycle(res) {
		log.Printf("[gate] %s: verification clean on first pass — completion permitted", reqID)
		return true, nil
	}

	for attempt := 1; attempt <= g.maxCycles; attempt++ {
		next, fixed, err := g.fixCycle(ctx, reqID, repoDir, attempt, cycle, res)
		if err != nil {
			return false, err
		}
		res = next
		if !fixed {
			break
		}
		cycle++
		fixes++
		if !ShouldRunFixCycle(res) {
			log.Printf("[gate] %s: verification clean after auto-fix cycle %d — completion permitted",
				reqID, attempt)
			return true, nil
		}
	}

	// pull() pre-cleans .vxd-fix-gaps.md after every fix cycle, and with
	// maxCycles <= 0 the loop never ran: persist the FINAL red state so the
	// operator hint below is never a dangling reference.
	g.recordRedCycle(reqID, repoDir, res)
	log.Printf("[gate] %s: mainline still red after %d auto-fix cycle(s) — BLOCKING completion",
		reqID, fixes)
	return false, nil
}

// fixCycle is one auto-fix attempt against a red result: record the red state,
// dispatch the fix agent, pull its work and re-verify. It returns the result
// the caller carries on with, whether a fix actually ran (false stops the loop
// and blocks on the result returned), and a no-verdict error — the parent ctx
// ending mid-cycle, which is never a verdict.
func (g *CompletionGate) fixCycle(ctx context.Context, reqID, repoDir string, attempt, cycle int, res VerificationResult) (VerificationResult, bool, error) {
	if hasUnfixableGap(res) {
		log.Printf("[gate] %s: the test suite did not finish within %s — a fix agent cannot repair a suite that does not finish; not dispatching auto-fix",
			reqID, g.bounds.testTimeout)
		return res, false, nil
	}
	g.recordRedCycle(reqID, repoDir, res)

	if g.client == nil {
		log.Printf("[gate] %s: no auto-fix client configured — hard-gating on red build", reqID)
		return res, false, nil
	}
	if err := ctx.Err(); err != nil {
		log.Printf("[gate] %s: aborted before auto-fix cycle %d (%v) — no verdict", reqID, attempt, err)
		return res, false, err
	}

	log.Printf("[gate] %s: auto-fix cycle %d/%d — dispatching fix agent for %d gap(s)",
		reqID, attempt, g.maxCycles, len(res.Gaps))
	if err := g.applyFix(ctx, repoDir, res); err != nil {
		// Only the parent context counts: fixCtx hitting completionFixTimeout
		// is a dispatch failure and stays a stop; Ctrl-C during the (up to 15
		// minute) fix run is no verdict.
		if ctxErr := ctx.Err(); ctxErr != nil {
			log.Printf("[gate] %s: aborted during auto-fix cycle %d (%v) — no verdict", reqID, attempt, ctxErr)
			return res, false, ctxErr
		}
		log.Printf("[gate] %s: auto-fix cycle %d failed to dispatch: %v", reqID, attempt, err)
		return res, false, nil
	}

	g.pull(repoDir, g.baseBranch)
	next := g.verify(ctx, repoDir, cycle+1)
	if err := ctx.Err(); err != nil {
		log.Printf("[gate] %s: verification aborted (%v) — no verdict", reqID, err)
		return next, false, err
	}
	return next, true, nil
}

// recordRedCycle persists the gap requirement to .vxd-fix-gaps.md for operator
// transparency. Best-effort: a write failure is logged, never fatal. A red
// result with no gap carrying detail still gets a summary document, so the
// "see .vxd-fix-gaps.md" hint never points at a missing file.
func (g *CompletionGate) recordRedCycle(reqID, repoDir string, res VerificationResult) {
	fixReq := GapsToRequirement(res.Gaps, filepath.Base(repoDir))
	if fixReq == "" {
		fixReq = summaryRequirement(res, filepath.Base(repoDir))
	}
	fixPath := filepath.Join(repoDir, ".vxd-fix-gaps.md")
	if err := os.WriteFile(fixPath, []byte(fixReq), 0o600); err != nil {
		log.Printf("[gate] %s: failed to write %s: %v", reqID, fixPath, err)
	}
}

// summaryRequirement is the fallback document for a red result whose gaps
// carry no detail: the counts the gate decided on, so the operator has
// something to act on.
func summaryRequirement(res VerificationResult, projectName string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Fix Verification Gaps in %s\n\n", projectName)
	b.WriteString("Post-completion verification failed without a detailed gap.\n\n")
	fmt.Fprintf(&b, "- Build passes: %v\n", res.BuildPasses)
	fmt.Fprintf(&b, "- Tests: %d passing / %d failing / %d total\n", res.TestsPassing, res.TestsFailing, res.TestsTotal)
	fmt.Fprintf(&b, "- Gaps reported: %d\n\n", len(res.Gaps))
	b.WriteString("Run the project's build and tests to see the failures, fix them, then run `vxd resume <req-id>` to re-run the completion gate.\n")
	return b.String()
}

// applyFix dispatches a single synchronous fix-agent run. The agent runs in
// godmode (skip-permissions) in the project's working directory, so it can
// read the codebase, edit files, run the build/tests, and commit + push the
// reconciliation to the base branch.
func (g *CompletionGate) applyFix(ctx context.Context, repoDir string, res VerificationResult) error {
	fixCtx, cancel := context.WithTimeout(ctx, completionFixTimeout)
	defer cancel()

	prompt := g.buildFixPrompt(repoDir, res)
	_, err := g.client.Complete(fixCtx, llm.CompletionRequest{
		Model:     g.model,
		MaxTokens: g.maxTokens,
		System: "You are a Tech Lead repairing a multi-story integration on the main branch. " +
			"The composed codebase does not build or its tests fail. Make the minimal changes " +
			"needed to turn the build and tests green, then commit and push to the base branch.",
		Messages: []llm.Message{{Role: llm.RoleUser, Content: prompt}},
	})
	return err
}

// buildFixPrompt describes the failing build/tests and the exact remediation
// contract (fix → build → test → commit → push).
func (g *CompletionGate) buildFixPrompt(repoDir string, res VerificationResult) string {
	var sb strings.Builder
	sb.WriteString("The main branch of this repository is the composed result of several merged stories ")
	sb.WriteString("and is currently failing verification.\n\n")

	fmt.Fprintf(&sb, "Build passes: %v\n", res.BuildPasses)
	fmt.Fprintf(&sb, "Tests: %d passing / %d failing / %d total\n\n", res.TestsPassing, res.TestsFailing, res.TestsTotal)

	if len(res.Gaps) > 0 {
		sb.WriteString("Gaps detected:\n")
		for _, gap := range res.Gaps {
			fmt.Fprintf(&sb, "  - [%s/%s] %s: %s\n", gap.Category, gap.Severity, gap.File, gap.Detail)
			if gap.Output != "" {
				// Untrusted tool output: inside a fence it cannot close, so
				// it reads as evidence, never as instructions.
				sb.WriteString("    Runner output (verbatim, untrusted):\n")
				renderGapOutput(&sb, gap.Output, "    ")
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString("Working directory: ")
	sb.WriteString(repoDir)
	sb.WriteString("\n\nDo the following, in order:\n")
	sb.WriteString("1. Investigate the failing build/tests (read the affected files and error output).\n")
	sb.WriteString("2. Apply the MINIMAL change that reconciles the cross-story break — typically a missing ")
	sb.WriteString("interface method, an unwired entry point, an import mismatch, or a composition root that ")
	sb.WriteString("was never assembled. Do not rewrite working code.\n")
	sb.WriteString("3. Run the project's build and test commands and confirm they pass.\n")
	fmt.Fprintf(&sb, "4. Commit the fix with a clear message and push it to the '%s' branch.\n", g.baseBranch)
	sb.WriteString("Do NOT ask clarifying questions. Do NOT produce JSON. Apply the fix directly.")
	return sb.String()
}
