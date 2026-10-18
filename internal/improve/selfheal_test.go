package improve

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const unformattedMain = "package main\n\nfunc main() {\n\tx   :=   1\n\t_ = x\n}\n"

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newHealRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"go.mod":       "module example.com/heal\n\ngo 1.26\n",
		"main.go":      unformattedMain,
		"main_test.go": "package main\n\nimport \"testing\"\n\nfunc TestOK(t *testing.T) { main() }\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "config", "user.email", "t@example.com")
	gitIn(t, dir, "config", "user.name", "t")
	gitIn(t, dir, "config", "commit.gpgsign", "false")
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "init")
	return dir
}

func gofmtReport() SelfAuditReport {
	return SelfAuditReport{Findings: ParseGofmtOutput("main.go\n")}
}

type recRunner struct{ calls []string }

func (r *recRunner) Run(_ context.Context, _ string, name string, args ...string) (string, int, error) {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	return "", 0, nil
}

func TestApplyDeterministicFixes_ArgvShape(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rr := &recRunner{}
	findings := append(ParseGofmtOutput("a.go\n"), ParseModTidyDrift("--- a/go.mod\n+++ b/go.mod\n@@ -1 +1 @@\n-x\n")...)
	findings = append(findings, ParseBuildOutput("b.go:1:1: boom\n")...) // must be ignored
	applied, err := ApplyDeterministicFixes(context.Background(), rr, dir, findings)
	if err != nil {
		t.Fatal(err)
	}
	if len(rr.calls) != 2 || rr.calls[0] != "gofmt -w a.go" || rr.calls[1] != "go mod tidy" {
		t.Errorf("unexpected argv: %v", rr.calls)
	}
	if len(applied) != 2 {
		t.Errorf("applied = %v", applied)
	}
}

func TestApplyDeterministicFixes_RejectsUnsafePath(t *testing.T) {
	rr := &recRunner{}
	_, err := ApplyDeterministicFixes(context.Background(), rr, t.TempDir(), ParseGofmtOutput("../x.go\n"))
	if err == nil {
		t.Fatal("expected error for traversal path")
	}
	if len(rr.calls) != 0 {
		t.Errorf("nothing must run, got %v", rr.calls)
	}
}

func TestApplyDeterministicFixes_RealGofmt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	if err := os.WriteFile(path, []byte(unformattedMain), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyDeterministicFixes(context.Background(), ExecRunner{}, dir, ParseGofmtOutput("a.go\n")); err != nil {
		t.Fatal(err)
	}
	out, _, _ := ExecRunner{}.Run(context.Background(), dir, "gofmt", "-l", ".")
	if strings.TrimSpace(out) != "" {
		t.Errorf("file still unformatted: %q", out)
	}
}

func TestSelfHeal_OpensPRFromCleanBranch(t *testing.T) {
	dir := newHealRepo(t)
	impl := NewImplementer(dir, "", 500, 10, false)
	var pushed, prBranch string
	impl.push = func(b string) error { pushed = b; return nil }
	impl.openPR = func(_, _, b string) (string, error) { prBranch = b; return "https://example.invalid/pr/1", nil }

	res, attempted := impl.SelfHeal(context.Background(), gofmtReport(), "2026-10-18")
	if !attempted || !res.IsImplemented() {
		t.Fatalf("want implemented, got attempted=%v res=%+v", attempted, res)
	}
	if res.PRURL == "" || pushed != "vxd-improve/2026-10-18-self-heal" || prBranch != pushed {
		t.Errorf("push/PR not wired: pushed=%q pr=%q url=%q", pushed, prBranch, res.PRURL)
	}
	if got := gitIn(t, dir, "show", pushed+":main.go"); got == strings.TrimSpace(unformattedMain) {
		t.Error("branch main.go should be formatted")
	}
	if got := gitIn(t, dir, "show", "main:main.go"); got != strings.TrimSpace(unformattedMain) {
		t.Error("main must be left untouched")
	}
	if cur := gitIn(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); cur != "main" {
		t.Errorf("should return to main, on %q", cur)
	}
}

func TestSelfHeal_IgnoresUnrelatedDirtyFiles(t *testing.T) {
	// The daily run dirties docs/self-improvement/ before self-heal runs, and
	// leaves untracked run summaries. Neither may block a heal, and neither may
	// leak into the heal commit.
	dir := newHealRepo(t)
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "changelog.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "docs/changelog.jsonl")
	gitIn(t, dir, "commit", "-q", "-m", "track changelog")
	if err := os.WriteFile(filepath.Join(dir, "docs", "changelog.jsonl"), []byte("{}\n{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scratch.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	impl := NewImplementer(dir, "", 500, 10, false)
	impl.push = func(string) error { return nil }
	impl.openPR = func(_, _, _ string) (string, error) { return "https://example.invalid/pr/2", nil }

	res, attempted := impl.SelfHeal(context.Background(), gofmtReport(), "2026-10-18")
	if !attempted || !res.IsImplemented() {
		t.Fatalf("unrelated dirty files must not block: %+v", res)
	}
	files := gitIn(t, dir, "show", "--name-only", "--format=", res.Branch)
	if strings.Contains(files, "changelog") || strings.Contains(files, "scratch") {
		t.Errorf("unrelated files leaked into the heal commit: %q", files)
	}
	if files != "main.go" {
		t.Errorf("heal commit should touch only main.go, got %q", files)
	}
}

func TestSelfHeal_RefusesWhenStagedPathsAreDirty(t *testing.T) {
	dir := newHealRepo(t)
	// main.go is the gofmt target and already has uncommitted edits.
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(unformattedMain+"// local edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	impl := NewImplementer(dir, "", 500, 10, false)
	impl.push = func(string) error { t.Error("must not push"); return nil }

	res, attempted := impl.SelfHeal(context.Background(), gofmtReport(), "2026-10-18")
	if !attempted || res.Disposition != "aborted" || !strings.Contains(res.Error, "already modified") {
		t.Fatalf("want aborted/already modified, got %+v", res)
	}
	if b := gitIn(t, dir, "branch", "--list", "vxd-improve/*"); b != "" {
		t.Errorf("no branch should be created, got %q", b)
	}
}

func TestSelfHeal_NothingDeterministic(t *testing.T) {
	impl := NewImplementer(t.TempDir(), "", 500, 10, false)
	rep := SelfAuditReport{Findings: ParseBuildOutput("a.go:1:1: boom\n")}
	if _, attempted := impl.SelfHeal(context.Background(), rep, "2026-10-18"); attempted {
		t.Error("judgement-only report must not attempt a heal")
	}
}

func TestSelfHeal_DryRunProposes(t *testing.T) {
	impl := NewImplementer(t.TempDir(), "", 500, 10, true)
	res, attempted := impl.SelfHeal(context.Background(), gofmtReport(), "2026-10-18")
	if !attempted || res.Disposition != "proposed" {
		t.Errorf("dry run must propose, got %+v", res)
	}
}
