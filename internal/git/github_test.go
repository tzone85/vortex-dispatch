package git_test

import (
	"testing"

	vxdgit "github.com/tzone85/vortex-dispatch/internal/git"
)

func TestGHAvailable(t *testing.T) {
	// Just verify it doesn't panic.
	_ = vxdgit.GHAvailable()
}

// PR tests (CreatePR, MergePR, GetPRStatus) require a real GitHub repo with
// authentication, so they are exercised in E2E tests rather than unit tests.
