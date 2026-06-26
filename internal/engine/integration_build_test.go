package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestIntegrationBuild_DetectsGoProject verifies that a directory containing
// go.mod is classified as projectGo.
func TestIntegrationBuild_DetectsGoProject(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	kind := detectProjectKind(dir)
	if kind != projectGo {
		t.Errorf("expected projectGo, got %v", kind)
	}
}

// TestIntegrationBuild_DetectsNodeProject verifies that package.json → projectNode.
func TestIntegrationBuild_DetectsNodeProject(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"test"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	kind := detectProjectKind(dir)
	if kind != projectNode {
		t.Errorf("expected projectNode, got %v", kind)
	}
}

// TestIntegrationBuild_DetectsRustProject verifies that Cargo.toml → projectRust.
func TestIntegrationBuild_DetectsRustProject(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]\nname = \"test\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	kind := detectProjectKind(dir)
	if kind != projectRust {
		t.Errorf("expected projectRust, got %v", kind)
	}
}

// TestIntegrationBuild_UnknownProjectIsNoop verifies that an empty directory
// returns projectUnknown and runIntegrationBuild returns nil.
func TestIntegrationBuild_UnknownProjectIsNoop(t *testing.T) {
	dir := t.TempDir()

	kind := detectProjectKind(dir)
	if kind != projectUnknown {
		t.Errorf("expected projectUnknown, got %v", kind)
	}

	if err := runIntegrationBuild(dir); err != nil {
		t.Errorf("expected nil for unknown project, got %v", err)
	}
}

// TestIntegrationBuild_NodeWithoutBuildScript verifies that package.json
// without a "build" script causes runIntegrationBuild to return nil (skip).
func TestIntegrationBuild_NodeWithoutBuildScript(t *testing.T) {
	dir := t.TempDir()
	// No "build" script in scripts section.
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"test","scripts":{"test":"echo ok"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runIntegrationBuild(dir); err != nil {
		t.Errorf("expected nil (no build script), got %v", err)
	}
}

// TestIntegrationBuild_GoProjectFailsOnBrokenCode verifies that a Go project
// with a syntax error causes runIntegrationBuild to return a non-nil error
// containing the build output.
func TestIntegrationBuild_GoProjectFailsOnBrokenCode(t *testing.T) {
	dir := t.TempDir()

	// Minimal go.mod
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module broken.example.com\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Syntactically broken Go file.
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() { BROKEN SYNTAX }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := runIntegrationBuild(dir)
	if err == nil {
		t.Fatal("expected build error for broken Go code, got nil")
	}
	// The error should contain something from the compiler.
	if len(err.Error()) == 0 {
		t.Error("expected non-empty error message from build failure")
	}
}

// TestIntegrationBuild_DetectsPythonProject pins the gap that let a Python
// FastAPI build (pulsereview) merge with endpoints declared-but-unwired and
// receive NO post-merge integration verification at all: Python was not a
// recognised project kind, so detectProjectKind returned projectUnknown and the
// gate was a silent no-op. Each canonical Python marker must now classify.
func TestIntegrationBuild_DetectsPythonProject(t *testing.T) {
	for _, marker := range []string{"pyproject.toml", "setup.py", "setup.cfg", "requirements.txt"} {
		t.Run(marker, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, marker), []byte("# marker\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if kind := detectProjectKind(dir); kind != projectPython {
				t.Errorf("marker %s: expected projectPython, got %v", marker, kind)
			}
		})
	}
}

// TestIntegrationCommand_PythonRunsTestsWhenPresent proves the recurrence guard:
// a Python project that ships a test suite is verified post-merge by running
// pytest. Combined with the planner's assembled-app acceptance criteria, a
// future "route declared but not wired" defect fails that suite here and
// dispatches an integration-fix story — instead of merging green as before.
func TestIntegrationCommand_PythonRunsTestsWhenPresent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte("[project]\nname='x'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "tests"), 0o755); err != nil {
		t.Fatal(err)
	}

	argv, ok := integrationCommand(detectProjectKind(dir), dir)
	if !ok {
		t.Fatal("expected a verification command for a Python project with tests/, got skip")
	}
	if got := strings.Join(argv, " "); !strings.Contains(got, "pytest") {
		t.Errorf("expected pytest verification command, got %q", got)
	}
}

// TestIntegrationCommand_PythonWithoutTestsSkips mirrors the Node "no build
// script → skip" guard: a Python project with no discoverable test suite must
// not invoke pytest (which would error with "no tests ran" and produce a false
// integration-fix).
func TestIntegrationCommand_PythonWithoutTestsSkips(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("fastapi\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, ok := integrationCommand(detectProjectKind(dir), dir); ok {
		t.Error("expected skip (no tests) for a Python project without a test suite")
	}
}

// TestIntegrationCommand_GoAndNodeUnchanged guards against a regression in the
// existing command selection while refactoring detection into a pure helper.
func TestIntegrationCommand_GoAndNodeUnchanged(t *testing.T) {
	goDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(goDir, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if argv, ok := integrationCommand(detectProjectKind(goDir), goDir); !ok || strings.Join(argv, " ") != "go build ./..." {
		t.Errorf("go: got %v ok=%v", argv, ok)
	}

	nodeDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(nodeDir, "package.json"), []byte(`{"scripts":{"build":"tsc"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if argv, ok := integrationCommand(detectProjectKind(nodeDir), nodeDir); !ok || strings.Join(argv, " ") != "npm run build" {
		t.Errorf("node: got %v ok=%v", argv, ok)
	}
}
