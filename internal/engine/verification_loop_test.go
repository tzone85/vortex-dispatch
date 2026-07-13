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

// TestParseGoTestJSON_BuildFailIsCounted pins the fail-open fix: a test binary
// that fails to COMPILE emits a package-level "build-fail" (empty Test) and no
// per-test events. Previously all such events were skipped, so a non-compiling
// suite parsed as (0,0,0) = clean. It must now count as failing.
func TestParseGoTestJSON_BuildFailIsCounted(t *testing.T) {
	output := `{"ImportPath":"pkg [pkg.test]","Action":"build-output","Output":"# pkg\n"}
{"ImportPath":"pkg [pkg.test]","Action":"build-output","Output":"./x_test.go:4:8: undefined: Foo\n"}
{"ImportPath":"pkg [pkg.test]","Action":"build-fail"}
{"Action":"fail","Package":"pkg","FailedBuild":"pkg [pkg.test]"}`
	_, failing, _ := parseGoTestJSON(output)
	if failing == 0 {
		t.Fatalf("a package build-fail must count as failing, got failing=0 (fail-open)")
	}
}

// TestCheckTests_BrokenTestFileIsNotClean is the end-to-end regression for the
// completion-gate fail-open. Production code builds (go build ./... ignores test
// files), but a _test.go references an undefined symbol — exactly the cross-story
// drift the completion gate exists to catch. checkTests must report failing > 0
// so ShouldRunFixCycle blocks REQ_COMPLETED instead of reporting green.
func TestCheckTests_BrokenTestFileIsNotClean(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/x\n\ngo 1.26\n")
	write("main.go", "package x\n\n// Add is real production code and compiles fine.\nfunc Add(a, b int) int { return a + b }\n")
	// Broken test: references a symbol that does not exist (cross-story drift).
	write("x_test.go", "package x\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\t_ = NonexistentSymbol\n}\n")

	if !checkBuild(dir) {
		t.Fatalf("go build ./... must pass — it does not compile test files")
	}
	_, failing, _ := checkTests(dir)
	if failing == 0 {
		t.Fatalf("a test file that fails to compile must count as failing, got failing=0 (fail-open: REQ_COMPLETED on an un-compilable suite)")
	}
}
