package engine

// The test-runner output parsers behind checkTests: the go test -json event
// stream (per-package build failures, early exits, failing tests, the legacy
// "[build failed]" line, output kept per failing test) and the Node line scan.
// Split out of verification_loop.go, which holds the loop and the gaps.

import (
	"encoding/json"
	"fmt"
	"strings"
)

// parseTestOutput runs the ecosystem's parser; res is zero for Node, whose
// line-scan parser has no package view. When the runner exited 0 the exit
// status vetoes what the output seems to say (the same principle as
// ErrWaitDelay): for Go, legacy "[build failed]" lines that are the only
// build failures; for Node, the whole line scan, which cannot read a green
// --json summary (jest prints "numPassedTests" and "numFailedTests":0 on one
// line, so it counts a failure — #136).
func parseTestOutput(eco, output string, runErr error) (res goTestParse, passing, failing int) {
	if eco == "go" {
		res = parseGoTestOutput(output)
		if runErr == nil && res.legacyBuild > 0 && res.legacyBuild == res.buildFailures {
			res.failing -= res.legacyBuild
			res.buildFailures, res.legacyBuild = 0, 0
		}
		return res, res.passing, res.failing
	}
	passing, failing, _ = parseNodeTestOutput(output)
	if runErr == nil {
		// jest and vitest both exit non-zero on a failing test, so a clean
		// exit is the verdict and the line scan is not.
		failing = 0
	}
	return res, passing, failing
}

// runnerText is the runner output as a person would read it: for `go test
// -json`, the decoded Output fields, plus any line that is not an event. The
// secret patterns match the shapes a test prints ("password": "…"); in the
// raw stream those are JSON-escaped (\"password\": …) and no pattern sees
// them, so every gap that falls back to the runner's own output decodes it
// first. Other ecosystems already emit plain text.
func runnerText(eco, output string) string {
	if eco != "go" {
		return output
	}
	var b strings.Builder
	for _, l := range strings.Split(output, "\n") {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		var e goTestEvent
		// Action is what makes a JSON object an event; a bare JSON line a
		// test printed is kept as it is, so the patterns still see it.
		if strings.HasPrefix(t, "{") && json.Unmarshal([]byte(t), &e) == nil && e.Action != "" {
			b.WriteString(e.Output)
			continue
		}
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return b.String()
}

// noGoPackages reports the "module with no Go packages" case: the runner
// emitted no JSON events and printed exactly the toolchain's line.
func noGoPackages(output string, events int) bool {
	if events != 0 {
		return false
	}
	for _, l := range strings.Split(output, "\n") {
		if strings.TrimSpace(l) == "no packages to test" {
			return true
		}
	}
	return false
}

// parseNodeTestOutput derives counts from jest/vitest output. It is a
// best-effort line scan; a runner that exits non-zero with no parseable
// results is handled by reconcileExit in checkTests.
func parseNodeTestOutput(output string) (passing, failing, total int) {
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

	return passing, failing, passing + failing
}

// goTestParse is what parseGoTestOutput extracts from `go test -json` output.
type goTestParse struct {
	passing, failing int
	buildFailures    int    // packages whose test binary did not build (counted in failing)
	earlyExits       int    // packages that failed with no test-level failure and no build failure (counted in failing)
	events           int    // JSON events decoded (0 = the runner never got going)
	legacyBuild      int    // build failures known only from a legacy "[build failed]" output line (subset of buildFailures)
	buildOutput      string // compiler output from build-output events, for the gap detail
	failedPkgOutput  string // package-level output of packages that ended early
	failedTestOutput string // output of the tests that failed, in order
}

// goTestEvent is the subset of a `go test -json` event the parser reads.
type goTestEvent struct {
	Action      string `json:"Action"`
	Test        string `json:"Test"`
	Package     string `json:"Package"`
	ImportPath  string `json:"ImportPath"`
	Output      string `json:"Output"`
	FailedBuild string `json:"FailedBuild"`
}

// pkg is the bare package an event belongs to. The toolchain stamps
// "build-fail" with ImportPath ("p [p.test]", or "p_test [p.test]" for an
// external test package) and the paired package "fail" with Package ("p").
func (e goTestEvent) pkg() string {
	if e.Package != "" {
		return e.Package
	}
	return buildFailPkg(e.ImportPath)
}

// isBuildFailure: the modern "build-fail" action, or the package "fail" that
// carries FailedBuild. Because `go build ./...` never compiles _test.go
// files, these are the only place a test-compilation failure becomes visible.
func (e goTestEvent) isBuildFailure() bool {
	return e.Action == "build-fail" || (e.Action == "fail" && e.FailedBuild != "")
}

// isLegacyBuildFailLine: older toolchains have neither build-fail nor
// FailedBuild; a test-compile failure surfaces only as the package-level
// output line "FAIL\tpkg [build failed]" (or "[setup failed]"). The match is
// anchored to that exact shape — package output is untrusted, and a test
// that merely prints "[build failed]" must not turn a green suite red.
func (e goTestEvent) isLegacyBuildFailLine() bool {
	if e.Action != "output" || e.Test != "" {
		return false
	}
	t := strings.TrimRight(e.Output, "\n")
	return strings.HasPrefix(t, "FAIL\t") &&
		(strings.HasSuffix(t, "[build failed]") || strings.HasSuffix(t, "[setup failed]"))
}

// buildFailPkg maps the ImportPath of a build failure to the package it
// belongs to: "p_test [p.test]" and "p [p.test]" both map to "p" (an external
// test package fails alongside its package and must count once); a plain
// "p" stays "p".
func buildFailPkg(importPath string) string {
	if i := strings.Index(importPath, " ["); i >= 0 {
		return strings.TrimSuffix(strings.TrimSuffix(importPath[i+2:], "]"), ".test")
	}
	return strings.TrimSpace(importPath)
}

// pkgTracker holds the per-package state of one parse: which packages
// already count as failed (dedupes build-fail / FailedBuild / the legacy
// line, and stops a package from counting twice), how many test-level
// failures each package reported, and each package's own output (a panic
// before any test ran, os.Exit in TestMain), surfaced when it ends early.
type pkgTracker struct {
	failed        map[string]bool
	testFails     map[string]int
	out           map[string]*strings.Builder
	testOut       map[string]*strings.Builder // per (package, test) output, flushed on a test-level fail
	buildOut      strings.Builder
	failedOut     strings.Builder
	failedTestOut strings.Builder
}

func newPkgTracker() *pkgTracker {
	return &pkgTracker{failed: map[string]bool{}, testFails: map[string]int{},
		out: map[string]*strings.Builder{}, testOut: map[string]*strings.Builder{}}
}

// buildFailure counts pkg once; legacy marks a failure known only from the
// legacy output line, which checkTests lets an exit status of 0 veto.
func (p *pkgTracker) buildFailure(r *goTestParse, pkg string, legacy bool) {
	if pkg != "" && p.failed[pkg] {
		return
	}
	if pkg != "" {
		p.failed[pkg] = true
	}
	r.failing++
	r.buildFailures++
	if legacy {
		r.legacyBuild++
	}
}

// testEvent counts test-level pass/fail and keeps each test's own output so
// the failing tests' output can be surfaced as the gap's evidence.
func (p *pkgTracker) testEvent(r *goTestParse, e goTestEvent) {
	key := e.Package + "\x00" + e.Test
	switch e.Action {
	case "output":
		b := p.testOut[key]
		if b == nil {
			b = &strings.Builder{}
			p.testOut[key] = b
		}
		b.WriteString(e.Output)
	case "pass":
		r.passing++
		delete(p.testOut, key)
	case "skip":
		delete(p.testOut, key)
	case "fail":
		r.failing++
		p.testFails[e.Package]++
		if b := p.testOut[key]; b != nil {
			p.failedTestOut.WriteString(b.String())
			delete(p.testOut, key)
		}
	}
}

func (p *pkgTracker) output(pkg, text string) {
	b := p.out[pkg]
	if b == nil {
		b = &strings.Builder{}
		p.out[pkg] = b
	}
	b.WriteString(text)
}

// packagePass frees a passed package's output: only a failed package's
// output is ever surfaced, and a large suite's output is held until the end
// of the parse otherwise.
func (p *pkgTracker) packagePass(pkg string) {
	delete(p.out, pkg)
}

// packageFail: a package-level fail with no build failure and no test-level
// failure is an early exit; it counts as one failure and its output is kept.
func (p *pkgTracker) packageFail(r *goTestParse, pkg string) {
	if p.failed[pkg] || p.testFails[pkg] > 0 {
		return
	}
	p.failed[pkg] = true
	r.failing++
	r.earlyExits++
	if b := p.out[pkg]; b != nil {
		p.failedOut.WriteString(b.String())
	}
}

func parseGoTestOutput(output string) goTestParse {
	var r goTestParse
	tr := newPkgTracker()
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var evt goTestEvent
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			continue
		}
		r.events++
		switch {
		case evt.Action == "build-output":
			// Compiler output, collected whole; the gap keeps its head.
			tr.buildOut.WriteString(evt.Output)
		case evt.isBuildFailure():
			tr.buildFailure(&r, evt.pkg(), false)
		case evt.isLegacyBuildFailLine():
			tr.buildFailure(&r, evt.pkg(), true)
		case evt.Test != "":
			tr.testEvent(&r, evt)
		case evt.Action == "output" && evt.Package != "":
			tr.output(evt.Package, evt.Output)
		case evt.Action == "fail" && evt.Package != "":
			tr.packageFail(&r, evt.Package)
		case evt.Action == "pass" && evt.Package != "":
			tr.packagePass(evt.Package)
		}
	}
	r.buildOutput = strings.TrimSpace(tr.buildOut.String())
	r.failedPkgOutput = strings.TrimSpace(tr.failedOut.String())
	r.failedTestOutput = strings.TrimSpace(tr.failedTestOut.String())
	return r
}
