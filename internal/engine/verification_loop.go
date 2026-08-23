package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	Detail   string
}

// RunVerificationLoop executes the post-completion verification cycle.
// It checks build, tests, artifacts, hallucinations, and dependency state.
// Returns gaps found. If gaps exist, they can be fed back as a new requirement.
//
// This implements the evaluate → fix → verify loop:
//
//	Cycle 1: Full verification after all stories merge
//	Cycle 2: Confirmation pass after fixes (lighter, just build + tests)
func RunVerificationLoop(ctx context.Context, repoDir string, cycle int) VerificationResult {
	log.Printf("[verify] starting verification cycle %d for %s", cycle, filepath.Base(repoDir))

	result := VerificationResult{}

	// Step 1: Ensure dependencies are installed
	result.DepsInstalled = ensureDependencies(repoDir)

	// Step 2: Check build
	result.BuildPasses = checkBuild(repoDir)

	// Step 3: Run tests
	result.TestsPassing, result.TestsFailing, result.TestsTotal = checkTests(repoDir)

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

// checkBuild attempts to build the project.
func checkBuild(repoDir string) bool {
	if err := validateBuild(repoDir); err != nil {
		log.Printf("[verify] build failed: %v", err)
		return false
	}
	log.Printf("[verify] build passed")
	return true
}

// checkTests runs the project's test suite and returns pass/fail/total counts.
func checkTests(repoDir string) (passing, failing, total int) {
	var cmd *exec.Cmd

	if fileExists(filepath.Join(repoDir, "package.json")) {
		cmd = exec.Command("npx", "jest", "--passWithNoTests", "--json")
		// Also try vitest
		if fileExists(filepath.Join(repoDir, "vitest.config.ts")) || fileExists(filepath.Join(repoDir, "vitest.config.js")) {
			cmd = exec.Command("npx", "vitest", "run", "--reporter=json")
		}
	} else if fileExists(filepath.Join(repoDir, "go.mod")) {
		cmd = exec.Command("go", "test", "-count=1", "-json", "./...")
	}

	if cmd == nil {
		log.Printf("[verify] no test framework detected")
		return 0, 0, 0
	}

	cmd.Dir = repoDir
	out, runErr := cmd.CombinedOutput()
	output := string(out)

	if fileExists(filepath.Join(repoDir, "go.mod")) {
		return parseGoTestJSON(output)
	}

	return parseNodeTestOutput(output, runErr != nil)
}

// parseNodeTestOutput scores jest/vitest output. The counting is intentionally
// crude (it keys off the presence of the result-JSON fields), but it carries
// one safety property the completion gate depends on: runFailed reports whether
// the runner process exited non-zero. A runner that ABORTS before emitting any
// result JSON — a broken merged jest.config/vitest.config, a setup module that
// throws at load, an unresolvable runner — produces none of the signals below,
// so passing/failing both stay 0. checkBuild does NOT execute the test runner
// (it runs `npm run build` / `tsc --noEmit`, and build tsconfigs typically
// exclude test files), so without the runFailed guard the completion gate would
// report GREEN on a suite that never ran — the same false-green class as the Go
// build-fail bug. The guard can only ever move the verdict toward RED
// (fail-closed); it never manufactures a false GREEN.
func parseNodeTestOutput(output string, runFailed bool) (passing, failing, total int) {
	// Parse test results (simplified — count PASS/FAIL lines)
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "\"numPassedTests\"") || strings.Contains(line, "PASS:") {
			passing++
		}
		if strings.Contains(line, "\"numFailedTests\"") || strings.Contains(line, "FAIL:") {
			failing++
		}
	}

	// Fallback: parse Jest summary line
	if strings.Contains(output, "Tests:") {
		for _, line := range strings.Split(output, "\n") {
			if strings.Contains(line, "Tests:") && strings.Contains(line, "passed") {
				// Parse "Tests: X failed, Y passed, Z total"
				_, _ = fmt.Sscanf(line, "Tests: %d failed, %d passed, %d total", &failing, &passing, &total) // partial parse keeps zero counters
				break
			}
		}
	}

	// A non-zero runner exit that produced no parseable result means the suite
	// did not run — count it as a failure so ShouldRunFixCycle (TestsFailing > 0)
	// fires instead of completing GREEN on an unexecuted suite. Bounded to the
	// zero/zero case so a clean run (exit 0) or a run that emitted counts is
	// never disturbed.
	if runFailed && passing == 0 && failing == 0 {
		failing = 1
	}

	total = passing + failing
	log.Printf("[verify] tests: %d passing, %d failing, %d total", passing, failing, total)
	return passing, failing, total
}

func parseGoTestJSON(output string) (passing, failing, total int) {
	buildFailed := false
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var evt struct {
			Action      string `json:"Action"`
			Test        string `json:"Test"`
			FailedBuild string `json:"FailedBuild"`
		}
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			continue
		}
		// A test-file compile failure surfaces only as package-scoped events with
		// no Test field: a "build-fail" action, or a package-level "fail" carrying
		// FailedBuild. `go build ./...` (checkBuild) does NOT compile _test.go
		// files, so these are the ONLY signal that the test binary is broken. If
		// we skip them the completion gate reports 0 failing and marks the
		// requirement GREEN on a suite that does not even compile — the exact
		// "shipped on a red mainline" class the gate exists to prevent.
		if evt.Action == "build-fail" || evt.FailedBuild != "" {
			buildFailed = true
			continue
		}
		if evt.Test == "" {
			continue
		}
		switch evt.Action {
		case "pass":
			passing++
		case "fail":
			failing++
		}
	}
	// Surface an un-compilable test suite as at least one failure so
	// ShouldRunFixCycle (TestsFailing > 0) fires even when no individual test
	// could run. Only bump when nothing else already counted as failing.
	if buildFailed && failing == 0 {
		failing = 1
	}
	total = passing + failing
	log.Printf("[verify] tests: %d passing, %d failing, %d total", passing, failing, total)
	return passing, failing, total
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
		fmt.Fprintf(&b, "**Category:** %s\n\n", g.Category)
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
