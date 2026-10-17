package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

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

// TestParseGoTestJSON_TestFileCompileFailureCountsAsFailing pins the completion-gate
// false-green fix: `go build ./...` does not compile _test.go files, so a story
// that breaks another story's test compilation passes checkBuild. `go test -json`
// reports that compile break only as package-level "build-fail"/"fail" events with
// an EMPTY Test field. These MUST count as failing, or ShouldRunFixCycle returns
// false and REQ_COMPLETED fires on an uncompilable test suite. Payload is the exact
// shape emitted by `go test -count=1 -json ./...` on a test-file compile error.
func TestParseGoTestJSON_TestFileCompileFailureCountsAsFailing(t *testing.T) {
	output := `{"ImportPath":"gtj [gtj.test]","Action":"build-output","Output":"# gtj [gtj.test]\n"}
{"ImportPath":"gtj [gtj.test]","Action":"build-output","Output":"./lib_test.go:6:6: undefined: Subtract\n"}
{"ImportPath":"gtj [gtj.test]","Action":"build-fail"}
{"Action":"start","Package":"gtj"}
{"Action":"output","Package":"gtj","Output":"FAIL\tgtj [build failed]\n"}
{"Action":"fail","Package":"gtj","Elapsed":0,"FailedBuild":"gtj [gtj.test]"}`
	passing, failing, total := parseGoTestJSON(output)
	if failing == 0 {
		t.Fatalf("test-file compile failure must count as failing; got pass=%d fail=%d total=%d", passing, failing, total)
	}
	if passing != 0 {
		t.Fatalf("expected 0 passing on a build-failed package; got %d", passing)
	}
	if !ShouldRunFixCycle(VerificationResult{BuildPasses: true, TestsFailing: failing}) {
		t.Fatal("a compile-broken test suite must trigger a fix cycle, not a false green")
	}
}

// TestParseGoTestJSON_PackageLevelFailNotDoubleCounted ensures a package that
// already reported an individual test failure is not counted twice by the new
// package-level failure handling.
func TestParseGoTestJSON_PackageLevelFailNotDoubleCounted(t *testing.T) {
	output := `{"Action":"pass","Package":"pkg","Test":"TestA"}
{"Action":"fail","Package":"pkg","Test":"TestB"}
{"Action":"fail","Package":"pkg"}`
	passing, failing, total := parseGoTestJSON(output)
	if passing != 1 || failing != 1 || total != 2 {
		t.Fatalf("package-level fail must not double-count a per-test failure; got pass=%d fail=%d total=%d", passing, failing, total)
	}
}

// TestParseGoTestJSON_NoTestFilesIsNotFailing keeps the benign case green: a Go
// module with no test files emits a package-level "skip" and exits 0.
func TestParseGoTestJSON_NoTestFilesIsNotFailing(t *testing.T) {
	output := `{"Action":"start","Package":"gtj2"}
{"Action":"output","Package":"gtj2","Output":"?   \tgtj2\t[no test files]\n"}
{"Action":"skip","Package":"gtj2","Elapsed":0}`
	passing, failing, total := parseGoTestJSON(output)
	if passing != 0 || failing != 0 || total != 0 {
		t.Fatalf("no-test-files must be 0/0/0; got pass=%d fail=%d total=%d", passing, failing, total)
	}
}

// TestCheckTests_GoTestCompileFailureIsCaught drives the real command path
// (checkTests → go test -json → parseGoTestJSON) against a module whose test
// file does not compile. `go build ./...` passes on this tree, so this is the
// end-to-end guard that the completion gate no longer scores it green.
func TestCheckTests_GoTestCompileFailureIsCaught(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module gtjtest\n\ngo 1.24\n")
	write("lib.go", "package gtjtest\n\nfunc Add(a, b int) int { return a + b }\n")
	// Undefined symbol => _test.go compile failure that `go build ./...` misses.
	write("lib_test.go", "package gtjtest\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) { _ = Subtract(2, 1) }\n")

	// Sanity: the build check the gate relies on passes on this tree.
	if err := validateBuild(dir); err != nil {
		t.Fatalf("expected go build ./... to pass (test files are not built); got %v", err)
	}

	_, failing, _ := checkTests(dir)
	if failing == 0 {
		t.Fatal("checkTests must report failing>0 for an uncompilable test suite")
	}
}
