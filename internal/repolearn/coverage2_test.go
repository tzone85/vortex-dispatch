package repolearn

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// detectBranchPattern
// ---------------------------------------------------------------------------

func TestDetectBranchPattern_MainOnly(t *testing.T) {
	dir := t.TempDir()
	exec.Command("git", "init", dir).Run()
	exec.Command("git", "-C", dir, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", dir, "config", "user.name", "Test").Run()
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0o644)
	exec.Command("git", "-C", dir, "add", ".").Run()
	exec.Command("git", "-C", dir, "commit", "-m", "init").Run()

	result := detectBranchPattern(dir)
	if result != "main-only" {
		t.Errorf("expected 'main-only' for single-branch repo, got '%s'", result)
	}
}

func TestDetectBranchPattern_MultipleBranches(t *testing.T) {
	dir := t.TempDir()
	exec.Command("git", "init", dir).Run()
	exec.Command("git", "-C", dir, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", dir, "config", "user.name", "Test").Run()
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0o644)
	exec.Command("git", "-C", dir, "add", ".").Run()
	exec.Command("git", "-C", dir, "commit", "-m", "init").Run()

	// Create feature branches — the function detects pattern prefixes
	// from branch names with slashes. Local branches (no remote) are
	// checked after remote branches fail. The logic looks for "prefix/rest"
	// in local branch names. For local branches (no origin/), the first
	// slash in "feature/auth" gives prefix="feature" directly.
	for _, branch := range []string{"feature/auth", "feature/login", "fix/bug1", "fix/bug2", "random"} {
		exec.Command("git", "-C", dir, "branch", branch).Run()
	}

	result := detectBranchPattern(dir)
	// Result depends on branch parsing of local branches — at minimum
	// should not panic and should return something
	if result == "" {
		t.Errorf("expected non-empty result for multiple branches")
	}
}

func TestDetectBranchPattern_NotGitDir(t *testing.T) {
	dir := t.TempDir()
	result := detectBranchPattern(dir)
	// Should fall back gracefully
	if result != "" && result != "main-only" && result != "freeform" {
		t.Errorf("unexpected result for non-git dir: '%s'", result)
	}
}

// ---------------------------------------------------------------------------
// detectCommitFormat
// ---------------------------------------------------------------------------

func TestDetectCommitFormat_ConventionalCommits(t *testing.T) {
	dir := t.TempDir()
	exec.Command("git", "init", dir).Run()
	exec.Command("git", "-C", dir, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", dir, "config", "user.name", "Test").Run()

	// Create several conventional commits
	for _, msg := range []string{
		"feat: add auth",
		"fix: resolve login bug",
		"docs: update README",
		"refactor: clean up utils",
		"test: add unit tests",
		"chore: update deps",
		"perf: optimize query",
		"ci: add pipeline",
	} {
		os.WriteFile(filepath.Join(dir, "file.txt"), []byte(msg), 0o644)
		exec.Command("git", "-C", dir, "add", ".").Run()
		exec.Command("git", "-C", dir, "commit", "-m", msg).Run()
	}

	result := detectCommitFormat(dir)
	if result != "conventional" {
		t.Errorf("expected 'conventional', got '%s'", result)
	}
}

func TestDetectCommitFormat_TicketPrefix(t *testing.T) {
	dir := t.TempDir()
	exec.Command("git", "init", dir).Run()
	exec.Command("git", "-C", dir, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", dir, "config", "user.name", "Test").Run()

	for _, msg := range []string{
		"JIRA-123 fix login issue",
		"PROJ-456 add new feature",
		"BUG-789 resolve crash",
		"TASK-111 update config",
		"FEAT-222 implement search",
	} {
		os.WriteFile(filepath.Join(dir, "file.txt"), []byte(msg), 0o644)
		exec.Command("git", "-C", dir, "add", ".").Run()
		exec.Command("git", "-C", dir, "commit", "-m", msg).Run()
	}

	result := detectCommitFormat(dir)
	if result != "ticket-prefix" {
		t.Errorf("expected 'ticket-prefix', got '%s'", result)
	}
}

func TestDetectCommitFormat_Freeform(t *testing.T) {
	dir := t.TempDir()
	exec.Command("git", "init", dir).Run()
	exec.Command("git", "-C", dir, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", dir, "config", "user.name", "Test").Run()

	for _, msg := range []string{
		"Fixed the thing",
		"Added some stuff",
		"Updated code",
		"Changed something",
		"More changes",
	} {
		os.WriteFile(filepath.Join(dir, "file.txt"), []byte(msg), 0o644)
		exec.Command("git", "-C", dir, "add", ".").Run()
		exec.Command("git", "-C", dir, "commit", "-m", msg).Run()
	}

	result := detectCommitFormat(dir)
	if result != "freeform" {
		t.Errorf("expected 'freeform', got '%s'", result)
	}
}

func TestDetectCommitFormat_NotGitDir(t *testing.T) {
	dir := t.TempDir()
	result := detectCommitFormat(dir)
	if result != "freeform" {
		t.Errorf("expected 'freeform' for non-git dir, got '%s'", result)
	}
}

// ---------------------------------------------------------------------------
// detectTestConfig — various languages
// ---------------------------------------------------------------------------

func TestDetectTestConfig_Python_WithPytest(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "pytest.ini"), []byte("[pytest]"), 0o644)

	tc := detectTestConfig(dir, "python")
	if tc.TestFramework != "pytest" {
		t.Errorf("expected pytest framework, got '%s'", tc.TestFramework)
	}
	if tc.TestCommand != "pytest" {
		t.Errorf("expected 'pytest' command, got '%s'", tc.TestCommand)
	}
}

func TestDetectTestConfig_Python_Pyproject(t *testing.T) {
	dir := t.TempDir()
	pyproject := `[tool.pytest.ini_options]
minversion = "6.0"
`
	os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte(pyproject), 0o644)

	tc := detectTestConfig(dir, "python")
	if tc.TestFramework != "pytest" {
		t.Errorf("expected pytest framework, got '%s'", tc.TestFramework)
	}
}

func TestDetectTestConfig_Python_DefaultUnittest(t *testing.T) {
	dir := t.TempDir()

	tc := detectTestConfig(dir, "python")
	if tc.TestFramework != "unittest" {
		t.Errorf("expected unittest framework, got '%s'", tc.TestFramework)
	}
}

func TestDetectTestConfig_Rust(t *testing.T) {
	dir := t.TempDir()

	tc := detectTestConfig(dir, "rust")
	if tc.TestCommand != "cargo test" {
		t.Errorf("expected 'cargo test', got '%s'", tc.TestCommand)
	}
	if tc.TestFramework != "cargo test" {
		t.Errorf("expected 'cargo test' framework, got '%s'", tc.TestFramework)
	}
}

func TestDetectTestConfig_Ruby_WithSpec(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "spec"), 0o755)

	tc := detectTestConfig(dir, "ruby")
	if tc.TestFramework != "rspec" {
		t.Errorf("expected rspec framework, got '%s'", tc.TestFramework)
	}
}

func TestDetectTestConfig_Ruby_NoSpec(t *testing.T) {
	dir := t.TempDir()

	tc := detectTestConfig(dir, "ruby")
	if tc.TestFramework != "minitest" {
		t.Errorf("expected minitest framework, got '%s'", tc.TestFramework)
	}
}

func TestDetectTestConfig_Java(t *testing.T) {
	dir := t.TempDir()

	tc := detectTestConfig(dir, "java")
	if tc.TestFramework != "junit" {
		t.Errorf("expected junit framework, got '%s'", tc.TestFramework)
	}
}

func TestDetectTestConfig_WithMakefileTestTarget(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Makefile"), []byte("test:\n\tgo test ./...\n"), 0o644)

	tc := detectTestConfig(dir, "go")
	if tc.TestCommand != "make test" {
		t.Errorf("expected 'make test', got '%s'", tc.TestCommand)
	}
}

func TestDetectTestConfig_WithTestDirs(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "test"), 0o755)
	os.MkdirAll(filepath.Join(dir, "tests"), 0o755)
	os.MkdirAll(filepath.Join(dir, "__tests__"), 0o755)

	tc := detectTestConfig(dir, "unknown")
	if len(tc.TestDirs) < 3 {
		t.Errorf("expected at least 3 test dirs, got %d: %v", len(tc.TestDirs), tc.TestDirs)
	}
}

// ---------------------------------------------------------------------------
// detectBuildConfig — various languages
// ---------------------------------------------------------------------------

func TestDetectBuildConfig_Rust(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]\nname = \"test\"\n"), 0o644)

	bc := detectBuildConfig(dir, "rust")
	if bc.BuildCommand != "cargo build" {
		t.Errorf("expected 'cargo build', got '%s'", bc.BuildCommand)
	}
	if bc.LintCommand != "cargo clippy" {
		t.Errorf("expected 'cargo clippy', got '%s'", bc.LintCommand)
	}
}

func TestDetectBuildConfig_Python(t *testing.T) {
	dir := t.TempDir()

	bc := detectBuildConfig(dir, "python")
	// Python has no default build command unless pip/poetry detected
	_ = bc // just verify no panic
}

// ---------------------------------------------------------------------------
// extractNodeVersion
// ---------------------------------------------------------------------------

func TestExtractNodeVersion_FromNvmrc(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".nvmrc"), []byte("18.17.0\n"), 0o644)

	v := extractNodeVersion(dir)
	if v != "18.17.0" {
		t.Errorf("expected '18.17.0', got '%s'", v)
	}
}

func TestExtractNodeVersion_FromNodeVersion(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".node-version"), []byte("20.0.0\n"), 0o644)

	v := extractNodeVersion(dir)
	if v != "20.0.0" {
		t.Errorf("expected '20.0.0', got '%s'", v)
	}
}

func TestExtractNodeVersion_FromPackageJSON(t *testing.T) {
	dir := t.TempDir()
	pkg := `{"engines":{"node":">=16.0.0"}}`
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkg), 0o644)

	v := extractNodeVersion(dir)
	if v != ">=16.0.0" {
		t.Errorf("expected '>=16.0.0', got '%s'", v)
	}
}

func TestExtractNodeVersion_NoFile(t *testing.T) {
	dir := t.TempDir()
	v := extractNodeVersion(dir)
	if v != "" {
		t.Errorf("expected empty string, got '%s'", v)
	}
}

// ---------------------------------------------------------------------------
// SaveProfile — creates directory
// ---------------------------------------------------------------------------

func TestSaveProfile_CreatesDir(t *testing.T) {
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "deep", "nested")

	profile := &RepoProfile{
		TechStack: TechStackDetail{PrimaryLanguage: "go"},
	}

	err := SaveProfile(profileDir, profile)
	if err != nil {
		t.Fatalf("save profile: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(ProfilePath(profileDir)); os.IsNotExist(err) {
		t.Error("expected profile file to exist")
	}
}

// ---------------------------------------------------------------------------
// LoadProfile — file not parseable
// ---------------------------------------------------------------------------

func TestLoadProfile_CorruptedJSON(t *testing.T) {
	dir := t.TempDir()
	profilePath := ProfilePath(dir)
	os.MkdirAll(filepath.Dir(profilePath), 0o755)
	os.WriteFile(profilePath, []byte("not json"), 0o644)

	_, err := LoadProfile(dir)
	if err == nil {
		t.Error("expected error for corrupted JSON")
	}
}

// ---------------------------------------------------------------------------
// daysSinceLastCommit
// ---------------------------------------------------------------------------

func TestDaysSinceLastCommit_NoGitDir(t *testing.T) {
	dir := t.TempDir()
	days := daysSinceLastCommit(dir)
	if days != 0 {
		t.Errorf("expected 0 for non-git dir (error fallback), got %d", days)
	}
}

func TestDaysSinceLastCommit_FreshRepo(t *testing.T) {
	dir := t.TempDir()
	exec.Command("git", "init", dir).Run()
	exec.Command("git", "-C", dir, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", dir, "config", "user.name", "Test").Run()
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0o644)
	exec.Command("git", "-C", dir, "add", ".").Run()
	exec.Command("git", "-C", dir, "commit", "-m", "init").Run()

	days := daysSinceLastCommit(dir)
	if days < 0 || days > 1 {
		t.Errorf("expected 0 or 1 days since fresh commit, got %d", days)
	}
}

// ---------------------------------------------------------------------------
// countFilesInDir
// ---------------------------------------------------------------------------

func TestCountFilesInDir_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	count := countFilesInDir(dir)
	if count != 0 {
		t.Errorf("expected 0 files, got %d", count)
	}
}

func TestCountFilesInDir_WithFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o644)
	os.MkdirAll(filepath.Join(dir, "subdir"), 0o755)

	count := countFilesInDir(dir)
	if count != 2 {
		t.Errorf("expected 2 files (no dirs), got %d", count)
	}
}

func TestCountFilesInDir_NonexistentDir(t *testing.T) {
	count := countFilesInDir("/nonexistent/path")
	if count != 0 {
		t.Errorf("expected 0 for nonexistent dir, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// detectJSFramework
// ---------------------------------------------------------------------------

func TestDetectJSFramework_NextJS(t *testing.T) {
	dir := t.TempDir()
	pkg := `{"dependencies":{"next":"14.0.0","react":"18.0.0"}}`
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkg), 0o644)

	fw := detectJSFramework(dir)
	if fw != "Next.js" {
		t.Errorf("expected 'Next.js', got '%s'", fw)
	}
}

func TestDetectJSFramework_React(t *testing.T) {
	dir := t.TempDir()
	pkg := `{"dependencies":{"react":"18.0.0"}}`
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkg), 0o644)

	fw := detectJSFramework(dir)
	if fw != "React" {
		t.Errorf("expected 'React', got '%s'", fw)
	}
}

func TestDetectJSFramework_NoPackageJSON(t *testing.T) {
	dir := t.TempDir()
	fw := detectJSFramework(dir)
	if fw != "" {
		t.Errorf("expected empty string, got '%s'", fw)
	}
}

func TestDetectJSFramework_Angular(t *testing.T) {
	dir := t.TempDir()
	pkg := `{"dependencies":{"@angular/core":"17.0.0"}}`
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkg), 0o644)

	fw := detectJSFramework(dir)
	if fw != "Angular" {
		t.Errorf("expected 'Angular', got '%s'", fw)
	}
}

// ---------------------------------------------------------------------------
// detectPythonFramework
// ---------------------------------------------------------------------------

func TestDetectPythonFramework_Django(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("django==4.2\ncelery==5.3\n"), 0o644)

	fw := detectPythonFramework(dir)
	if fw != "Django" {
		t.Errorf("expected 'Django', got '%s'", fw)
	}
}

func TestDetectPythonFramework_NoFiles(t *testing.T) {
	dir := t.TempDir()
	fw := detectPythonFramework(dir)
	if fw != "" {
		t.Errorf("expected empty string, got '%s'", fw)
	}
}

// ---------------------------------------------------------------------------
// detectNPMBuildConfig
// ---------------------------------------------------------------------------

func TestDetectNPMBuildConfig_WithBuildScript(t *testing.T) {
	dir := t.TempDir()
	pkg := `{"scripts":{"build":"tsc","lint":"eslint .","format":"prettier --write ."}}`
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkg), 0o644)

	bc := detectNPMBuildConfig(dir, BuildConfig{})
	if bc.BuildCommand != "npm run build" {
		t.Errorf("expected 'npm run build', got '%s'", bc.BuildCommand)
	}
	if bc.LintCommand != "npm run lint" {
		t.Errorf("expected 'npm run lint', got '%s'", bc.LintCommand)
	}
	if bc.FormatCommand != "npm run format" {
		t.Errorf("expected 'npm run format', got '%s'", bc.FormatCommand)
	}
}

func TestDetectNPMBuildConfig_NoPackageJSON(t *testing.T) {
	dir := t.TempDir()
	bc := detectNPMBuildConfig(dir, BuildConfig{})
	if bc.BuildCommand != "" {
		t.Errorf("expected empty build command, got '%s'", bc.BuildCommand)
	}
}

// ---------------------------------------------------------------------------
// detectNPMTestConfig
// ---------------------------------------------------------------------------

func TestDetectNPMTestConfig_WithJest(t *testing.T) {
	dir := t.TempDir()
	pkg := `{"scripts":{"test":"jest"},"devDependencies":{"jest":"29.0.0"}}`
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkg), 0o644)

	tc := detectNPMTestConfig(dir, TestConfig{})
	if tc.TestCommand != "npm test" {
		t.Errorf("expected 'npm test', got '%s'", tc.TestCommand)
	}
}

func TestDetectNPMTestConfig_WithVitest(t *testing.T) {
	dir := t.TempDir()
	pkg := `{"scripts":{"test":"vitest"},"devDependencies":{"vitest":"1.0.0"}}`
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkg), 0o644)

	tc := detectNPMTestConfig(dir, TestConfig{})
	if tc.TestFramework != "vitest" {
		t.Errorf("expected 'vitest' framework, got '%s'", tc.TestFramework)
	}
}
