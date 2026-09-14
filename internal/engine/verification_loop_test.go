package engine

import (
	"os"
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

// TestParseGoTestJSON_TestOnlyBuildFailureCountsAsFailure guards the
// completion-gate false-negative: when a package's *test* binary fails to
// compile, `go test -json` emits a package-level "build-fail" (and a "fail"
// with an empty Test) but no per-test events. `go build ./...` does not
// compile test files, so this is the only signal of a test-only build break.
// If it were not counted, the gate would treat a non-compiling suite as
// "0 failing" and permit REQ_COMPLETED on code whose tests do not compile.
func TestParseGoTestJSON_TestOnlyBuildFailureCountsAsFailure(t *testing.T) {
	// Verbatim shape of `go test -json ./...` when a test file does not compile.
	output := `{"ImportPath":"example.com/m [example.com/m.test]","Action":"build-output","Output":"# example.com/m [example.com/m.test]\n"}
{"ImportPath":"example.com/m [example.com/m.test]","Action":"build-output","Output":"./lib_test.go:6:8: undefined: Undefined\n"}
{"ImportPath":"example.com/m [example.com/m.test]","Action":"build-fail"}
{"Action":"start","Package":"example.com/m"}
{"Action":"output","Package":"example.com/m","Output":"FAIL\texample.com/m [build failed]\n"}
{"Action":"fail","Package":"example.com/m","FailedBuild":"example.com/m [example.com/m.test]"}`
	passing, failing, total := parseGoTestJSON(output)
	if failing == 0 {
		t.Fatalf("expected a test-only build failure to count as failing; got pass=%d fail=%d total=%d", passing, failing, total)
	}
}

// TestCheckTests_TestOnlyBuildBreakBlocksCompletion is the end-to-end guard:
// a Go module that builds cleanly (`go build ./...` passes) but whose test
// file does not compile must report failing tests so ShouldRunFixCycle blocks
// completion. Skips when the go toolchain is unavailable.
func TestCheckTests_TestOnlyBuildBreakBlocksCompletion(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("go.mod", "module example.com/m\n\ngo 1.26\n")
	write("lib.go", "package m\n\nfunc Add(a, b int) int { return a + b }\n")
	// Compiles under `go build ./...` (test files excluded) but not under
	// `go test` — calls Add with the wrong arity and references an undefined id.
	write("lib_test.go", "package m\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\t_ = Add(1)\n\tif Undefined() {\n\t\tt.Fail()\n\t}\n}\n")

	if !checkBuild(dir) {
		t.Skip("go build unexpectedly failed (toolchain unavailable); skipping")
	}
	passing, failing, total := checkTests(dir)
	if failing == 0 {
		t.Fatalf("expected a test-only compile break to report failing tests; got pass=%d fail=%d total=%d", passing, failing, total)
	}
	if !ShouldRunFixCycle(VerificationResult{BuildPasses: true, TestsFailing: failing}) {
		t.Fatal("expected ShouldRunFixCycle to block completion when the test suite does not compile")
	}
}
