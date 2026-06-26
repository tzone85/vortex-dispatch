package engine

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// projectKind identifies the build system used by a directory.
type projectKind int

const (
	projectUnknown projectKind = iota
	projectGo
	projectNode
	projectRust
	projectPython
)

// integrationBuildTimeout is the maximum time allowed for a post-merge build.
const integrationBuildTimeout = 60 * time.Second

// detectProjectKind inspects repoDir for well-known build system markers and
// returns the matching kind. Returns projectUnknown when no recognisable
// marker is found (caller treats it as a no-op).
func detectProjectKind(repoDir string) projectKind {
	switch {
	case fileExists(filepath.Join(repoDir, "go.mod")):
		return projectGo
	case fileExists(filepath.Join(repoDir, "Cargo.toml")):
		return projectRust
	case fileExists(filepath.Join(repoDir, "package.json")):
		return projectNode
	case fileExists(filepath.Join(repoDir, "pyproject.toml")),
		fileExists(filepath.Join(repoDir, "setup.py")),
		fileExists(filepath.Join(repoDir, "setup.cfg")),
		fileExists(filepath.Join(repoDir, "requirements.txt")):
		// Python has no compile step; the integration gate is the test suite,
		// which is exactly what catches composition gaps (routes declared but
		// not wired, app factory not assembled) that import/compile cleanly.
		return projectPython
	default:
		return projectUnknown
	}
}

// integrationCommand is a PURE function (no I/O beyond reading repo markers
// already classified by the caller) that selects the post-merge verification
// command for a project kind. It returns (argv, true) when a command should
// run, or (nil, false) when the kind has no runnable verification (skip).
//
// Detection rules (first match wins):
//   - go.mod        → go build ./...
//   - Cargo.toml    → cargo build
//   - package.json  → npm run build   (only if a "build" script exists)
//   - pyproject/... → python3 -m pytest -q  (only if the project ships tests)
//
// Keeping this separate from execution makes command selection unit-testable
// without invoking the toolchain.
func integrationCommand(kind projectKind, repoDir string) ([]string, bool) {
	switch kind {
	case projectGo:
		return []string{"go", "build", "./..."}, true
	case projectRust:
		return []string{"cargo", "build"}, true
	case projectNode:
		if !hasNPMBuildScript(repoDir) {
			return nil, false // no "build" script — skip
		}
		return []string{"npm", "run", "build"}, true
	case projectPython:
		if !hasPythonTests(repoDir) {
			return nil, false // no test suite to run — skip
		}
		return []string{"python3", "-m", "pytest", "-q"}, true
	default:
		return nil, false // unrecognised build system — best-effort no-op
	}
}

// runIntegrationBuild runs the project's post-merge verification command against
// repoDir and returns combined stderr+stdout on failure, or nil on success.
//
// When no verification command is applicable the function returns nil
// (best-effort). For Python the verification is the test suite: a FastAPI/Flask
// app whose routes are declared but never wired imports cleanly, so only an
// assembled-app test — required by the planner's acceptance criteria — fails
// here, which dispatches a TechLeadFixer integration-fix story.
func runIntegrationBuild(repoDir string) error {
	kind := detectProjectKind(repoDir)

	argv, ok := integrationCommand(kind, repoDir)
	if !ok {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), integrationBuildTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		output := bytes.TrimSpace(out)
		if len(output) == 0 {
			return fmt.Errorf("verification command %v exited with no output: %w", argv, err)
		}
		return fmt.Errorf("%s", output)
	}
	return nil
}

// hasPythonTests reports whether repoDir ships a runnable test suite, so the
// integration gate only invokes pytest when there is something to run (mirrors
// the Node "build script must exist" guard). True when a tests/ directory
// exists, pytest is configured in pyproject.toml/setup.cfg, or a top-level
// test_*.py / *_test.py file is present.
func hasPythonTests(repoDir string) bool {
	if info, err := os.Stat(filepath.Join(repoDir, "tests")); err == nil && info.IsDir() {
		return true
	}
	for _, cfg := range []string{"pyproject.toml", "setup.cfg", "pytest.ini", "tox.ini"} {
		if data, err := os.ReadFile(filepath.Join(repoDir, cfg)); err == nil {
			if strings.Contains(string(data), "pytest") {
				return true
			}
		}
	}
	entries, err := os.ReadDir(repoDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".py") &&
			(strings.HasPrefix(name, "test_") || strings.HasSuffix(name, "_test.py")) {
			return true
		}
	}
	return false
}

// hasNPMBuildScript reads package.json and checks for a "build" script entry
// without importing a full JSON parser — a simple substring check suffices.
func hasNPMBuildScript(repoDir string) bool {
	data, err := os.ReadFile(filepath.Join(repoDir, "package.json"))
	if err != nil {
		return false
	}
	// Fast substring check: "build": "<something>"
	return strings.Contains(string(data), `"build"`)
}
