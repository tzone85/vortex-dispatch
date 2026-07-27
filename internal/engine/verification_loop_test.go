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

// TestParseGoTestJSON_BuildFailCountsAsFailing pins that a package which fails
// to COMPILE is counted as failing. `go test -json` reports a compile break as
// a package-level "build-fail" (and a "fail" with an empty Test field) with no
// per-test event — the exact cross-story drift the completion gate exists to
// catch. Before the fix these were skipped and the gate saw zero failures.
func TestParseGoTestJSON_BuildFailCountsAsFailing(t *testing.T) {
	// Verbatim shape emitted by `go test -count=1 -json ./...` on a package
	// whose test file references an undefined symbol.
	output := `{"ImportPath":"gtj [gtj.test]","Action":"build-output","Output":"# gtj [gtj.test]\n"}
{"ImportPath":"gtj [gtj.test]","Action":"build-output","Output":"./x_test.go:3:32: undefined: Bar\n"}
{"ImportPath":"gtj [gtj.test]","Action":"build-fail"}
{"Action":"start","Package":"gtj"}
{"Action":"output","Package":"gtj","Output":"FAIL\tgtj [build failed]\n"}
{"Action":"fail","Package":"gtj","Elapsed":0,"FailedBuild":"gtj [gtj.test]"}`
	passing, failing, _ := parseGoTestJSON(output)
	if passing != 0 {
		t.Errorf("passing = %d, want 0", passing)
	}
	if failing == 0 {
		t.Fatal("a build-failed package must count as failing; got 0 (false green)")
	}
}

// TestCheckTests_CompileBreakNotGreen is the end-to-end guard: a composed
// mainline whose test files do not compile must NOT be reported as green by the
// completion gate. `go build ./...` alone passes (it does not compile test
// files), so this is exactly the false-green the gate must not produce.
func TestCheckTests_CompileBreakNotGreen(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module gtj\n\ngo 1.26\n")
	write("x.go", "package gtj\n\nfunc Foo() int { return 1 }\n")
	// Test references an undefined symbol → test package fails to compile.
	write("x_test.go", "package gtj\n\nimport \"testing\"\n\nfunc TestFoo(t *testing.T) { if Bar() != 1 { t.Fatal(\"no\") } }\n")

	_, failing, _ := checkTests(dir)
	if failing == 0 {
		t.Fatal("checkTests reported a compile-broken tree as green (0 failing)")
	}
}
