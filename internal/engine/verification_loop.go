package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/sanitize"
)

// VerificationResult holds the outcome of a post-completion verification cycle.
type VerificationResult struct {
	BuildPasses    bool
	TestsPassing   int
	TestsFailing   int
	TestsTotal     int
	Gaps           []VerificationGap
	CleanArtifacts bool
	DepsInstalled  bool
}

// VerificationGap describes a specific issue found during verification.
type VerificationGap struct {
	Category string // "build", "test", "wiring", "hallucination", "artifact", "dependency", "documentation"
	Severity string // "critical", "high", "medium", "low"
	File     string
	Detail   string // one line: what happened
	// Output is runner/compiler output (untrusted, possibly many lines),
	// truncated and with known secret shapes redacted (keepHead/keepTail).
	// It is rendered fenced by GapsToRequirement and buildFixPrompt, never as
	// a heading; checkTests and checkBuild log the fact of a failure, not the
	// output.
	Output string
	// Kind is the failure class, for the decisions that must not depend on
	// the wording of Detail: whether a fix agent can do anything about it
	// (gapTimeout cannot), and whether the build gap already carries the same
	// compiler output (gapCompile does).
	Kind gapKind
}

// gapKind classifies a verification gap. The zero value is a gap that needs
// no special handling.
type gapKind int

const (
	gapOther gapKind = iota
	gapBuild
	gapCompile   // the test suite does not compile
	gapEarlyExit // a package ended without reporting test-level failures
	gapTestFail
	gapSilent // a non-zero runner that reported nothing
	gapTimeout
	gapAborted
)

// RunVerificationLoop executes the post-completion verification cycle.
// It checks build, tests, artifacts, hallucinations, and dependency state.
// Returns gaps found. If gaps exist, they can be fed back as a new requirement.
//
// This implements the evaluate → fix → verify loop:
//
//	Cycle 1: Full verification after all stories merge
//	Cycle 2: Confirmation pass after fixes (lighter, just build + tests)
func RunVerificationLoop(ctx context.Context, repoDir string, cycle int) VerificationResult {
	return runVerificationLoop(ctx, repoDir, cycle, defaultBounds())
}

// runVerificationLoop is RunVerificationLoop under the bounds the completion
// gate carries.
func runVerificationLoop(ctx context.Context, repoDir string, cycle int, b verifyBounds) VerificationResult {
	log.Printf("[verify] starting verification cycle %d for %s", cycle, filepath.Base(repoDir))

	result := VerificationResult{}

	// Step 1: Ensure dependencies are installed
	result.DepsInstalled = ensureDependencies(repoDir)

	// Step 2: Check build. A red build is a critical gap carrying the build
	// output, so .vxd-fix-gaps.md and the fix agent see why.
	var buildGap *VerificationGap
	result.BuildPasses, buildGap = checkBuild(repoDir)
	if buildGap != nil {
		result.Gaps = append(result.Gaps, *buildGap)
	}

	// Step 3: Run tests. A suite that fails, fails to compile or fails to
	// start comes back as critical "test" gaps carrying the runner output —
	// one per failure class, so a compile break in one package does not hide
	// a failing test in another — and the fix agent and .vxd-fix-gaps.md see
	// the failure, not "1 failing".
	var testGaps []VerificationGap
	result.TestsPassing, result.TestsFailing, result.TestsTotal, testGaps = checkTests(ctx, repoDir, b)
	// A broken build and a test suite that will not compile are the same
	// defect twice: checkBuild already carries the compiler output, so the
	// suite's compile gap would send the fix agent the same errors again.
	if !result.BuildPasses {
		testGaps = dropGaps(testGaps, gapCompile)
	}
	result.Gaps = append(result.Gaps, testGaps...)
	if err := ctx.Err(); err != nil {
		// No verdict and no side effects: the caller records nothing for an
		// aborted run, and step 6 below would commit to the repository.
		log.Printf("[verify] cycle %d aborted (%v) — skipping the remaining checks", cycle, err)
		return result
	}

	// Step 4: Scan for hallucination artifacts
	hallucinations := scanForHallucinations(repoDir)
	for _, h := range hallucinations {
		result.Gaps = append(result.Gaps, VerificationGap{
			Category: "hallucination",
			Severity: "critical",
			File:     h,
			Detail:   "LLM reasoning text found in source file",
		})
	}

	// Step 5: Check for merge conflict markers
	conflicts := validateNoConflictMarkers(repoDir)
	for _, c := range conflicts {
		result.Gaps = append(result.Gaps, VerificationGap{
			Category: "wiring",
			Severity: "critical",
			File:     c,
			Detail:   "Unresolved merge conflict markers",
		})
	}

	// Step 6: Clean VXD workspace artifacts
	result.CleanArtifacts = cleanWorkspaceArtifacts(repoDir)

	// Step 7: Check for missing README
	if _, err := os.Stat(filepath.Join(repoDir, "README.md")); os.IsNotExist(err) {
		result.Gaps = append(result.Gaps, VerificationGap{
			Category: "documentation",
			Severity: "medium",
			File:     "README.md",
			Detail:   "No README.md found — project needs documentation",
		})
	}

	// Log summary
	log.Printf("[verify] cycle %d complete: build=%v, tests=%d/%d passing, gaps=%d",
		cycle, result.BuildPasses, result.TestsPassing, result.TestsTotal, len(result.Gaps))

	for _, g := range result.Gaps {
		log.Printf("[verify] gap [%s/%s] %s: %s", g.Category, g.Severity, g.File, g.Detail)
	}

	return result
}

// ensureDependencies runs the appropriate install command for the project.
func ensureDependencies(repoDir string) bool {
	if fileExists(filepath.Join(repoDir, "package.json")) {
		log.Printf("[verify] running npm install ...")
		cmd := exec.Command("npm", "install")
		cmd.Dir = repoDir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			log.Printf("[verify] npm install failed: %v", err)
			return false
		}
		return true
	}
	if fileExists(filepath.Join(repoDir, "go.mod")) {
		cmd := exec.Command("go", "mod", "download")
		cmd.Dir = repoDir
		if err := cmd.Run(); err != nil {
			log.Printf("[verify] go mod download failed: %v", err)
			return false
		}
		return true
	}
	return true // no deps to install
}

// checkBuild attempts to build the project; a failure comes back as a
// critical build gap whose Output is the head of the build output (the first
// errors are the useful ones; the tail is often "too many errors").
func checkBuild(repoDir string) (bool, *VerificationGap) {
	// validateBuild has already redacted and cut its output (buildOutput,
	// buildOutputBytes) — the effective bound; keepHead is the same treatment
	// once more, so the gap never carries more than a gap does.
	if err := validateBuild(repoDir); err != nil {
		log.Printf("[verify] build failed — output kept in the build gap")
		return false, &VerificationGap{Category: "build", Severity: "critical", Kind: gapBuild,
			Detail: "build failed", Output: keepHead(err.Error())}
	}
	log.Printf("[verify] build passed")
	return true, nil
}

// testEcosystem picks the test runner AND the parser for a repo once, so a
// repo with both package.json and go.mod is not run with jest and parsed as
// Go JSON (which read a green jest run as 0/0/0). go.mod wins for polyglot
// repos: the Go parser is reliable and it is the Go suite whose compile
// break this gate exists to catch; the Node line-scan parser is known to
// misread green runs (tracked separately). Running every detected ecosystem
// and summing the counts is the follow-up, together with validateBuild and
// ensureDependencies, which still prefer package.json.
func testEcosystem(repoDir string) string {
	switch {
	case fileExists(filepath.Join(repoDir, "go.mod")):
		return "go"
	case fileExists(filepath.Join(repoDir, "package.json")):
		return "node"
	}
	return ""
}

// verifyTestTimeout is the default bound on one test-suite run inside the
// completion gate (qa.completion_test_timeout_s overrides it per gate). The
// caller's ctx is a signal context that only ends on Ctrl-C, so without this a
// hung suite would hang the gate (precedent: completionFixTimeout). A suite
// that does not finish is a timeout gap (gapTimeout), which the gate blocks on
// without dispatching a fix agent: an agent cannot repair a suite that does
// not finish. Documented in docs/configuration.md (qa).
const verifyTestTimeout = 20 * time.Minute

// timeoutDetailPrefix starts the Detail of a timeout gap, and
// compileDetailPrefix the Detail of a compile gap; both are wording, and
// nothing decides on them — Kind does.
const (
	timeoutDetailPrefix = "test suite did not finish within "
	compileDetailPrefix = "test suite failed to compile"
)

// verifyWaitDelay is the default for how long Wait keeps reading the runner's
// output pipes after the runner itself exited (a grandchild that inherited the
// pipe) and after a cancel (before the group kill is reported).
const verifyWaitDelay = 5 * time.Second

// verifyBounds are the time limits one verification run works under. They
// travel with the call (the gate carries them) rather than living in package
// variables the tests mutate: a shortened delay is then an argument, and
// nothing stops these tests running in parallel.
type verifyBounds struct {
	testTimeout time.Duration // one suite run (qa.completion_test_timeout_s)
	waitDelay   time.Duration // Wait's grace for a pipe a child still holds
}

// defaultBounds is what a caller with no configuration of its own uses.
func defaultBounds() verifyBounds {
	return verifyBounds{testTimeout: verifyTestTimeout, waitDelay: verifyWaitDelay}
}

// withTestTimeout returns the bounds with d as the suite timeout; d <= 0
// keeps the default.
func (b verifyBounds) withTestTimeout(d time.Duration) verifyBounds {
	if d > 0 {
		b.testTimeout = d
	}
	return b
}

// gapOutputBytes is how much runner output a gap keeps (the tail).
const gapOutputBytes = 2000

// hasUnfixableGap reports a red result no fix agent should be dispatched for:
// a suite that did not finish within the gate's test timeout (the gap carries
// gapTimeout, so rewording its message cannot re-enable auto-fix). The
// requirement is blocked with the gap on record; the operator makes the suite
// finish and `vxd resume` re-runs the gate.
func hasUnfixableGap(res VerificationResult) bool {
	for _, g := range res.Gaps {
		if g.Kind == gapTimeout {
			return true
		}
	}
	return false
}

// dropGaps removes every gap of one kind — the suite's compile gap when the
// build already reported the same compiler output.
func dropGaps(gaps []VerificationGap, kind gapKind) []VerificationGap {
	kept := make([]VerificationGap, 0, len(gaps))
	for _, g := range gaps {
		if g.Kind == kind {
			continue
		}
		kept = append(kept, g)
	}
	return kept
}

// checkTests runs the project's test suite under the bounds the gate passes
// and returns pass/fail/total counts and the gaps that explain a red run, one
// per failure class. When the runner exits non-zero without producing
// test-level failures (the suite did not compile or start) it fails closed
// and returns a critical gap carrying the runner output so the failure is
// actionable downstream. A cancelled ctx yields no verdict: failing=1 so
// nothing is certified green, plus a gap that says the run was aborted, and
// CompletionGate.Run stops before any state-changing event.
func checkTests(ctx context.Context, repoDir string, b verifyBounds) (passing, failing, total int, gaps []VerificationGap) {
	eco := testEcosystem(repoDir)
	tctx, cancel := context.WithTimeout(ctx, b.testTimeout)
	defer cancel()
	cmd := buildTestCmd(tctx, repoDir, eco, b.waitDelay)
	if cmd == nil {
		log.Printf("[verify] no test framework detected")
		return 0, 0, 0, nil
	}
	// Keep the exit error: a test runner that fails to compile or start emits
	// no per-test failure events, so the parsed counts alone cannot distinguish
	// "0 failing" from "the suite never ran". The exit code can.
	out, runErr := cmd.CombinedOutput()
	output := string(out)
	// Whatever is left of the runner's process group must not outlive the
	// gate, whichever way the run ended: Wait reports an ExitError over
	// ErrWaitDelay, so a failing runner with a pipe-holding grandchild (a jest
	// globalSetup dev server plus a failing test) never says ErrWaitDelay —
	// the fix agent would then run against a port that is still bound. After
	// a cancel or a clean exit the group is already gone and this is a no-op.
	killProcessGroup(cmd)

	if ctx.Err() != nil {
		// runErr says whether the group kill landed (signal: killed) or the
		// WaitDelay fallback had to save the run.
		log.Printf("[verify] test run aborted: %v — no verdict (runner: %v)", ctx.Err(), runErr)
		return 0, 1, 1, []VerificationGap{{Category: "test", Severity: "critical", Kind: gapAborted,
			Detail: "test run aborted (" + ctx.Err().Error() + ") — no verdict"}}
	}
	if runErr != nil && errors.Is(tctx.Err(), context.DeadlineExceeded) {
		// runErr != nil: a deadline that expires as the runner exits 0 is a
		// finished suite, not a timeout.
		log.Printf("[verify] test run exceeded %s — treating the suite as failing", b.testTimeout)
		return 0, 1, 1, []VerificationGap{{Category: "test", Severity: "critical", Kind: gapTimeout,
			Detail: timeoutDetailPrefix + b.testTimeout.String(), Output: keepTail(runnerText(eco, output))}}
	}
	if errors.Is(runErr, exec.ErrWaitDelay) {
		// The runner exited 0; a leaked child held the pipe past WaitDelay.
		// The exit status is the verdict, the leaked process is not (it was
		// killed with the group above).
		log.Printf("[verify] test runner exited cleanly; a child held its output open past %s — its process group was killed", b.waitDelay)
		runErr = nil
	}

	res, passing, failing := parseTestOutput(eco, output, runErr)
	// A Go module with no packages (docs-only or scaffold requirements, or a
	// nested module) exits 1 with "no packages to test" while `go build`
	// exits 0: not a broken suite — but only with zero JSON events and exactly
	// that line, since test output is untrusted and must not buy a verdict.
	if eco == "go" && runErr != nil && failing == 0 && noGoPackages(output, res.events) {
		log.Printf("[verify] no Go packages to test — treating as 0/0/0")
		return 0, 0, 0, nil
	}
	gaps = explainRunnerFailure(res, failing, runErr, output, runnerText(eco, output))
	passing, failing, total = reconcileExit(passing, failing, runErr)
	log.Printf("[verify] tests: %d passing, %d failing, %d total", passing, failing, total)
	return passing, failing, total, gaps
}

// buildTestCmd is the runner for an ecosystem, or nil when there is none. It
// runs in its own process group (killed whole on cancel: go test spawns
// pkg.test binaries, npx spawns node) and stops waiting for grandchildren that
// inherited the output pipe after verifyWaitDelay.
func buildTestCmd(ctx context.Context, repoDir, eco string, waitDelay time.Duration) *exec.Cmd {
	var cmd *exec.Cmd
	switch eco {
	case "node":
		cmd = exec.CommandContext(ctx, "npx", "jest", "--passWithNoTests", "--json")
		if fileExists(filepath.Join(repoDir, "vitest.config.ts")) || fileExists(filepath.Join(repoDir, "vitest.config.js")) {
			cmd = exec.CommandContext(ctx, "npx", "vitest", "run", "--reporter=json")
		}
	case "go":
		cmd = exec.CommandContext(ctx, "go", "test", "-count=1", "-json", "./...")
	default:
		return nil
	}
	cmd.Dir = repoDir
	setProcessGroup(cmd)
	cmd.WaitDelay = waitDelay
	return cmd
}

// explainRunnerFailure turns a non-zero runner exit into the gaps the
// operator and the fix agent read — one per failure class, so a compile
// break in package B does not hide the failing test in package A (the fix
// agent would otherwise see A only on the next cycle). Detail is one line;
// the evidence (compiler output, package output, failing tests' output, and
// readable — the decoded runner output — as the last resort) goes in Output,
// truncated.
func explainRunnerFailure(res goTestParse, failing int, runErr error, output, readable string) []VerificationGap {
	if runErr == nil {
		return nil
	}
	var gaps []VerificationGap
	if res.buildFailures > 0 {
		gaps = append(gaps, compileGap(res, runErr, output))
	}
	if res.earlyExits > 0 {
		gaps = append(gaps, earlyExitGap(res, runErr, output, readable))
	}
	if n := failing - res.buildFailures - res.earlyExits; n > 0 {
		gaps = append(gaps, testFailureGap(n, res, runErr, output, readable))
	}
	if len(gaps) == 0 {
		gaps = append(gaps, silentRunnerGap(runErr, output, readable))
	}
	return gaps
}

// compileGap: a package's test binary did not build. The compiler output
// comes from build-output events; older toolchains print it as plain stderr
// lines instead, so fall back to the non-JSON lines. The head is the useful
// end of compiler output.
func compileGap(res goTestParse, runErr error, output string) VerificationGap {
	detail := res.buildOutput
	if detail == "" {
		detail = nonJSONLines(output)
	}
	return VerificationGap{Kind: gapCompile, Category: "test", Severity: "critical",
		Detail: fmt.Sprintf("%s (%d package(s); runner %s)", compileDetailPrefix, res.buildFailures, exitStatus(runErr)),
		Output: keepHead(detail)}
}

// earlyExitGap: a package ended without reporting a test-level failure
// (TestMain called os.Exit, a panic before its tests ran). Its package output
// is the evidence.
func earlyExitGap(res goTestParse, runErr error, output, readable string) VerificationGap {
	detail := res.failedPkgOutput
	if detail == "" {
		detail = nonJSONLines(output)
	}
	if detail == "" {
		detail = readable
	}
	log.Printf("[verify] test runner exited with error (%v) and %d package(s) ended without test-level failures — treating the suite as failing", runErr, res.earlyExits)
	return VerificationGap{Kind: gapEarlyExit, Category: "test", Severity: "critical",
		Detail: fmt.Sprintf("%d package(s) ended without reporting test-level failures (runner %s)", res.earlyExits, exitStatus(runErr)),
		Output: keepTail(detail)}
}

// testFailureGap: plain test failures, the most common red. The failing
// tests' own output is the evidence (Go); Node has no per-test view, so the
// runner output stands in.
func testFailureGap(n int, res goTestParse, runErr error, output, readable string) VerificationGap {
	detail := res.failedTestOutput
	if detail == "" {
		detail = nonJSONLines(output)
	}
	if detail == "" {
		detail = readable
	}
	return VerificationGap{Kind: gapTestFail, Category: "test", Severity: "critical",
		Detail: fmt.Sprintf("%d test(s) failed (runner %s)", n, exitStatus(runErr)),
		Output: keepTail(detail)}
}

// silentRunnerGap: the runner exited non-zero and reported nothing parseable
// (binary missing, or (Node) it failed to collect tests). The raw output is
// all there is.
func silentRunnerGap(runErr error, output, readable string) VerificationGap {
	log.Printf("[verify] test runner exited with error (%v) and reported no test-level failures — treating the suite as failing", runErr)
	detail := nonJSONLines(output)
	if detail == "" {
		detail = readable
	}
	return VerificationGap{Kind: gapSilent, Category: "test", Severity: "critical",
		Detail: fmt.Sprintf("test runner exited before or without reporting test-level failures (runner %s)", exitStatus(runErr)),
		Output: keepTail(detail)}
}

// reconcileExit fails closed when the runner exited non-zero yet no
// test-level failure was parsed (the suite did not compile or start).
func reconcileExit(passing, failing int, runErr error) (int, int, int) {
	if runErr != nil && failing == 0 {
		failing = 1
	}
	return passing, failing, passing + failing
}

// exitStatus renders the runner's failure for a gap: its exit status when it
// ran ("exit status 1", "signal: killed"), why it could not start (binary
// missing from PATH), or the bare error otherwise. Cancellation is handled
// before this is called. Ecosystem-neutral: it applies to go test and to
// jest/vitest alike.
func exitStatus(runErr error) string {
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return exitErr.String()
	}
	var startErr *exec.Error
	if errors.As(runErr, &startErr) {
		return "could not start: " + runErr.Error()
	}
	return "failed: " + runErr.Error()
}

// nonJSONLines returns the lines of a `go test -json` stream that are not
// JSON events — where older toolchains print compiler errors.
func nonJSONLines(output string) string {
	var b strings.Builder
	for _, l := range strings.Split(output, "\n") {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "{") {
			continue
		}
		b.WriteString(t)
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

// keepHead and keepTail are what a gap's Output holds: known secret shapes
// redacted first (runner output can echo a token from the environment or a
// fixture, and the gap is written to .vxd-fix-gaps.md and sent to the fix
// agent), then truncated to gapOutputBytes.
func keepHead(s string) string { return headOutput(sanitize.RedactSecrets(s), gapOutputBytes) }
func keepTail(s string) string { return tailOutput(sanitize.RedactSecrets(s), gapOutputBytes) }

// headOutput keeps the FIRST maxLen bytes — for compiler output the first
// errors are the useful part; the tail is often "too many errors".
func headOutput(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return strings.ToValidUTF8(s[:maxLen], "") + "…"
}

// tailOutput keeps the LAST maxLen bytes — for a hung or crashed suite the
// end of the output (the last running test, the panic) is the useful part.
// The cut can land inside a rune; the partial rune is dropped.
func tailOutput(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return "…" + strings.ToValidUTF8(s[len(s)-maxLen:], "")
}

// scanForHallucinations checks all source files for LLM preamble text.
func scanForHallucinations(repoDir string) []string {
	var found []string
	//nolint:errcheck // best-effort hallucination scan; callback handles per-path errors
	filepath.Walk(repoDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		// Skip non-source files and common dirs
		rel, _ := filepath.Rel(repoDir, path)
		if strings.Contains(rel, "node_modules") || strings.Contains(rel, ".git") ||
			strings.Contains(rel, "dist") || strings.Contains(rel, "build") {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if !isSourceExt(ext) {
			return nil
		}
		data, err := os.ReadFile(path) // #nosec G122 G304 -- best-effort read-only scan of the local repo tree; a TOCTOU race only skips a file
		if err != nil {
			return nil
		}
		firstLine := strings.TrimSpace(strings.SplitN(string(data), "\n", 2)[0])
		if isHallucinationLine(firstLine) {
			found = append(found, rel)
		}
		return nil
	})
	return found
}

// cleanWorkspaceArtifacts removes VXD temporary files from the repo.
func cleanWorkspaceArtifacts(repoDir string) bool {
	artifacts := []string{"WAVE_CONTEXT.md", "REQUIREMENT.md", ".vxd-prompts"}
	cleaned := false
	for _, name := range artifacts {
		path := filepath.Join(repoDir, name)
		if info, err := os.Stat(path); err == nil {
			if info.IsDir() {
				_ = os.RemoveAll(path) // best-effort artifact cleanup
			} else {
				_ = os.Remove(path) // best-effort artifact cleanup
			}
			log.Printf("[verify] removed artifact: %s", name)
			cleaned = true
		}
	}
	if cleaned {
		// Commit the cleanup
		addCmd := exec.Command("git", "add", "-A")
		addCmd.Dir = repoDir
		_ = addCmd.Run() // best-effort; commit below covers the failure case
		commitCmd := exec.Command("git", "commit", "-m", "chore: clean VXD workspace artifacts")
		commitCmd.Dir = repoDir
		_ = commitCmd.Run() // best-effort cleanup commit
	}
	return !cleaned // true means already clean
}

// GapsToRequirement converts verification gaps into a follow-up requirement
// that VXD can process to fix the issues.
func GapsToRequirement(gaps []VerificationGap, projectName string) string {
	if len(gaps) == 0 {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Fix Verification Gaps in %s\n\n", projectName)
	b.WriteString("The following issues were found during post-completion verification.\n")
	b.WriteString("Each must be fixed and verified.\n\n")

	for i, g := range gaps {
		fmt.Fprintf(&b, "## %d. [%s] %s\n", i+1, strings.ToUpper(g.Severity), g.Detail)
		if g.File != "" {
			fmt.Fprintf(&b, "**File:** `%s`\n", g.File)
		}
		fmt.Fprintf(&b, "**Category:** %s\n", g.Category)
		if g.Output != "" {
			b.WriteString("\n")
			renderGapOutput(&b, g.Output, "")
		}
		b.WriteString("\n")
	}

	b.WriteString("## Acceptance Criteria\n")
	b.WriteString("- All gaps resolved\n")
	b.WriteString("- Build passes\n")
	b.WriteString("- Tests pass (or test failures documented as pre-existing)\n")
	b.WriteString("- No hallucination text in source files\n")
	b.WriteString("- No merge conflict markers\n")
	b.WriteString("- README.md exists and is up to date\n")

	return b.String()
}

// fenceFor returns a backtick fence longer than any backtick run in s, so
// untrusted output cannot close it (a line of four backticks in runner output
// would otherwise end a fixed four-backtick fence and everything after it
// would read as document text — or, in the fix prompt, as instructions).
func fenceFor(s string) string {
	longest, run := 0, 0
	for _, r := range s {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return strings.Repeat("`", max(4, longest+1))
}

// renderGapOutput writes untrusted runner output inside a fence it cannot
// close, each line prefixed with indent. Used by GapsToRequirement and by the
// fix prompt so the two never drift.
func renderGapOutput(w io.Writer, output, indent string) {
	fence := fenceFor(output)
	fmt.Fprintf(w, "%s%stext\n", indent, fence)
	for _, line := range strings.Split(strings.TrimRight(output, "\n"), "\n") {
		fmt.Fprintf(w, "%s%s\n", indent, line)
	}
	fmt.Fprintf(w, "%s%s\n", indent, fence)
}

// ShouldRunFixCycle determines if a second VXD dispatch is needed based on
// verification results.
func ShouldRunFixCycle(result VerificationResult) bool {
	if !result.BuildPasses {
		return true
	}
	if result.TestsFailing > 0 {
		return true
	}
	for _, g := range result.Gaps {
		if g.Severity == "critical" || g.Severity == "high" {
			return true
		}
	}
	return false
}
