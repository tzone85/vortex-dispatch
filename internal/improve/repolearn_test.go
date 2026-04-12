package improve

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestRepoLearner_EnsureRepo_CloneFresh(t *testing.T) {
	// Create a local bare repo to clone from (no network needed)
	bareDir := t.TempDir()
	setupBareRepo(t, bareDir)

	cloneDir := t.TempDir()
	rl := NewRepoLearner(cloneDir, "claude", true)

	repoDir := filepath.Join(cloneDir, "test-repo")
	isNew, err := rl.ensureRepo(context.Background(), bareDir, repoDir)
	if err != nil {
		t.Fatalf("ensureRepo (clone): %v", err)
	}
	if !isNew {
		t.Error("first clone should return isNew=true")
	}

	// Verify .git exists
	if _, err := os.Stat(filepath.Join(repoDir, ".git")); os.IsNotExist(err) {
		t.Error(".git directory should exist after clone")
	}
}

func TestRepoLearner_EnsureRepo_PullExisting(t *testing.T) {
	bareDir := t.TempDir()
	setupBareRepo(t, bareDir)

	cloneDir := t.TempDir()
	rl := NewRepoLearner(cloneDir, "claude", true)

	repoDir := filepath.Join(cloneDir, "test-repo")
	// First clone
	_, err := rl.ensureRepo(context.Background(), bareDir, repoDir)
	if err != nil {
		t.Fatalf("ensureRepo (clone): %v", err)
	}

	// Pull (should succeed, not new)
	isNew, err := rl.ensureRepo(context.Background(), bareDir, repoDir)
	if err != nil {
		t.Fatalf("ensureRepo (pull): %v", err)
	}
	if isNew {
		t.Error("pull should return isNew=false")
	}
}

func TestRepoLearner_LearnFromRepo_DryRun(t *testing.T) {
	bareDir := t.TempDir()
	setupBareRepo(t, bareDir)

	cloneDir := t.TempDir()
	rl := NewRepoLearner(cloneDir, "claude", true) // dry run

	learnings, err := rl.LearnFromRepo(context.Background(), LearningRepo{
		Name:  "test-repo",
		URL:   bareDir,
		Focus: []string{"testing"},
	})
	if err != nil {
		t.Fatalf("LearnFromRepo: %v", err)
	}
	// Dry run returns nil learnings
	if learnings != nil {
		t.Errorf("dry run should return nil learnings, got %d", len(learnings))
	}
}

func TestRepoLearner_LearnFromRepo_NoNewCommits(t *testing.T) {
	bareDir := t.TempDir()
	setupBareRepo(t, bareDir)

	cloneDir := t.TempDir()
	rl := NewRepoLearner(cloneDir, "claude", true)

	repo := LearningRepo{Name: "test-repo", URL: bareDir}

	// First run — analyzes (dry run, so nil learnings)
	_, err := rl.LearnFromRepo(context.Background(), repo)
	if err != nil {
		t.Fatalf("LearnFromRepo (first): %v", err)
	}

	// Second run — no new commits, should return nil
	learnings, err := rl.LearnFromRepo(context.Background(), repo)
	if err != nil {
		t.Fatalf("LearnFromRepo (second): %v", err)
	}
	if learnings != nil {
		t.Error("no new commits should return nil learnings")
	}
}

func TestConvertToFindings(t *testing.T) {
	learnings := []Learning{
		{
			RepoName:    "agentflow",
			Pattern:     "Retry with backoff",
			Description: "Linear backoff on retries",
			Relevance:   8,
			Component:   "engine/escalation",
			Suggestion:  "Add backoff to retry loop",
		},
	}
	findings := ConvertToFindings(learnings)
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(findings))
	}
	if findings[0].Category != "competitors" {
		t.Errorf("category = %q, want competitors", findings[0].Category)
	}
	if findings[0].SourceURL != "repo:agentflow" {
		t.Errorf("sourceURL = %q, want repo:agentflow", findings[0].SourceURL)
	}
	if findings[0].Title != "[agentflow] Retry with backoff" {
		t.Errorf("title = %q, want [agentflow] Retry with backoff", findings[0].Title)
	}
}

func TestStoreLearnings(t *testing.T) {
	dir := t.TempDir()
	learnings := []Learning{
		{RepoName: "test", Pattern: "Pattern 1", Description: "Desc 1", ExtractedAt: time.Now().UTC()},
		{RepoName: "test", Pattern: "Pattern 2", Description: "Desc 2", ExtractedAt: time.Now().UTC()},
	}

	err := StoreLearnings(dir, learnings)
	if err != nil {
		t.Fatalf("StoreLearnings: %v", err)
	}

	// Read back
	date := time.Now().UTC().Format("2006-01-02")
	loaded, err := LoadLearnings(filepath.Join(dir, date+".jsonl"))
	if err != nil {
		t.Fatalf("LoadLearnings: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("loaded = %d, want 2", len(loaded))
	}
	if loaded[0].Pattern != "Pattern 1" {
		t.Errorf("pattern = %q, want 'Pattern 1'", loaded[0].Pattern)
	}
}

func TestStoreLearnings_Empty(t *testing.T) {
	dir := t.TempDir()
	err := StoreLearnings(dir, nil)
	if err != nil {
		t.Fatalf("StoreLearnings(nil): %v", err)
	}
}

func TestLoadLearnings_MissingFile(t *testing.T) {
	_, err := LoadLearnings("/nonexistent/file.jsonl")
	if err == nil {
		t.Fatal("should fail on missing file")
	}
}

func TestExtractJSONArray(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{`[{"a": 1}]`, `[{"a": 1}]`},
		{`Here are the results: [{"a": 1}] done`, `[{"a": 1}]`},
		{`no array here`, `no array here`},
	}
	for _, tc := range tests {
		got := extractJSONArray(tc.in)
		if got != tc.want {
			t.Errorf("extractJSONArray(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestReadBaseline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "baseline")

	// Missing file
	if got := readBaseline(path); got != "" {
		t.Errorf("missing baseline should return empty, got %q", got)
	}

	// Written file
	if err := os.WriteFile(path, []byte("abc123\n"), 0644); err != nil {
		t.Fatalf("write baseline: %v", err)
	}
	if got := readBaseline(path); got != "abc123" {
		t.Errorf("baseline = %q, want abc123", got)
	}
}

func TestLearningReposForDay(t *testing.T) {
	// Monday (weekday 1) should return a repo
	monday := time.Date(2026, 4, 13, 6, 0, 0, 0, time.UTC) // Monday
	repos := LearningReposForDay(monday)
	if len(repos) != 1 {
		t.Fatalf("Monday: expected 1 repo, got %d", len(repos))
	}
	if repos[0].Name == "" {
		t.Error("expected non-empty repo name")
	}

	// Saturday should return nil
	saturday := time.Date(2026, 4, 11, 6, 0, 0, 0, time.UTC) // Saturday
	repos = LearningReposForDay(saturday)
	if len(repos) != 0 {
		t.Errorf("Saturday: expected 0 repos, got %d", len(repos))
	}

	// Sunday should return nil
	sunday := time.Date(2026, 4, 12, 6, 0, 0, 0, time.UTC) // Sunday
	repos = LearningReposForDay(sunday)
	if len(repos) != 0 {
		t.Errorf("Sunday: expected 0 repos, got %d", len(repos))
	}

	// Different weekdays should rotate through repos
	tuesday := time.Date(2026, 4, 14, 6, 0, 0, 0, time.UTC) // Tuesday
	tuesdayRepos := LearningReposForDay(tuesday)
	if len(tuesdayRepos) != 1 {
		t.Fatalf("Tuesday: expected 1 repo, got %d", len(tuesdayRepos))
	}
	// Monday and Tuesday may or may not be the same depending on the index,
	// but both should have valid names
	if tuesdayRepos[0].Name == "" {
		t.Error("Tuesday repo should have non-empty name")
	}
}

func TestTruncate(t *testing.T) {
	if got := truncateStr("hello world", 5); got != "hello..." {
		t.Errorf("truncateStr = %q, want 'hello...'", got)
	}
	if got := truncateStr("hi", 10); got != "hi" {
		t.Errorf("truncateStr short = %q, want 'hi'", got)
	}
}

// setupBareRepo creates a bare git repo with one commit for testing.
func setupBareRepo(t *testing.T, dir string) {
	t.Helper()
	// Init a regular repo, add a commit, then we'll clone from it
	tmpRepo := filepath.Join(t.TempDir(), "src")
	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = tmpRepo
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v (%s)", args, err, string(out))
		}
	}
	if err := os.MkdirAll(tmpRepo, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	run("git", "init")
	if err := os.WriteFile(filepath.Join(tmpRepo, "README.md"), []byte("# Test"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	run("git", "add", ".")
	run("git", "commit", "-m", "initial")

	// Clone as bare to the target dir
	cmd := exec.Command("git", "clone", "--bare", tmpRepo, dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone bare: %v (%s)", err, string(out))
	}
}
