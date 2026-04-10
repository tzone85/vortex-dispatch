# Client Project Isolation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Isolate VXD state per project so multiple clients can be served simultaneously.

**Architecture:** Project state directories under `~/.vxd/projects/<name>/` with automatic name resolution from git remote URL. Each project gets its own `events.jsonl`, `vxd.db`, `worktrees/`, and `logs/`. Existing data auto-migrates to `_legacy` on first run. Config resolution chains: repo `vxd.yaml` -> global `~/.vxd/config.yaml` -> `DefaultConfig()`. CLI gains `--project` persistent flag and `vxd projects` command.

**Tech Stack:** Go 1.23+, os/exec (git commands), encoding/json, filepath, regexp

**Spec:** docs/superpowers/specs/2026-04-10-client-project-isolation-design.md

---

## Task 1: Project Resolution and Metadata

**Files:**
- New: `internal/engine/project.go`
- New: `internal/engine/project_test.go`

### Steps

- [ ] Create `internal/engine/project.go` with `ResolveProjectName()`, `ProjectMetadata`, `WriteMetadata()`, `ReadMetadata()`, `ListProjects()`, `SanitizeProjectName()`
- [ ] Create `internal/engine/project_test.go` with table-driven tests for name sanitization, git resolution, metadata read/write, and project listing
- [ ] Run tests: `go test ./internal/engine/ -run TestProject -v`
- [ ] Verify all pass

### Test Code

**New file `internal/engine/project_test.go`:**

```go
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
```

### Implementation Code

**New file `internal/engine/project.go`:**

```go
package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ProjectMetadata holds information about a VXD project.
type ProjectMetadata struct {
	Name         string    `json:"name"`
	RepoPath     string    `json:"repo_path"`
	RemoteURL    string    `json:"remote_url,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	LastActivity time.Time `json:"last_activity"`

	// Migration-only fields
	MigratedFrom string `json:"migrated_from,omitempty"`
	MigratedAt   string `json:"migrated_at,omitempty"`
	Note         string `json:"note,omitempty"`
}

// nonAlphanumDash matches any character that is not a lowercase letter, digit, or dash.
var nonAlphanumDash = regexp.MustCompile(`[^a-z0-9-]`)

// multiDash collapses consecutive dashes into a single dash.
var multiDash = regexp.MustCompile(`-{2,}`)

// SanitizeProjectName normalises a raw name to a filesystem-safe project slug.
// Result is lowercase, alphanumeric + dashes, no leading/trailing dashes.
// The .git suffix is stripped. Returns "unnamed" for empty/all-symbol input.
func SanitizeProjectName(raw string) string {
	s := strings.ToLower(raw)
	s = strings.TrimSuffix(s, ".git")
	s = nonAlphanumDash.ReplaceAllString(s, "-")
	s = multiDash.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "unnamed"
	}
	return s
}

// extractRepoName extracts the repository name from a git remote URL.
// Handles SSH (git@host:org/repo.git), HTTPS (https://host/org/repo.git),
// and nested paths (returns last segment only).
func extractRepoName(remoteURL string) string {
	// Take last path segment
	parts := strings.Split(remoteURL, "/")
	last := parts[len(parts)-1]
	// Handle SSH colon-separated paths like git@github.com:org/repo.git
	if colonIdx := strings.LastIndex(last, ":"); colonIdx >= 0 {
		last = last[colonIdx+1:]
		// If colon split gave us "org/repo.git", split again
		subParts := strings.Split(last, "/")
		last = subParts[len(subParts)-1]
	}
	return SanitizeProjectName(last)
}

// ResolveProjectName detects the project name from a git repository path.
// It first tries the origin remote URL, then falls back to the directory name
// of the repository root. Returns an error if the path is not inside a git repo.
func ResolveProjectName(repoPath string) (string, error) {
	// Verify this is a git repo
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = repoPath
	topOut, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not a git repository: %s", repoPath)
	}
	repoRoot := strings.TrimSpace(string(topOut))

	// Try origin remote URL
	cmd = exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = repoRoot
	remoteOut, err := cmd.Output()
	if err == nil {
		remoteURL := strings.TrimSpace(string(remoteOut))
		if remoteURL != "" {
			return extractRepoName(remoteURL), nil
		}
	}

	// Fallback: use directory name of repo root
	dirName := filepath.Base(repoRoot)
	return SanitizeProjectName(dirName), nil
}

// WriteMetadata writes project metadata to metadata.json in the given directory.
// Creates the directory if it does not exist.
func WriteMetadata(projectDir string, meta ProjectMetadata) error {
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		return fmt.Errorf("create project dir: %w", err)
	}

	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	path := filepath.Join(projectDir, "metadata.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}
	return nil
}

// ReadMetadata reads project metadata from metadata.json in the given directory.
func ReadMetadata(projectDir string) (ProjectMetadata, error) {
	path := filepath.Join(projectDir, "metadata.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return ProjectMetadata{}, fmt.Errorf("read metadata: %w", err)
	}

	var meta ProjectMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return ProjectMetadata{}, fmt.Errorf("parse metadata: %w", err)
	}
	return meta, nil
}

// ListProjects enumerates all projects under the VXD base directory.
// It reads metadata.json from each subdirectory of <baseDir>/projects/.
// Directories without a valid metadata.json are skipped.
func ListProjects(baseDir string) ([]ProjectMetadata, error) {
	projectsDir := filepath.Join(baseDir, "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read projects dir: %w", err)
	}

	var projects []ProjectMetadata
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		meta, err := ReadMetadata(filepath.Join(projectsDir, entry.Name()))
		if err != nil {
			// Skip directories without valid metadata
			continue
		}
		projects = append(projects, meta)
	}
	return projects, nil
}

// ProjectDir returns the full path to a project's state directory.
// baseDir is typically ~/.vxd.
func ProjectDir(baseDir, projectName string) string {
	return filepath.Join(baseDir, "projects", projectName)
}
```

### Run

```bash
go test ./internal/engine/ -run TestProject -v
go test ./internal/engine/ -run TestSanitize -v
go test ./internal/engine/ -run TestExtractRepoName -v
go test ./internal/engine/ -run TestResolve -v
go test ./internal/engine/ -run TestWriteAndRead -v
go test ./internal/engine/ -run TestListProjects -v
```

### Commit

```bash
git add internal/engine/project.go internal/engine/project_test.go
git commit -m "feat: add project resolution, metadata, and listing for client isolation"
```

---

## Task 2: Auto-Migration

**Files:**
- Edit: `internal/engine/project.go` (add `MigrateOldLayout()`)
- Edit: `internal/engine/project_test.go` (add migration tests)

### Steps

- [ ] Read `internal/engine/project.go` (from Task 1)
- [ ] Add `MigrateOldLayout()` function that moves old flat-layout files into `projects/_legacy/`
- [ ] Add migration tests with temp dirs simulating old layout
- [ ] Run tests and verify idempotency
- [ ] Run full engine test suite

### Test Code

**Append to `internal/engine/project_test.go`:**

```go
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
```

### Implementation Code

**Append to `internal/engine/project.go`:**

```go
// MigrateOldLayout moves pre-isolation VXD state files from the flat layout
// (~/.vxd/events.jsonl, ~/.vxd/vxd.db, etc.) into ~/.vxd/projects/_legacy/.
//
// Trigger: ~/.vxd/events.jsonl exists AND ~/.vxd/projects/ does not exist.
// Returns true if migration was performed, false if skipped (already migrated
// or no old files). Idempotent: safe to call on every startup.
func MigrateOldLayout(baseDir string) (bool, error) {
	// Check if projects/ already exists — skip if so (idempotent guard)
	projectsDir := filepath.Join(baseDir, "projects")
	if _, err := os.Stat(projectsDir); err == nil {
		return false, nil
	}

	// Check if old layout exists (events.jsonl is the sentinel file)
	oldEvents := filepath.Join(baseDir, "events.jsonl")
	if _, err := os.Stat(oldEvents); os.IsNotExist(err) {
		return false, nil
	}

	// Create legacy directory
	legacyDir := filepath.Join(projectsDir, "_legacy")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		return false, fmt.Errorf("create legacy dir: %w", err)
	}

	// Move files that exist
	filesToMove := []string{"events.jsonl", "vxd.db"}
	for _, name := range filesToMove {
		src := filepath.Join(baseDir, name)
		dst := filepath.Join(legacyDir, name)
		if _, err := os.Stat(src); err == nil {
			if err := os.Rename(src, dst); err != nil {
				return false, fmt.Errorf("move %s: %w", name, err)
			}
		}
	}

	// Move directories that exist
	dirsToMove := []string{"worktrees", "logs"}
	for _, name := range dirsToMove {
		src := filepath.Join(baseDir, name)
		dst := filepath.Join(legacyDir, name)
		if _, err := os.Stat(src); err == nil {
			if err := os.Rename(src, dst); err != nil {
				return false, fmt.Errorf("move %s/: %w", name, err)
			}
		}
	}

	// Write legacy metadata
	meta := ProjectMetadata{
		Name:         "_legacy",
		MigratedFrom: baseDir,
		MigratedAt:   time.Now().UTC().Format(time.RFC3339),
		Note:         "Auto-migrated from pre-isolation layout",
		CreatedAt:    time.Now().UTC(),
		LastActivity: time.Now().UTC(),
	}
	if err := WriteMetadata(legacyDir, meta); err != nil {
		return false, fmt.Errorf("write legacy metadata: %w", err)
	}

	return true, nil
}
```

### Run

```bash
go test ./internal/engine/ -run TestMigrateOldLayout -v
go test ./internal/engine/ -v
```

### Commit

```bash
git add internal/engine/project.go internal/engine/project_test.go
git commit -m "feat: add auto-migration of old flat layout to projects/_legacy/"
```

---

## Task 3: Config Global Fallback Chain

**Files:**
- Edit: `internal/config/loader.go`
- Edit: `internal/config/config_test.go`

### Steps

- [ ] Read `internal/config/loader.go` and `internal/config/config_test.go`
- [ ] Refactor `LoadFromFile` to add a `LoadConfigChain()` function that implements: repo `vxd.yaml` -> `~/.vxd/config.yaml` -> `DefaultConfig()`
- [ ] Add tests for the fallback chain
- [ ] Run config tests
- [ ] Run full test suite to verify no regressions

### Test Code

**Append to `internal/config/config_test.go`:**

```go
func TestLoadConfigChain_RepoFileFirst(t *testing.T) {
	dir := t.TempDir()

	// Create a repo-level vxd.yaml with a custom log level
	repoYAML := `
version: "1.0"
workspace:
  state_dir: "~/.vxd"
  backend: dolt
  log_level: debug
  log_retention_days: 30
routing:
  junior_max_complexity: 3
  intermediate_max_complexity: 5
  max_retries_before_escalation: 2
  max_qa_failures_before_escalation: 3
  max_senior_retries: 2
  max_manager_attempts: 2
cleanup:
  worktree_prune: immediate
  branch_retention_days: 7
  log_archive: dolt
merge:
  auto_merge: true
  base_branch: main
`
	repoPath := filepath.Join(dir, "vxd.yaml")
	os.WriteFile(repoPath, []byte(repoYAML), 0o644)

	cfg, err := LoadConfigChain(repoPath, filepath.Join(dir, "nonexistent", "config.yaml"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Workspace.LogLevel != "debug" {
		t.Errorf("expected debug log level from repo config, got %q", cfg.Workspace.LogLevel)
	}
}

func TestLoadConfigChain_FallsToGlobal(t *testing.T) {
	dir := t.TempDir()

	// No repo config, but global config exists
	globalYAML := `
version: "1.0"
workspace:
  state_dir: "~/.vxd"
  backend: dolt
  log_level: warn
  log_retention_days: 30
routing:
  junior_max_complexity: 3
  intermediate_max_complexity: 5
  max_retries_before_escalation: 2
  max_qa_failures_before_escalation: 3
  max_senior_retries: 2
  max_manager_attempts: 2
cleanup:
  worktree_prune: immediate
  branch_retention_days: 7
  log_archive: dolt
merge:
  auto_merge: true
  base_branch: main
`
	globalPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(globalPath, []byte(globalYAML), 0o644)

	cfg, err := LoadConfigChain(filepath.Join(dir, "nonexistent.yaml"), globalPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Workspace.LogLevel != "warn" {
		t.Errorf("expected warn log level from global config, got %q", cfg.Workspace.LogLevel)
	}
}

func TestLoadConfigChain_FallsToDefault(t *testing.T) {
	dir := t.TempDir()

	cfg, err := LoadConfigChain(
		filepath.Join(dir, "nope.yaml"),
		filepath.Join(dir, "also-nope.yaml"),
	)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// Should match DefaultConfig values
	def := DefaultConfig()
	if cfg.Workspace.LogLevel != def.Workspace.LogLevel {
		t.Errorf("expected default log level %q, got %q", def.Workspace.LogLevel, cfg.Workspace.LogLevel)
	}
	if cfg.Workspace.Backend != def.Workspace.Backend {
		t.Errorf("expected default backend %q, got %q", def.Workspace.Backend, cfg.Workspace.Backend)
	}
}
```

### Implementation Code

**Edit `internal/config/loader.go`** -- add `LoadConfigChain()` after the existing `LoadFromFile()`:

```go
// LoadConfigChain tries to load configuration from a chain of sources:
//  1. repoPath — per-project vxd.yaml in the repo root (highest priority)
//  2. globalPath — global ~/.vxd/config.yaml (fallback defaults)
//  3. DefaultConfig() — hardcoded fallback
//
// Returns the first successfully loaded config. If all files are missing,
// returns DefaultConfig() without error. Only returns an error if a file
// exists but is malformed.
func LoadConfigChain(repoPath, globalPath string) (Config, error) {
	// Try repo config first
	cfg, err := LoadFromFile(repoPath)
	if err == nil {
		return cfg, nil
	}
	// If file exists but is malformed, return that error
	if !os.IsNotExist(unwrapReadErr(err)) {
		if _, statErr := os.Stat(repoPath); statErr == nil {
			return Config{}, err
		}
	}

	// Try global config
	cfg, err = LoadFromFile(globalPath)
	if err == nil {
		return cfg, nil
	}
	if !os.IsNotExist(unwrapReadErr(err)) {
		if _, statErr := os.Stat(globalPath); statErr == nil {
			return Config{}, err
		}
	}

	// Fall back to defaults
	return DefaultConfig(), nil
}

// unwrapReadErr attempts to extract the underlying OS error from a wrapped
// config loading error so we can check os.IsNotExist.
func unwrapReadErr(err error) error {
	if err == nil {
		return nil
	}
	// The error chain from LoadFromFile is: "reading config file: <os error>"
	// Use errors.Unwrap chain
	for e := err; e != nil; {
		if os.IsNotExist(e) {
			return e
		}
		if unwrapped, ok := e.(interface{ Unwrap() error }); ok {
			e = unwrapped.Unwrap()
		} else {
			break
		}
	}
	return err
}
```

### Run

```bash
go test ./internal/config/ -run TestLoadConfigChain -v
go test ./internal/config/ -v
```

### Commit

```bash
git add internal/config/loader.go internal/config/config_test.go
git commit -m "feat: add config chain loader with repo -> global -> default fallback"
```

---

## Task 4: Rewire loadStores for Project Isolation

**Files:**
- Edit: `internal/cli/helpers.go` (major rewrite of `loadStores()`, add `resolveProject()`)

### Steps

- [ ] Read `internal/cli/helpers.go` (current version)
- [ ] Add `resolveProject(cmd *cobra.Command) string` that checks: `--project` flag -> `VXD_PROJECT` env -> git detection
- [ ] Rewrite `loadStores()` to use project-scoped state directory
- [ ] Integrate `MigrateOldLayout()` call on first run
- [ ] Update `loadConfig()` to use `LoadConfigChain()`
- [ ] Ensure metadata is written on first access to a new project
- [ ] Run tests

### Implementation Code

**Replace `internal/cli/helpers.go` with:**

```go
package cli

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/engine"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// stores bundles the event store and projection store opened from a config.
// Both must be closed by the caller.
type stores struct {
	Config     config.Config
	Events     state.EventStore
	Proj       *state.SQLiteStore
	ProjectDir string // e.g. ~/.vxd/projects/acme-corp-api
}

// loadStores loads configuration and opens both event and projection stores
// scoped to the current project. The caller is responsible for closing both
// stores.
//
// Project resolution order:
//  1. --project flag (explicit name)
//  2. VXD_PROJECT env var
//  3. Git repo detection from cwd
func loadStores(cmd *cobra.Command) (stores, error) {
	cfgPath, _ := cmd.Flags().GetString("config")
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return stores{}, err
	}

	baseDir := expandHome(cfg.Workspace.StateDir)

	// Auto-migrate old flat layout on first run
	migrated, migrateErr := engine.MigrateOldLayout(baseDir)
	if migrateErr != nil {
		log.Printf("warning: migration failed: %v", migrateErr)
	}
	if migrated {
		log.Printf("Migrated existing VXD data to %s/projects/_legacy/", baseDir)
	}

	// Resolve project name
	projectName, err := resolveProject(cmd)
	if err != nil {
		return stores{}, fmt.Errorf("resolve project: %w", err)
	}

	projectDir := engine.ProjectDir(baseDir, projectName)
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		return stores{}, fmt.Errorf("create project dir: %w", err)
	}

	// Write metadata if this is a new project (metadata.json does not exist)
	metaPath := filepath.Join(projectDir, "metadata.json")
	if _, statErr := os.Stat(metaPath); os.IsNotExist(statErr) {
		cwd, _ := os.Getwd()
		remoteURL := detectRemoteURL(cwd)
		meta := engine.ProjectMetadata{
			Name:         projectName,
			RepoPath:     cwd,
			RemoteURL:    remoteURL,
			CreatedAt:    time.Now().UTC(),
			LastActivity: time.Now().UTC(),
		}
		if writeErr := engine.WriteMetadata(projectDir, meta); writeErr != nil {
			log.Printf("warning: could not write project metadata: %v", writeErr)
		}
	}

	es, err := state.NewFileStore(filepath.Join(projectDir, "events.jsonl"))
	if err != nil {
		return stores{}, fmt.Errorf("open event store: %w", err)
	}

	ps, err := state.NewSQLiteStore(filepath.Join(projectDir, "vxd.db"))
	if err != nil {
		es.Close()
		return stores{}, fmt.Errorf("open projection store: %w", err)
	}

	// Backfill acceptance_criteria for stories created before the column existed.
	allEvents, _ := es.List(state.EventFilter{Type: state.EventStoryCreated})
	ps.BackfillAcceptanceCriteria(allEvents)

	return stores{
		Config:     cfg,
		Events:     es,
		Proj:       ps,
		ProjectDir: projectDir,
	}, nil
}

// Close releases both stores.
func (s stores) Close() {
	if s.Events != nil {
		s.Events.Close()
	}
	if s.Proj != nil {
		s.Proj.Close()
	}
}

// resolveProject determines the project name from (in priority order):
//  1. --project flag
//  2. VXD_PROJECT environment variable
//  3. Git repository detection from cwd
func resolveProject(cmd *cobra.Command) (string, error) {
	// 1. Explicit --project flag
	if flagVal, _ := cmd.Flags().GetString("project"); flagVal != "" {
		return engine.SanitizeProjectName(flagVal), nil
	}

	// 2. VXD_PROJECT environment variable
	if envVal := os.Getenv("VXD_PROJECT"); envVal != "" {
		return engine.SanitizeProjectName(envVal), nil
	}

	// 3. Git detection from cwd
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}

	name, err := engine.ResolveProjectName(cwd)
	if err != nil {
		return "", fmt.Errorf("not in a git repository. Use --project or VXD_PROJECT env var")
	}
	return name, nil
}

// detectRemoteURL returns the git origin remote URL from the given directory,
// or an empty string if not available.
func detectRemoteURL(dir string) string {
	cmd := engine.GitCommand("remote", "get-url", "origin")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// loadConfig loads configuration using the chain: repo config -> global -> defaults.
func loadConfig(cfgPath string) (config.Config, error) {
	if cfgPath == "" {
		cfgPath = "vxd.yaml"
	}

	home, err := os.UserHomeDir()
	if err != nil {
		// Can't determine home, try repo config only then default
		cfg, loadErr := config.LoadFromFile(cfgPath)
		if loadErr != nil {
			return config.DefaultConfig(), nil
		}
		return cfg, nil
	}

	globalPath := filepath.Join(home, ".vxd", "config.yaml")
	return config.LoadConfigChain(cfgPath, globalPath)
}

// expandHome replaces a leading ~ with the user's home directory.
func expandHome(path string) string {
	if len(path) == 0 {
		return path
	}
	if path[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[1:])
	}
	return path
}
```

**IMPORTANT:** The above code references `engine.GitCommand()` for `detectRemoteURL`. Since `detect.go` uses `exec.Command` directly, we should use `exec.Command` in `detectRemoteURL` instead. Update the function:

```go
// detectRemoteURL returns the git origin remote URL from the given directory,
// or an empty string if not available.
func detectRemoteURL(dir string) string {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
```

And add `"os/exec"` and `"strings"` to the imports.

**Note on signature change:** `loadStores` now takes `cmd *cobra.Command` instead of `cfgPath string`. Every caller must be updated. The callers are in the various CLI command files (status.go, metrics.go, req.go, etc.). Each currently does:

```go
cfgPath, _ := cmd.Flags().GetString("config")
s, err := loadStores(cfgPath)
```

This must become:

```go
s, err := loadStores(cmd)
```

The `cfgPath` extraction moves inside `loadStores()`. Search for all occurrences and update them.

### Run

```bash
# Find all callers of loadStores to update
grep -rn "loadStores(" internal/cli/
# After updating all callers:
go build ./cmd/vxd
go test ./internal/cli/ -v
go test ./... -v
```

### Commit

```bash
git add internal/cli/helpers.go internal/cli/status.go internal/cli/metrics.go internal/cli/req.go internal/cli/resume.go internal/cli/agents.go internal/cli/events.go internal/cli/dashboard.go internal/cli/archive.go internal/cli/gc.go internal/cli/config.go
git commit -m "feat: rewire loadStores for per-project state isolation"
```

---

## Task 5: Root Command + Projects Command

**Files:**
- Edit: `internal/cli/root.go` (add `--project` flag, register `newProjectsCmd()`)
- New: `internal/cli/projects.go` (new `vxd projects` command)

### Steps

- [ ] Read `internal/cli/root.go`
- [ ] Add `--project` persistent flag to `rootCmd`
- [ ] Register `newProjectsCmd()` in `init()`
- [ ] Create `internal/cli/projects.go` with `vxd projects` command
- [ ] The command lists all projects with story counts by reading metadata and opening each project's SQLite DB
- [ ] Run build and manual test
- [ ] Run test suite

### Implementation Code

**Edit `internal/cli/root.go`** -- add the persistent flag and register the command:

```go
package cli

import (
	"github.com/spf13/cobra"
)

var version = "0.1.0"

var rootCmd = &cobra.Command{
	Use:   "vxd",
	Short: "Vortex Dispatch -- AI agent orchestrator",
	Long:  "VXD orchestrates autonomous AI agents through the full software development lifecycle.\nHand off a requirement, walk away, come back to merged PRs.",
	Version: version,
}

func init() {
	rootCmd.PersistentFlags().String("config", "vxd.yaml", "Path to config file")
	rootCmd.PersistentFlags().String("project", "", "Project name (auto-detected from git repo if not specified)")

	rootCmd.AddCommand(newInitCmd())
	rootCmd.AddCommand(newReqCmd())
	rootCmd.AddCommand(newStatusCmd())
	rootCmd.AddCommand(newPauseCmd())
	rootCmd.AddCommand(newResumeCmd())
	rootCmd.AddCommand(newAgentsCmd())
	rootCmd.AddCommand(newEscalationsCmd())
	rootCmd.AddCommand(newGCCmd())
	rootCmd.AddCommand(newConfigCmd())
	rootCmd.AddCommand(newEventsCmd())
	rootCmd.AddCommand(newDashboardCmd())
	rootCmd.AddCommand(newArchiveCmd())
	rootCmd.AddCommand(newMemoryCmd())
	rootCmd.AddCommand(newOpportunityCmd())
	rootCmd.AddCommand(newMetricsCmd())
	rootCmd.AddCommand(newProjectsCmd())
}

func Execute() error {
	return rootCmd.Execute()
}
```

**New file `internal/cli/projects.go`:**

```go
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/tzone85/vortex-dispatch/internal/engine"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

func newProjectsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "projects",
		Short: "List all VXD projects",
		Long: `Lists all projects managed by VXD with story counts and status.

Projects are automatically created when you run vxd commands in a git repository.
Use --project to switch between projects explicitly.`,
		RunE: runProjects,
	}
	cmd.SilenceUsage = true
	return cmd
}

func runProjects(cmd *cobra.Command, _ []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home dir: %w", err)
	}
	baseDir := filepath.Join(home, ".vxd")

	projects, err := engine.ListProjects(baseDir)
	if err != nil {
		return fmt.Errorf("list projects: %w", err)
	}

	out := cmd.OutOrStdout()

	if len(projects) == 0 {
		fmt.Fprintf(out, "No projects found. Run 'vxd req' in a git repository to get started.\n")
		return nil
	}

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "PROJECT\tREPO PATH\tSTORIES\tMERGED\tSTATUS\n")

	for _, p := range projects {
		stories, merged := countProjectStories(baseDir, p.Name)
		repoPath := p.RepoPath
		if repoPath == "" && p.MigratedFrom != "" {
			repoPath = fmt.Sprintf("(migrated from %s)", p.MigratedFrom)
		}
		status := "active"
		if p.Name == "_legacy" {
			status = "archived"
		}
		fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%s\n", p.Name, repoPath, stories, merged, status)
	}

	w.Flush()
	return nil
}

// countProjectStories opens the project's SQLite DB read-only and counts
// total stories and merged stories. Returns (0, 0) on any error.
func countProjectStories(baseDir, projectName string) (total, merged int) {
	dbPath := filepath.Join(baseDir, "projects", projectName, "vxd.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return 0, 0
	}

	ps, err := state.NewSQLiteStore(dbPath)
	if err != nil {
		return 0, 0
	}
	defer ps.Close()

	allStories, err := ps.ListStories(state.StoryFilter{})
	if err != nil {
		return 0, 0
	}

	total = len(allStories)
	for _, s := range allStories {
		if s.Status == "merged" || s.Status == "pr_submitted" {
			merged++
		}
	}
	return total, merged
}
```

### Run

```bash
go build -o ~/.local/bin/vxd ./cmd/vxd
vxd projects
vxd projects --help
```

### Commit

```bash
git add internal/cli/root.go internal/cli/projects.go
git commit -m "feat: add --project flag and 'vxd projects' command"
```

---

## Task 6: Wiring Tests

**Files:**
- Edit: `internal/engine/wiring_test.go` (add project isolation wiring tests)

### Steps

- [ ] Read `internal/engine/wiring_test.go`
- [ ] Add wiring tests that verify:
  - Project resolution from git repo produces correct name
  - Auto-migration moves files and is idempotent
  - `SanitizeProjectName` handles edge cases
  - `ProjectDir` computes correct paths
  - `ListProjects` reads metadata correctly
- [ ] Run wiring tests
- [ ] Run full test suite

### Test Code

**Append to `internal/engine/wiring_test.go`:**

```go
// --------------------------------------------------------------------------
// Session 2026-04-10: Client Project Isolation
// --------------------------------------------------------------------------

func TestWiring_ProjectResolution_FromGitRemote(t *testing.T) {
	// Verify ResolveProjectName extracts correct name from git remote URL.
	dir := t.TempDir()
	exec.Command("git", "init", dir).Run()
	cmd := exec.Command("git", "remote", "add", "origin", "git@github.com:tzone85/acme-corp-api.git")
	cmd.Dir = dir
	cmd.Run()

	name, err := engine.ResolveProjectName(dir)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if name != "acme-corp-api" {
		t.Errorf("WIRING FAILURE: ResolveProjectName returned %q, expected acme-corp-api.\n"+
			"The function exists but is not extracting the repo name correctly.", name)
	}
}

func TestWiring_ProjectResolution_NoRemoteFallsToDirName(t *testing.T) {
	dir := t.TempDir()
	repoDir := filepath.Join(dir, "my-client-app")
	os.MkdirAll(repoDir, 0o755)
	exec.Command("git", "init", repoDir).Run()

	name, err := engine.ResolveProjectName(repoDir)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if name != "my-client-app" {
		t.Errorf("WIRING FAILURE: ResolveProjectName fallback returned %q, expected my-client-app", name)
	}
}

func TestWiring_AutoMigration_MovesAndIsIdempotent(t *testing.T) {
	baseDir := t.TempDir()

	// Simulate old layout
	os.WriteFile(filepath.Join(baseDir, "events.jsonl"), []byte(`{"type":"REQ_CREATED"}`+"\n"), 0o644)
	os.WriteFile(filepath.Join(baseDir, "vxd.db"), []byte("db-content"), 0o644)

	// First migration should move files
	migrated, err := engine.MigrateOldLayout(baseDir)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if !migrated {
		t.Error("WIRING FAILURE: MigrateOldLayout did not migrate when old files exist")
	}

	// Verify files are in _legacy
	legacyEvents := filepath.Join(baseDir, "projects", "_legacy", "events.jsonl")
	if _, err := os.Stat(legacyEvents); err != nil {
		t.Errorf("WIRING FAILURE: events.jsonl not found in _legacy after migration")
	}

	// Second call should be idempotent
	migrated2, err := engine.MigrateOldLayout(baseDir)
	if err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if migrated2 {
		t.Error("WIRING FAILURE: MigrateOldLayout should be idempotent — second call should return false")
	}
}

func TestWiring_ProjectDir_ComputesCorrectPath(t *testing.T) {
	got := engine.ProjectDir("/home/user/.vxd", "acme-corp")
	want := "/home/user/.vxd/projects/acme-corp"
	if got != want {
		t.Errorf("WIRING FAILURE: ProjectDir = %q, want %q", got, want)
	}
}

func TestWiring_SanitizeProjectName_EdgeCases(t *testing.T) {
	// Verify sanitization is actually called and works
	tests := []struct {
		input, want string
	}{
		{"My.App.git", "my-app"},
		{"UPPER_CASE", "upper-case"},
		{"---", "unnamed"},
	}
	for _, tt := range tests {
		got := engine.SanitizeProjectName(tt.input)
		if got != tt.want {
			t.Errorf("WIRING FAILURE: SanitizeProjectName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestWiring_ListProjects_ReadsMetadata(t *testing.T) {
	baseDir := t.TempDir()
	pDir := filepath.Join(baseDir, "projects", "test-proj")
	os.MkdirAll(pDir, 0o755)

	meta := engine.ProjectMetadata{
		Name:     "test-proj",
		RepoPath: "/tmp/test-proj",
	}
	engine.WriteMetadata(pDir, meta)

	projects, err := engine.ListProjects(baseDir)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("WIRING FAILURE: ListProjects returned %d projects, expected 1", len(projects))
	}
	if projects[0].Name != "test-proj" {
		t.Errorf("WIRING FAILURE: listed project name = %q, want test-proj", projects[0].Name)
	}
}
```

### Run

```bash
go test ./internal/engine/ -run "TestWiring_Project|TestWiring_Auto|TestWiring_Sanitize|TestWiring_List" -v
go test ./internal/engine/ -v
```

### Commit

```bash
git add internal/engine/wiring_test.go
git commit -m "test: add wiring tests for project isolation, migration, and resolution"
```

---

## Task 7: Verification

**Files:** None (verification only)

### Steps

- [ ] Run full test suite: `go test ./... -v`
- [ ] Build VXD binary: `go build -o ~/.local/bin/vxd ./cmd/vxd`
- [ ] Verify `vxd projects` works: `vxd projects`
- [ ] Verify `vxd --project test status` works (or errors gracefully)
- [ ] Verify `vxd status` in the VXD repo still works (auto-detects project)
- [ ] Verify no regressions in existing commands
- [ ] Check that `~/.vxd/projects/` directory structure is created correctly
- [ ] If old data exists, verify auto-migration to `_legacy`

### Run

```bash
# Full test suite
go test ./... -v

# Build
go build -o ~/.local/bin/vxd ./cmd/vxd

# Smoke tests
vxd projects
vxd status
vxd metrics --limit 5
vxd --project _legacy status
vxd events --limit 5

# Verify directory structure
ls -la ~/.vxd/projects/
ls -la ~/.vxd/projects/vortex-dispatch/ 2>/dev/null || echo "Project dir will be created on first use"
```

### Commit

```bash
git add -A
git commit -m "chore: verify client project isolation end-to-end"
```
