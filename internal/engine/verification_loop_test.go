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

// TestParseGoTestJSONCountsCompileFailures pins the completion-gate backstop:
// when a package's test binary fails to compile, `go test -json` emits a
// build-fail action (and a package-scoped fail) with NO Test field and zero
// per-test events. Historically parseGoTestJSON skipped every empty-Test line,
// so a test-only compile break came back failing==0 and the completion gate
// reported the composed mainline GREEN on an uncompilable tree. It must now be
// counted as a failure so ShouldRunFixCycle triggers a fix cycle / block.
func TestParseGoTestJSONCountsCompileFailures(t *testing.T) {
	// Real `go test -json ./...` output for a package whose _test.go does not
	// compile (undefined symbol). No per-test events are emitted.
	output := `{"ImportPath":"x [x.test]","Action":"build-output","Output":"# x [x.test]\n"}
{"ImportPath":"x [x.test]","Action":"build-output","Output":"./a_test.go:3:34: undefined: Nonexistent\n"}
{"ImportPath":"x [x.test]","Action":"build-fail"}
{"Action":"start","Package":"x"}
{"Action":"output","Package":"x","Output":"FAIL\tx [build failed]\n"}
{"Action":"fail","Package":"x","FailedBuild":"x [x.test]"}`
	passing, failing, total := parseGoTestJSON(output)
	if passing != 0 || failing != 1 || total != 1 {
		t.Fatalf("expected 0 pass, 1 fail, 1 total for a compile break; got pass=%d fail=%d total=%d", passing, failing, total)
	}
	if !ShouldRunFixCycle(VerificationResult{BuildPasses: true, TestsFailing: failing}) {
		t.Fatal("a test-suite compile break must trigger a completion-gate fix cycle, not report GREEN")
	}
}
