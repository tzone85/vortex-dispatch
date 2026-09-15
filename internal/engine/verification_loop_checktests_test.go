package engine

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCheckTests_GoTestCompileFailureNotSilentlyGreen pins the completion-gate
// fix: a merged Go project whose _test.go files do not compile must be reported
// as failing. `go build ./...` does not compile test files, so checkBuild
// passes; `go test -json ./...` exits non-zero but emits only a package-level
// fail with an empty Test field (no per-test events), which parseGoTestJSON
// counts as zero. Before the fix, checkTests swallowed the exit code and
// returned 0/0/0, letting CompletionGate.Run emit REQ_COMPLETED on a repo whose
// test suite is red — the exact failure the gate exists to prevent.
func TestCheckTests_GoTestCompileFailureNotSilentlyGreen(t *testing.T) {
	dir := t.TempDir()
	writeCheckTestFile(t, dir, "go.mod", "module vxdverifytest\n\ngo 1.21\n")
	writeCheckTestFile(t, dir, "main.go", "package vxdverifytest\n\nfunc Add(a, b int) int { return a + b }\n")
	// A test file that does not compile (undeclared identifier).
	writeCheckTestFile(t, dir, "main_test.go", "package vxdverifytest\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) { this_does_not_compile }\n")

	// Sanity: the build must pass (test files are not compiled by `go build`),
	// so the test suite is the only signal that catches this breakage.
	if !checkBuild(dir) {
		t.Fatalf("precondition: go build ./... should pass (it ignores _test.go files)")
	}

	passing, failing, _ := checkTests(dir)
	if failing == 0 {
		t.Fatalf("checkTests reported no failures for a non-compiling test suite (passing=%d) — completion gate would falsely report green", passing)
	}
}

// TestCheckTests_GoTestAllGreen guards against the fix over-triggering: a healthy
// suite (go test exits 0) must report zero failures.
func TestCheckTests_GoTestAllGreen(t *testing.T) {
	dir := t.TempDir()
	writeCheckTestFile(t, dir, "go.mod", "module vxdverifyok\n\ngo 1.21\n")
	writeCheckTestFile(t, dir, "main.go", "package vxdverifyok\n\nfunc Add(a, b int) int { return a + b }\n")
	writeCheckTestFile(t, dir, "main_test.go", "package vxdverifyok\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"math is broken\")\n\t}\n}\n")

	if _, failing, _ := checkTests(dir); failing != 0 {
		t.Fatalf("checkTests reported %d failures for a passing suite — fix must not over-trigger", failing)
	}
}

// TestParseGoTestJSON_PackageBuildFailureHasNoPerTestEvent documents WHY the
// exit-code check in checkTests is required: parseGoTestJSON on its own counts
// zero failures for a test-package build failure, because `go test -json`
// reports it as a package-level fail with an empty Test field.
func TestParseGoTestJSON_PackageBuildFailureHasNoPerTestEvent(t *testing.T) {
	stream := `{"Action":"start","Package":"x"}
{"Action":"output","Package":"x","Output":"FAIL\tx [build failed]\n"}
{"Action":"fail","Package":"x","Elapsed":0,"FailedBuild":"x [x.test]"}`
	passing, failing, total := parseGoTestJSON(stream)
	if passing != 0 || failing != 0 || total != 0 {
		t.Fatalf("parseGoTestJSON should count 0/0/0 for a build-failure-only stream, got %d/%d/%d", passing, failing, total)
	}
}

func writeCheckTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
