package engine

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestEscalateTestRunFailure covers the shared guard applied to both the Go and
// Node/JS test-parsing paths: a non-zero run with zero parsed failures becomes a
// failure; a healthy or already-failing run is left untouched.
func TestEscalateTestRunFailure(t *testing.T) {
	runErr := errors.New("exit status 1")
	cases := []struct {
		name               string
		passing, failing   int
		err                error
		wantPass, wantFail int
	}{
		{"clean pass, zero exit", 5, 0, nil, 5, 0},
		{"real failures counted, non-zero exit", 3, 2, runErr, 3, 2},
		{"non-zero exit, nothing parsed → escalate", 0, 0, runErr, 0, 1},
		{"passing tests but non-zero exit, no failures parsed → escalate", 4, 0, runErr, 4, 1},
		{"zero exit, nothing parsed (no tests) → untouched", 0, 0, nil, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, f := escalateTestRunFailure(tc.passing, tc.failing, tc.err)
			if p != tc.wantPass || f != tc.wantFail {
				t.Fatalf("got (pass=%d fail=%d), want (pass=%d fail=%d)", p, f, tc.wantPass, tc.wantFail)
			}
		})
	}
}

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

// writeGoModuleWithTest writes a minimal single-package Go module (source +
// test) into dir. Distinct from completion_gate_test.go's writeGoModule, which
// writes a package-main module with no test file.
func writeGoModuleWithTest(t *testing.T, dir, srcTest string) {
	t.Helper()
	// `go 1.26` matches the running toolchain; GOTOOLCHAIN=local keeps the
	// nested `go test` hermetic (no toolchain download).
	mustWrite(t, filepath.Join(dir, "go.mod"), "module verifyfixture\n\ngo 1.26\n")
	mustWrite(t, filepath.Join(dir, "a.go"), "package verifyfixture\n\nfunc Add(a, b int) int { return a + b }\n")
	mustWrite(t, filepath.Join(dir, "a_test.go"), srcTest)
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestCheckTests_TestCompileFailureCountsAsFailing pins the completion-gate
// safety fix: a Go module whose *_test.go files do not compile must be reported
// as failing by checkTests. `go build ./...` (checkBuild) does not compile test
// files, so if checkTests reported 0 failing here the completion gate would
// green-light REQ_COMPLETED on code whose test suite does not even compile.
func TestCheckTests_TestCompileFailureCountsAsFailing(t *testing.T) {
	t.Setenv("GOTOOLCHAIN", "local")
	dir := t.TempDir()
	writeGoModuleWithTest(t, dir, "package verifyfixture\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\t_ = Add(1, 2)\n\t_ = ThisSymbolDoesNotExist() // test-only compile error\n}\n")

	// Sanity: go build must still pass — this is exactly why checkBuild can't
	// catch the defect and checkTests has to.
	if !checkBuild(dir) {
		t.Fatal("expected go build to pass despite the test-only compile error")
	}

	_, failing, _ := checkTests(dir)
	if failing == 0 {
		t.Fatal("test-compile failure must be reported as failing, got 0 (completion gate would false-green)")
	}
}

// TestCheckTests_PassingModuleReportsNoFailures guards against a false-positive
// regression: a healthy module with passing tests must report zero failures.
func TestCheckTests_PassingModuleReportsNoFailures(t *testing.T) {
	t.Setenv("GOTOOLCHAIN", "local")
	dir := t.TempDir()
	writeGoModuleWithTest(t, dir, "package verifyfixture\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"bad\")\n\t}\n}\n")

	passing, failing, _ := checkTests(dir)
	if failing != 0 {
		t.Fatalf("healthy module must report 0 failing, got %d", failing)
	}
	if passing == 0 {
		t.Fatal("healthy module must report at least one passing test")
	}
}
