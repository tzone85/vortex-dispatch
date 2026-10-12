package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// writeModule lays out a throwaway Go module under dir with the given files
// (relative path -> contents) plus a go.mod, so checkBuild/checkTests can run
// the real toolchain against it.
func writeModule(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	files["go.mod"] = "module vxdverify\n\ngo 1.21\n"
	for rel, content := range files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
}

// TestCheckTests_TestCompileFailureIsNotClean is a regression test for the
// completion-gate false-negative: a *_test.go that fails to compile (classic
// cross-story drift — a renamed/removed symbol still referenced by another
// story's test) must be reported as a test failure. `go build ./...` ignores
// *_test.go, so checkBuild passes; and `go test -json` reports the broken
// package only as a package-level "fail" with an empty Test field, which the
// per-test parser does not count. Before the fix checkTests returned
// failing=0 here, so ShouldRunFixCycle permitted REQ_COMPLETED on a tree whose
// tests do not compile.
func TestCheckTests_TestCompileFailureIsNotClean(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	dir := t.TempDir()
	writeModule(t, dir, map[string]string{
		// A package that builds and whose test passes.
		"good/good.go":      "package good\n\nfunc Add(a, b int) int { return a + b }\n",
		"good/good_test.go": "package good\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"bad\")\n\t}\n}\n",
		// Production code builds, but the test references an undefined symbol,
		// so the test package fails to compile.
		"bad/bad.go":      "package bad\n\nfunc Mul(a, b int) int { return a * b }\n",
		"bad/bad_test.go": "package bad\n\nimport \"testing\"\n\nfunc TestMul(t *testing.T) {\n\t_ = Nonexistent(1)\n\t_ = t\n}\n",
	})

	// `go build ./...` must pass — it does not compile *_test.go files.
	if !checkBuild(dir) {
		t.Fatal("checkBuild should pass: production code compiles cleanly")
	}

	_, failing, _ := checkTests(dir)
	if failing == 0 {
		t.Fatal("checkTests reported 0 failing on a tree whose test package does not compile; the completion gate would wrongly permit REQ_COMPLETED")
	}

	res := VerificationResult{BuildPasses: true, TestsFailing: failing}
	if !ShouldRunFixCycle(res) {
		t.Fatal("a test-compile failure must trigger a fix cycle, not completion")
	}
}

// TestCheckTests_CleanModuleReportsNoFailures guards against a false positive:
// a module whose tests all pass must report failing=0 (the guard only fires on
// a non-zero runner exit).
func TestCheckTests_CleanModuleReportsNoFailures(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	dir := t.TempDir()
	writeModule(t, dir, map[string]string{
		"good/good.go":      "package good\n\nfunc Add(a, b int) int { return a + b }\n",
		"good/good_test.go": "package good\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"bad\")\n\t}\n}\n",
	})

	passing, failing, _ := checkTests(dir)
	if failing != 0 {
		t.Fatalf("clean module reported %d failing, want 0", failing)
	}
	if passing == 0 {
		t.Fatal("clean module reported 0 passing, want >= 1")
	}
}
