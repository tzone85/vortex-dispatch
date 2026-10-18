package improve_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/improve"
)

// These tests run the REAL go toolchain over small throwaway modules so the
// parsers are checked against genuine tool output, not scripted strings.

func writeModule(t *testing.T, files map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func indexByCheck(rep improve.SelfAuditReport) map[improve.CheckName][]improve.SelfFinding {
	out := map[improve.CheckName][]improve.SelfFinding{}
	for _, f := range rep.Findings {
		out[f.Check] = append(out[f.Check], f)
	}
	return out
}

func skippedByCheck(rep improve.SelfAuditReport) map[improve.CheckName]string {
	out := map[improve.CheckName]string{}
	for _, s := range rep.Skipped() {
		out[s.Check] = s.Err
	}
	return out
}

func TestRealToolchain_BrokenBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the real go toolchain")
	}
	dir := writeModule(t, map[string]string{
		"go.mod":               "module example.com/rt\n\ngo 1.26\n\nrequire golang.org/x/text v0.41.0 // indirect\n",
		"pkg/ok/ok.go":         "package ok\n\nfunc Hello() string {\n\treturn   \"hi\"\n}\n",
		"pkg/broken/broken.go": "package broken\n\nfunc Broken() int {\n\treturn undefinedThing\n}\n",
	})
	rep, err := improve.NewSelfAuditor(dir, nil).Audit(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	by := indexByCheck(rep)
	skipped := skippedByCheck(rep)

	if n := len(by[improve.CheckBuild]); n != 1 || by[improve.CheckBuild][0].Target != "pkg/broken/broken.go:4:9" {
		t.Errorf("build findings = %+v", by[improve.CheckBuild])
	}
	if n := len(by[improve.CheckVet]); n != 0 {
		t.Errorf("vet must not duplicate the compile error: %+v", by[improve.CheckVet])
	}
	if n := len(by[improve.CheckGofmt]); n != 1 || by[improve.CheckGofmt][0].Target != "pkg/ok/ok.go" {
		t.Errorf("gofmt findings = %+v", by[improve.CheckGofmt])
	}
	if n := len(by[improve.CheckModTidy]); n != 1 {
		t.Errorf("tidy findings = %d, want 1 (unused indirect require)", n)
	}
	if msg, ok := skipped[improve.CheckTest]; !ok || !strings.Contains(msg, "build failed") {
		t.Errorf("go test must be skipped on a broken build, skipped=%v", skipped)
	}
	// govulncheck cannot load a broken package: exit 1, no findings => skipped, not clean.
	if _, ok := skipped[improve.CheckVuln]; !ok {
		t.Errorf("govulncheck must be reported as skipped on a broken build, skipped=%v", skipped)
	}
	if len(by[improve.CheckVuln]) != 0 {
		t.Errorf("govulncheck error text must not become findings: %+v", by[improve.CheckVuln])
	}
}

func TestRealToolchain_FailingTestAndVet(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the real go toolchain")
	}
	// unusedresult is NOT in the vet subset `go test` runs, so the tests still execute.
	dir := writeModule(t, map[string]string{
		"go.mod":            "module example.com/rt2\n\ngo 1.26\n",
		"pkg/ok/ok.go":      "package ok\n\nimport \"fmt\"\n\nfunc Hello() string {\n\tfmt.Sprintf(\"unused\")\n\treturn \"hi\"\n}\n",
		"pkg/ok/ok_test.go": "package ok\n\nimport \"testing\"\n\nfunc TestHelloFails(t *testing.T) {\n\tt.Fatal(\"deliberate failure\")\n}\n\nfunc TestPasses(t *testing.T) {}\n",
	})
	a := improve.NewSelfAuditor(dir, nil)
	a.TestTimeout = "2m"
	rep, err := a.Audit(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	by := indexByCheck(rep)
	skipped := skippedByCheck(rep)

	if len(by[improve.CheckBuild]) != 0 {
		t.Errorf("build should be clean: %+v", by[improve.CheckBuild])
	}
	if n := len(by[improve.CheckVet]); n != 1 || by[improve.CheckVet][0].Target != "pkg/ok/ok.go:6:2" {
		t.Errorf("vet findings = %+v", by[improve.CheckVet])
	}
	targets := map[string]bool{}
	for _, f := range by[improve.CheckTest] {
		targets[f.Target] = true
	}
	if !targets["TestHelloFails"] || !targets["example.com/rt2/pkg/ok"] || targets["TestPasses"] {
		t.Errorf("test findings = %v, want the failing test + its package only", targets)
	}
	if _, ok := skipped[improve.CheckTest]; ok {
		t.Error("go test must run on a compiling module")
	}
	if len(by[improve.CheckGofmt]) != 0 || len(by[improve.CheckModTidy]) != 0 {
		t.Errorf("no deterministic drift expected: gofmt=%+v tidy=%+v", by[improve.CheckGofmt], by[improve.CheckModTidy])
	}
	// govulncheck needs the network for its DB; a skip is acceptable, findings are not.
	if len(by[improve.CheckVuln]) != 0 {
		t.Errorf("unexpected vulnerability findings in a stdlib-only module: %+v", by[improve.CheckVuln])
	}
}
