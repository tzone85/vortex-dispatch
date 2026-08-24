package engine

import (
	"errors"
	"path/filepath"
	"testing"
)

// TestApplyRunnerFailure_FailClosedOnRunnerError pins the completion-gate
// fail-closed guard: a non-zero runner exit with no attributable named-test
// failure must score at least one failure so ShouldRunFixCycle fires.
func TestApplyRunnerFailure_FailClosedOnRunnerError(t *testing.T) {
	cases := []struct {
		name    string
		runErr  error
		failing int
		want    int
	}{
		{"runner ok, no failures", nil, 0, 0},
		{"runner ok, named failures", nil, 3, 3},
		{"runner failed, named failures parsed", errors.New("exit 1"), 2, 2},
		{"runner failed, zero parsed (compile/panic/launch)", errors.New("exit 1"), 0, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := applyRunnerFailure(tc.runErr, tc.failing); got != tc.want {
				t.Fatalf("applyRunnerFailure(%v, %d) = %d, want %d", tc.runErr, tc.failing, got, tc.want)
			}
		})
	}
}

// TestCheckTests_NonCompilingTestFileCountsAsFailure is the end-to-end
// regression for the false-negative: a Go module whose *_test.go does not
// compile passes `go build ./...` but its test suite cannot run. Before the
// fix, `go test -json` emitted only package-level build-fail events (empty
// Test field) that parseGoTestJSON skipped, and the non-zero exit was
// discarded — so checkTests returned failing=0 and the completion gate
// waved REQ_COMPLETED through on a non-building test tree.
func TestCheckTests_NonCompilingTestFileCountsAsFailure(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/vxdverify\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "main.go"), "package main\n\nfunc Add(a, b int) int { return a + b }\n\nfunc main() {}\n")
	// Calls Add with the wrong arity: compiles into the test binary only, so
	// `go build ./...` (no test files) succeeds while `go test` build-fails.
	writeFile(t, filepath.Join(dir, "broken_test.go"),
		"package main\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1) != 2 {\n\t\tt.Fatal(\"nope\")\n\t}\n}\n")

	// Sanity: the non-test build is green — this is what makes the gap dangerous.
	if !checkBuild(dir) {
		t.Fatal("precondition: go build ./... should pass (test files are not built)")
	}

	_, failing, _ := checkTests(dir)
	if failing == 0 {
		t.Fatal("checkTests scored 0 failing for a module whose test suite does not compile — completion gate would wave it through")
	}

	res := VerificationResult{BuildPasses: true, TestsFailing: failing}
	if !ShouldRunFixCycle(res) {
		t.Fatal("ShouldRunFixCycle must fire when the test suite fails to compile")
	}
}
