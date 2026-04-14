package engine

import "testing"

func TestExtractRepoName_SSH(t *testing.T) {
	got := extractRepoName("git@github.com:tzone85/vortex-dispatch.git")
	if got != "vortex-dispatch" {
		t.Errorf("expected vortex-dispatch, got %q", got)
	}
}

func TestExtractRepoName_HTTPS(t *testing.T) {
	got := extractRepoName("https://github.com/tzone85/my-client-app.git")
	if got != "my-client-app" {
		t.Errorf("expected my-client-app, got %q", got)
	}
}

func TestExtractRepoName_NoGitSuffix(t *testing.T) {
	got := extractRepoName("https://github.com/user/my-repo")
	if got != "my-repo" {
		t.Errorf("expected my-repo, got %q", got)
	}
}

func TestExtractRepoName_NestedPath(t *testing.T) {
	got := extractRepoName("https://gitlab.com/group/subgroup/repo.git")
	if got != "repo" {
		t.Errorf("expected repo, got %q", got)
	}
}

func TestExtractRepoName_SimpleURL(t *testing.T) {
	got := extractRepoName("https://host/project.git")
	if got != "project" {
		t.Errorf("expected project, got %q", got)
	}
}
