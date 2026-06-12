package improve

import (
	"os/exec"
	"strings"
	"testing"
)

// initTempRepo creates a bare-bones git repo in a t.TempDir() and returns
// its path. Tests that need a working git context use this to exercise the
// shell-wrapping helpers without hitting the host repo.
func initTempRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "Test"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
	return dir
}

func TestParseDiffStat_FullForm(t *testing.T) {
	files, lines := parseDiffStat(" 3 files changed, 42 insertions(+), 9 deletions(-)")
	if files != 3 || lines != 51 {
		t.Errorf("got files=%d lines=%d, want 3 / 51", files, lines)
	}
}

func TestParseDiffStat_SingularForms(t *testing.T) {
	files, lines := parseDiffStat(" 1 file changed, 1 insertion(+), 1 deletion(-)")
	if files != 1 || lines != 2 {
		t.Errorf("got files=%d lines=%d, want 1 / 2", files, lines)
	}
}

func TestParseDiffStat_InsertionsOnly(t *testing.T) {
	files, lines := parseDiffStat(" 2 files changed, 10 insertions(+)")
	if files != 2 || lines != 10 {
		t.Errorf("got files=%d lines=%d, want 2 / 10", files, lines)
	}
}

func TestParseDiffStat_DeletionsOnly(t *testing.T) {
	files, lines := parseDiffStat(" 1 file changed, 7 deletions(-)")
	if files != 1 || lines != 7 {
		t.Errorf("got files=%d lines=%d, want 1 / 7", files, lines)
	}
}

func TestParseDiffStat_Empty(t *testing.T) {
	files, lines := parseDiffStat("")
	if files != 0 || lines != 0 {
		t.Errorf("got files=%d lines=%d, want zero values", files, lines)
	}
}

func TestParseDiffStat_Malformed(t *testing.T) {
	files, lines := parseDiffStat("garbage")
	if files != 0 || lines != 0 {
		t.Errorf("got files=%d lines=%d, want zero values", files, lines)
	}
}

func TestSlugify_BasicReplacement(t *testing.T) {
	got := slugify("Hello World")
	if got != "hello-world" {
		t.Errorf("got %q, want hello-world", got)
	}
}

func TestSlugify_CollapsesMultipleSeparators(t *testing.T) {
	got := slugify("foo & bar -- baz")
	if got != "foo-bar-baz" {
		t.Errorf("got %q, want foo-bar-baz", got)
	}
}

func TestSlugify_TrimsLeadingTrailingHyphens(t *testing.T) {
	got := slugify("---hello---")
	if got != "hello" {
		t.Errorf("got %q, want hello", got)
	}
}

func TestSlugify_TruncatesAt50(t *testing.T) {
	long := strings.Repeat("a", 100)
	got := slugify(long)
	if len(got) > 50 {
		t.Errorf("slug too long: %d chars", len(got))
	}
}

func TestSlugify_OnlySymbols(t *testing.T) {
	got := slugify("!!!@@@###")
	if got != "" {
		t.Errorf("symbol-only input should yield empty slug, got %q", got)
	}
}

func TestImplementer_Git_Success(t *testing.T) {
	impl := &Implementer{repoPath: initTempRepo(t)}
	if err := impl.git("status", "--porcelain"); err != nil {
		t.Errorf("git status should succeed in a fresh repo, got: %v", err)
	}
}

func TestImplementer_Git_Failure(t *testing.T) {
	impl := &Implementer{repoPath: initTempRepo(t)}
	err := impl.git("bogus-subcommand")
	if err == nil {
		t.Error("expected error for bogus git subcommand")
	}
}

func TestImplementer_GitOutput_TrimsWhitespace(t *testing.T) {
	impl := &Implementer{repoPath: initTempRepo(t)}
	out, err := impl.gitOutput("config", "user.email")
	if err != nil {
		t.Fatalf("git config user.email: %v", err)
	}
	if out != "t@example.com" {
		t.Errorf("got %q, want %q", out, "t@example.com")
	}
}

func TestImplementer_GitOutput_PropagatesError(t *testing.T) {
	impl := &Implementer{repoPath: initTempRepo(t)}
	_, err := impl.gitOutput("rev-parse", "no-such-ref")
	if err == nil {
		t.Error("expected error for unknown ref")
	}
}

func TestImplementer_Run_SucceedsForKnownBinary(t *testing.T) {
	impl := &Implementer{repoPath: initTempRepo(t)}
	if err := impl.run("git", "--version"); err != nil {
		t.Errorf("git --version should succeed, got: %v", err)
	}
}

func TestImplementer_Run_FailsForUnknownBinary(t *testing.T) {
	impl := &Implementer{repoPath: initTempRepo(t)}
	err := impl.run("definitely-not-a-real-binary-xyz", "--help")
	if err == nil {
		t.Error("expected error for unknown binary")
	}
}

func TestCheckDiffSize_WithinLimit(t *testing.T) {
	diff := strings.Repeat("line\n", 10)
	if err := CheckDiffSize(diff, 50); err != nil {
		t.Errorf("expected pass, got: %v", err)
	}
}

func TestCheckDiffSize_ExceedsLimit(t *testing.T) {
	diff := strings.Repeat("line\n", 100)
	if err := CheckDiffSize(diff, 50); err == nil {
		t.Error("expected error for oversized diff")
	}
}

func TestCheckFileCount_WithinLimit(t *testing.T) {
	if err := CheckFileCount(" 3 files changed", 10); err != nil {
		t.Errorf("expected pass, got: %v", err)
	}
}

func TestCheckFileCount_ExceedsLimit(t *testing.T) {
	if err := CheckFileCount(" 20 files changed", 10); err == nil {
		t.Error("expected error for too many files")
	}
}

func TestCheckFileCount_Unparseable(t *testing.T) {
	// No match means the guard cannot evaluate — defaults to passing.
	if err := CheckFileCount("anything else", 1); err != nil {
		t.Errorf("unparseable stat should pass, got: %v", err)
	}
}
