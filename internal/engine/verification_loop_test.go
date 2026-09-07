package engine

import "testing"

func TestShouldRunFixCycle_WhenTestsFail(t *testing.T) {
	result := VerificationResult{
		BuildPasses:  true,
		TestsFailing: 1,
		TestsTotal:   2,
	}
	if !ShouldRunFixCycle(result) {
		t.Fatal("expected failing tests to trigger a fix cycle")
	}
}

func TestParseGoTestJSONCountsIndividualTests(t *testing.T) {
	output := `{"Action":"pass","Package":"pkg","Test":"TestA"}
{"Action":"fail","Package":"pkg","Test":"TestB"}
{"Action":"pass","Package":"pkg"}`
	passing, failing, total := parseGoTestJSON(output)
	if passing != 1 || failing != 1 || total != 2 {
		t.Fatalf("expected 1 pass, 1 fail, 2 total; got pass=%d fail=%d total=%d", passing, failing, total)
	}
}

// TestParseGoTestJSONCountsCompileFailures pins the completion-gate backstop:
// when a package's test binary fails to compile, `go test -json` emits a
// build-fail action (and a package-scoped fail) with NO Test field and zero
// per-test events. Historically parseGoTestJSON skipped every empty-Test line,
// so a test-only compile break came back failing==0 and the completion gate
// reported the composed mainline GREEN on an uncompilable tree. It must now be
// counted as a failure so ShouldRunFixCycle triggers a fix cycle / block.
func TestParseGoTestJSONCountsCompileFailures(t *testing.T) {
	// Real `go test -json ./...` output for a package whose _test.go does not
	// compile (undefined symbol). No per-test events are emitted.
	output := `{"ImportPath":"x [x.test]","Action":"build-output","Output":"# x [x.test]\n"}
{"ImportPath":"x [x.test]","Action":"build-output","Output":"./a_test.go:3:34: undefined: Nonexistent\n"}
{"ImportPath":"x [x.test]","Action":"build-fail"}
{"Action":"start","Package":"x"}
{"Action":"output","Package":"x","Output":"FAIL\tx [build failed]\n"}
{"Action":"fail","Package":"x","FailedBuild":"x [x.test]"}`
	passing, failing, total := parseGoTestJSON(output)
	if passing != 0 || failing != 1 || total != 1 {
		t.Fatalf("expected 0 pass, 1 fail, 1 total for a compile break; got pass=%d fail=%d total=%d", passing, failing, total)
	}
	if !ShouldRunFixCycle(VerificationResult{BuildPasses: true, TestsFailing: failing}) {
		t.Fatal("a test-suite compile break must trigger a completion-gate fix cycle, not report GREEN")
	}
}

// TestParseJSTestOutput_CollectionFailureCountsAsFailure is the JS/TS sibling of
// the Go compile-break guard: a jest/vitest run that fails to collect tests (a
// spec's TypeScript compile error, a broken config, a missing runner) exits
// non-zero and emits none of the pass/fail markers, so parsing yields 0/0/0.
// The completion gate would report the mainline GREEN on a suite that does not
// compile. A non-zero exit with no parseable results must count as a failure.
func TestParseJSTestOutput_CollectionFailureCountsAsFailure(t *testing.T) {
	// A vitest collection error — no numPassedTests/numFailedTests/Tests: markers.
	crash := "Error: Failed to load config\n  at ...\nTypeError: Cannot read properties of undefined"

	passing, failing, _ := parseJSTestOutput(crash, true /* runFailed */)
	if failing == 0 {
		t.Fatalf("a runner crash with no parseable results must count as a failure; got pass=%d fail=%d", passing, failing)
	}
	if !ShouldRunFixCycle(VerificationResult{BuildPasses: true, TestsFailing: failing}) {
		t.Fatal("a JS/TS test-collection failure must trigger a fix cycle, not report GREEN")
	}

	// Guard against misfire: a clean run that exits zero with no parseable
	// output (e.g. jest --passWithNoTests on an empty suite) is NOT a failure.
	if _, f, _ := parseJSTestOutput("", false /* runFailed */); f != 0 {
		t.Fatalf("an empty suite that exits zero must not be counted as failing; got fail=%d", f)
	}

	// And a normal run whose counts DID parse is untouched by the guard even on
	// a non-zero exit (jest exits non-zero when tests fail).
	if p, f, _ := parseJSTestOutput(`{"numPassedTests":3,"numFailedTests":1}`, true); p != 1 || f != 1 {
		t.Fatalf("parsed counts must survive the guard; got pass=%d fail=%d", p, f)
	}
}
