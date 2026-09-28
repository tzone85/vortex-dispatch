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

// TestParseGoTestJSON_TestCompileFailureCountsAsFailing guards the completion
// gate against a false-green. When a test package fails to COMPILE, `go test
// -json` reports it only via a `build-fail` event and a package-level `fail`
// event (both with an empty Test field) — there are no per-test results. The
// separate build gate (`go build ./...`) does not compile _test.go files, so it
// stays green. If parseGoTestJSON ignored these package-level events it would
// return 0/0/0, ShouldRunFixCycle would see no failures, and the requirement
// would be marked complete on a codebase whose test suite does not compile.
// This is the exact JSON `go test -json` emits for a test-compile error.
func TestParseGoTestJSON_TestCompileFailureCountsAsFailing(t *testing.T) {
	output := `{"ImportPath":"pkg [pkg.test]","Action":"build-output","Output":"# pkg [pkg.test]\n"}
{"ImportPath":"pkg [pkg.test]","Action":"build-output","Output":"./lib_test.go:5:10: not enough arguments in call to Add\n"}
{"ImportPath":"pkg [pkg.test]","Action":"build-fail"}
{"Action":"start","Package":"pkg"}
{"Action":"output","Package":"pkg","Output":"FAIL\tpkg [build failed]\n"}
{"Action":"fail","Package":"pkg","FailedBuild":"pkg [pkg.test]"}`
	passing, failing, total := parseGoTestJSON(output)
	if passing != 0 {
		t.Fatalf("expected 0 passing on a compile failure, got %d", passing)
	}
	if failing == 0 {
		t.Fatal("expected a test-compile failure to count as failing, got 0 — completion gate would false-green")
	}
	if total != passing+failing {
		t.Fatalf("total must equal passing+failing; got total=%d passing=%d failing=%d", total, passing, failing)
	}
	if ShouldRunFixCycle(VerificationResult{BuildPasses: true, TestsFailing: failing, TestsTotal: total}) == false {
		t.Fatal("a compile-broken test suite must trigger a fix cycle")
	}
}

// TestParseGoTestJSON_NoTestsIsNotAFailure keeps the legitimate zero-tests case
// green: a package with no test files emits neither per-test nor package-level
// fail events, so the suite is reported as 0/0/0 and does not trip the gate.
func TestParseGoTestJSON_NoTestsIsNotAFailure(t *testing.T) {
	output := `{"Action":"output","Package":"pkg","Output":"?   \tpkg\t[no test files]\n"}
{"Action":"skip","Package":"pkg","Elapsed":0}`
	passing, failing, total := parseGoTestJSON(output)
	if passing != 0 || failing != 0 || total != 0 {
		t.Fatalf("expected 0/0/0 for a package with no tests; got pass=%d fail=%d total=%d", passing, failing, total)
	}
}

// TestParseGoTestJSON_RealTestFailureNotInflated ensures the package-level fail
// event that accompanies a genuine per-test failure does not double-count: a
// single failing test reports failing=1, not 2.
func TestParseGoTestJSON_RealTestFailureNotInflated(t *testing.T) {
	output := `{"Action":"pass","Package":"pkg","Test":"TestA"}
{"Action":"fail","Package":"pkg","Test":"TestB"}
{"Action":"fail","Package":"pkg"}`
	passing, failing, total := parseGoTestJSON(output)
	if passing != 1 || failing != 1 || total != 2 {
		t.Fatalf("expected 1 pass, 1 fail, 2 total (no inflation); got pass=%d fail=%d total=%d", passing, failing, total)
	}
}
