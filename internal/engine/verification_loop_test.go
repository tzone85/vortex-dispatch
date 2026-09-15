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

// TestParseGoTestJSONTreatsTestCompileFailureAsFailing pins the completion-gate
// false-negative fix: when a package's _test.go files fail to compile,
// `go test -json ./...` emits only package-scoped events (no Test field) — a
// build-fail action plus a package fail carrying FailedBuild. `go build ./...`
// still passes (it ignores test files), so unless parseGoTestJSON recognises
// these signals the gate sees 0 failing tests and marks a non-compiling tree
// GREEN. This output is verbatim from `go test -json` on a test-only compile
// break.
func TestParseGoTestJSONTreatsTestCompileFailureAsFailing(t *testing.T) {
	output := `{"ImportPath":"gtjson [gtjson.test]","Action":"build-output","Output":"# gtjson [gtjson.test]\n"}
{"ImportPath":"gtjson [gtjson.test]","Action":"build-output","Output":"./lib_test.go:5:5: undefined: AddBroken\n"}
{"ImportPath":"gtjson [gtjson.test]","Action":"build-fail"}
{"Action":"start","Package":"gtjson"}
{"Action":"output","Package":"gtjson","Output":"FAIL\tgtjson [build failed]\n"}
{"Action":"fail","Package":"gtjson","Elapsed":0,"FailedBuild":"gtjson [gtjson.test]"}`
	passing, failing, total := parseGoTestJSON(output)
	if failing == 0 {
		t.Fatalf("test-compile failure must count as failing so ShouldRunFixCycle fires; got pass=%d fail=%d total=%d", passing, failing, total)
	}
	if !ShouldRunFixCycle(VerificationResult{BuildPasses: true, TestsFailing: failing}) {
		t.Fatal("a non-compiling test suite must trigger a fix cycle, not a GREEN completion")
	}
}

// TestParseNodeTestOutput_RunnerAbortNoOutputCountsAsFailing pins the JS/TS
// sibling of the Go build-fail fix: a jest/vitest runner that aborts before
// emitting any result JSON (broken merged config, throwing setup file,
// unresolvable runner) exits non-zero and produces none of the counted
// signals. Without the runFailed guard it scores 0/0 and the completion gate
// marks a never-run suite GREEN.
func TestParseNodeTestOutput_RunnerAbortNoOutputCountsAsFailing(t *testing.T) {
	passing, failing, _ := parseNodeTestOutput("Cannot find module 'jest-preset-x'\n", true)
	if failing == 0 {
		t.Fatalf("aborted runner must count as failing; got pass=%d fail=%d", passing, failing)
	}
	if !ShouldRunFixCycle(VerificationResult{BuildPasses: true, TestsFailing: failing}) {
		t.Fatal("a runner that never produced results must trigger a fix cycle, not a GREEN completion")
	}
}

// TestParseNodeTestOutput_CleanExitNoOutputStaysZero ensures the guard is
// fail-closed ONLY: an exit-0 run with no parseable output (e.g. jest
// --passWithNoTests on a project with zero tests) must not be turned into a
// synthetic failure.
func TestParseNodeTestOutput_CleanExitNoOutputStaysZero(t *testing.T) {
	passing, failing, _ := parseNodeTestOutput("", false)
	if passing != 0 || failing != 0 {
		t.Fatalf("clean exit with no output must stay 0/0; got pass=%d fail=%d", passing, failing)
	}
}

// TestParseNodeTestOutput_ResultJSONStillCounted ensures the runFailed guard
// does not disturb a run that DID emit result JSON (the existing key-presence
// counting still governs).
func TestParseNodeTestOutput_ResultJSONStillCounted(t *testing.T) {
	out := `{"numPassedTests":5,"numFailedTests":0,"numTotalTests":5}`
	passing, failing, _ := parseNodeTestOutput(out, false)
	if passing == 0 {
		t.Fatalf("jest result JSON must be counted, not ignored; got pass=%d fail=%d", passing, failing)
	}
}

// TestParseGoTestJSONMixedFailuresNoDoubleCount ensures the build-failed bump
// does not inflate counts when individual tests already failed in other
// packages: the synthetic failure is added only when nothing else failed.
func TestParseGoTestJSONMixedFailuresNoDoubleCount(t *testing.T) {
	output := `{"Action":"pass","Package":"a","Test":"TestA"}
{"Action":"fail","Package":"a","Test":"TestB"}
{"ImportPath":"b [b.test]","Action":"build-fail"}
{"Action":"fail","Package":"b","Elapsed":0,"FailedBuild":"b [b.test]"}`
	passing, failing, _ := parseGoTestJSON(output)
	if passing != 1 || failing != 1 {
		t.Fatalf("expected the real per-test fail to stand alone (no synthetic bump); got pass=%d fail=%d", passing, failing)
	}
}
