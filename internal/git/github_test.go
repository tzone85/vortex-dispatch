package git_test

import (
	"os"
	"path/filepath"
	"testing"

	vxdgit "github.com/tzone85/vortex-dispatch/internal/git"
)

func TestGHAvailable(t *testing.T) {
	// Just verify it doesn't panic.
	_ = vxdgit.GHAvailable()
}

// TestCreatePR_StdoutOnly verifies that CreatePR parses only stdout from
// "gh pr create", ignoring any stderr content (e.g. progress messages).
// This is a regression test for Bug 10 where CombinedOutput contaminated
// the PR URL with stderr noise, causing auto-merge to never trigger.
func TestCreatePR_StdoutOnly(t *testing.T) {
	const prURL = "https://github.com/owner/repo/pull/42"

	// Build a fake "gh" script: emits stderr noise + clean URL on stdout.
	fakeDir := t.TempDir()
	fakeScript := "#!/bin/sh\necho 'Creating pull request for branch...' >&2\nprintf '%s\n' '" + prURL + "'\n"
	scriptPath := filepath.Join(fakeDir, "gh")
	if err := os.WriteFile(scriptPath, []byte(fakeScript), 0755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}

	// Prepend fake dir to PATH so our script is found first.
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+origPath)

	repoDir := t.TempDir()
	info, err := vxdgit.CreatePR(repoDir, "title", "body", "main", "feature/x")
	if err != nil {
		t.Fatalf("CreatePR: %v", err)
	}

	if info.URL != prURL {
		t.Errorf("URL = %q; want %q", info.URL, prURL)
	}
	if info.Number != 42 {
		t.Errorf("Number = %d; want 42", info.Number)
	}
}

// PR tests for MergePR and GetPRStatus require a real GitHub repo with
// authentication, so they are exercised in E2E tests rather than unit tests.
