package engine

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestSanitizeProjectName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"acme-corp-api", "acme-corp-api"},
		{"Acme_Corp_API", "acme-corp-api"},
		{"my.repo.name", "my-repo-name"},
		{"---leading-trailing---", "leading-trailing"},
		{"hello world!", "hello-world"},
		{"UPPER CASE", "upper-case"},
		{"repo.git", "repo"},
		{"a--b---c", "a-b-c"},
		{"", "unnamed"},
		{"---", "unnamed"},
		{"my/repo/path", "my-repo-path"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := SanitizeProjectName(tt.input)
			if got != tt.want {
				t.Errorf("SanitizeProjectName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestResolveProjectName_FromRemote(t *testing.T) {
	dir := setupGitRepoWithRemote(t, "git@github.com:tzone85/acme-corp-api.git")

	name, err := ResolveProjectName(dir)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if name != "acme-corp-api" {
		t.Errorf("expected acme-corp-api, got %q", name)
	}
}

func TestResolveProjectName_HTTPSRemote(t *testing.T) {
	dir := setupGitRepoWithRemote(t, "https://github.com/tzone85/my-client-app.git")

	name, err := ResolveProjectName(dir)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if name != "my-client-app" {
		t.Errorf("expected my-client-app, got %q", name)
	}
}

func TestResolveProjectName_NoRemote_FallsBackToDirName(t *testing.T) {
	dir := t.TempDir()
	// Create a repo dir with a known name
	repoDir := filepath.Join(dir, "my-local-project")
	os.MkdirAll(repoDir, 0o755)

	cmd := exec.Command("git", "init", repoDir)
	if err := cmd.Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}

	name, err := ResolveProjectName(repoDir)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if name != "my-local-project" {
		t.Errorf("expected my-local-project, got %q", name)
	}
}

func TestResolveProjectName_NotGitRepo(t *testing.T) {
	dir := t.TempDir()
	_, err := ResolveProjectName(dir)
	if err == nil {
		t.Error("expected error for non-git directory")
	}
}

func TestWriteAndReadMetadata(t *testing.T) {
	dir := t.TempDir()

	meta := ProjectMetadata{
		Name:         "test-project",
		RepoPath:     "/Users/test/Sites/test-project",
		RemoteURL:    "git@github.com:test/test-project.git",
		CreatedAt:    time.Date(2026, 4, 10, 8, 0, 0, 0, time.UTC),
		LastActivity: time.Date(2026, 4, 10, 15, 30, 0, 0, time.UTC),
	}

	if err := WriteMetadata(dir, meta); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := ReadMetadata(dir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if got.Name != meta.Name {
		t.Errorf("name: got %q, want %q", got.Name, meta.Name)
	}
	if got.RepoPath != meta.RepoPath {
		t.Errorf("repo_path: got %q, want %q", got.RepoPath, meta.RepoPath)
	}
	if got.RemoteURL != meta.RemoteURL {
		t.Errorf("remote_url: got %q, want %q", got.RemoteURL, meta.RemoteURL)
	}
}

func TestReadMetadata_NotExist(t *testing.T) {
	dir := t.TempDir()
	_, err := ReadMetadata(dir)
	if err == nil {
		t.Error("expected error for missing metadata.json")
	}
}

func TestListProjects(t *testing.T) {
	baseDir := t.TempDir()
	projectsDir := filepath.Join(baseDir, "projects")

	// Create two project directories with metadata
	for _, name := range []string{"alpha", "beta"} {
		pDir := filepath.Join(projectsDir, name)
		os.MkdirAll(pDir, 0o755)
		meta := ProjectMetadata{
			Name:      name,
			RepoPath:  "/tmp/" + name,
			CreatedAt: time.Now(),
		}
		data, _ := json.Marshal(meta)
		os.WriteFile(filepath.Join(pDir, "metadata.json"), data, 0o644)
	}

	// Create a directory without metadata (should be skipped gracefully)
	os.MkdirAll(filepath.Join(projectsDir, "no-meta"), 0o755)

	projects, err := ListProjects(baseDir)
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	// Should find exactly 2 (the one without metadata is skipped)
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}

	names := map[string]bool{}
	for _, p := range projects {
		names[p.Name] = true
	}
	if !names["alpha"] || !names["beta"] {
		t.Errorf("expected alpha and beta, got %v", names)
	}
}

func TestListProjects_NoProjectsDir(t *testing.T) {
	baseDir := t.TempDir()

	projects, err := ListProjects(baseDir)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(projects) != 0 {
		t.Errorf("expected 0 projects, got %d", len(projects))
	}
}

func TestExtractRepoName_Variants(t *testing.T) {
	tests := []struct {
		remoteURL string
		want      string
	}{
		{"git@github.com:tzone85/acme-corp-api.git", "acme-corp-api"},
		{"https://github.com/tzone85/my-app.git", "my-app"},
		{"https://github.com/tzone85/my-app", "my-app"},
		{"git@gitlab.com:org/sub/deep-repo.git", "deep-repo"},
		{"ssh://git@bitbucket.org/team/repo-name.git", "repo-name"},
	}

	for _, tt := range tests {
		t.Run(tt.remoteURL, func(t *testing.T) {
			got := extractRepoName(tt.remoteURL)
			if got != tt.want {
				t.Errorf("extractRepoName(%q) = %q, want %q", tt.remoteURL, got, tt.want)
			}
		})
	}
}

// --------------------------------------------------------------------------
// Migration Tests
// --------------------------------------------------------------------------

func TestMigrateOldLayout_MovesFiles(t *testing.T) {
	baseDir := t.TempDir()

	// Create old layout files
	os.WriteFile(filepath.Join(baseDir, "events.jsonl"), []byte(`{"type":"test"}`+"\n"), 0o644)
	os.WriteFile(filepath.Join(baseDir, "vxd.db"), []byte("sqlite-data"), 0o644)
	os.MkdirAll(filepath.Join(baseDir, "worktrees", "wt-1"), 0o755)
	os.WriteFile(filepath.Join(baseDir, "worktrees", "wt-1", "file.go"), []byte("package main"), 0o644)
	os.MkdirAll(filepath.Join(baseDir, "logs"), 0o755)
	os.WriteFile(filepath.Join(baseDir, "logs", "agent.log"), []byte("log line"), 0o644)

	migrated, err := MigrateOldLayout(baseDir)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if !migrated {
		t.Error("expected migration to happen")
	}

	legacyDir := filepath.Join(baseDir, "projects", "_legacy")

	// Verify files moved to _legacy
	if _, err := os.Stat(filepath.Join(legacyDir, "events.jsonl")); err != nil {
		t.Error("events.jsonl not found in _legacy")
	}
	if _, err := os.Stat(filepath.Join(legacyDir, "vxd.db")); err != nil {
		t.Error("vxd.db not found in _legacy")
	}
	if _, err := os.Stat(filepath.Join(legacyDir, "worktrees", "wt-1", "file.go")); err != nil {
		t.Error("worktrees not moved to _legacy")
	}
	if _, err := os.Stat(filepath.Join(legacyDir, "logs", "agent.log")); err != nil {
		t.Error("logs not moved to _legacy")
	}

	// Verify original files are gone
	if _, err := os.Stat(filepath.Join(baseDir, "events.jsonl")); !os.IsNotExist(err) {
		t.Error("events.jsonl should be gone from base dir")
	}
	if _, err := os.Stat(filepath.Join(baseDir, "vxd.db")); !os.IsNotExist(err) {
		t.Error("vxd.db should be gone from base dir")
	}

	// Verify metadata.json exists in _legacy
	meta, err := ReadMetadata(legacyDir)
	if err != nil {
		t.Fatalf("read legacy metadata: %v", err)
	}
	if meta.Name != "_legacy" {
		t.Errorf("expected name _legacy, got %q", meta.Name)
	}
	if meta.MigratedFrom == "" {
		t.Error("expected migrated_from to be set")
	}
}

func TestMigrateOldLayout_Idempotent(t *testing.T) {
	baseDir := t.TempDir()

	// Create old layout
	os.WriteFile(filepath.Join(baseDir, "events.jsonl"), []byte(`{"type":"test"}`+"\n"), 0o644)

	// First migration
	migrated1, err := MigrateOldLayout(baseDir)
	if err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if !migrated1 {
		t.Error("expected first migration to happen")
	}

	// Second migration should be a no-op
	migrated2, err := MigrateOldLayout(baseDir)
	if err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if migrated2 {
		t.Error("expected second migration to be skipped (idempotent)")
	}
}

func TestMigrateOldLayout_NoOldFiles(t *testing.T) {
	baseDir := t.TempDir()

	// No old files exist — migration should not trigger
	migrated, err := MigrateOldLayout(baseDir)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if migrated {
		t.Error("expected no migration when no old files exist")
	}

	// projects/ dir should NOT be created
	if _, err := os.Stat(filepath.Join(baseDir, "projects")); !os.IsNotExist(err) {
		t.Error("projects/ should not be created when there is nothing to migrate")
	}
}

func TestMigrateOldLayout_OnlyEventsJSONL(t *testing.T) {
	baseDir := t.TempDir()

	// Only events.jsonl exists (no db, no worktrees, no logs)
	os.WriteFile(filepath.Join(baseDir, "events.jsonl"), []byte(`{"type":"test"}`+"\n"), 0o644)

	migrated, err := MigrateOldLayout(baseDir)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if !migrated {
		t.Error("expected migration to happen")
	}

	legacyDir := filepath.Join(baseDir, "projects", "_legacy")
	if _, err := os.Stat(filepath.Join(legacyDir, "events.jsonl")); err != nil {
		t.Error("events.jsonl not found in _legacy")
	}
}

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

func setupGitRepoWithRemote(t *testing.T, remoteURL string) string {
	t.Helper()
	dir := t.TempDir()

	cmd := exec.Command("git", "init", dir)
	if err := cmd.Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}

	cmd = exec.Command("git", "remote", "add", "origin", remoteURL)
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git remote add: %v", err)
	}

	return dir
}
