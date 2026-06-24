package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/llm"
)

func gitInit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@t.t"},
		{"config", "user.name", "t"},
	} {
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v — %s", args, err, out)
		}
	}
}

// generateDocumentation must, after writing the README, deterministically
// produce VALID SVG diagrams and link them — the factory documentation loop end
// to end. This is the wiring guard: if the diagram call is ever dropped from
// generateDocumentation, this fails.
func TestGenerateDocumentation_ProducesSVGDiagrams(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module demo\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	client := llm.NewReplayClient(
		llm.CompletionResponse{Content: "# Demo\n\nA demo project."}, // README
		llm.CompletionResponse{Content: validArchSVG},                // architecture.svg
		llm.CompletionResponse{Content: validArchSVG},                // sequence.svg
	)

	generateDocumentation(context.Background(), dir, "Build a demo", []string{"s-001: thing"}, client, "m")

	for _, rel := range []string{"docs/architecture.svg", "docs/sequence.svg"} {
		data, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			t.Fatalf("expected %s to exist: %v", rel, err)
		}
		if err := validateSVG(string(data)); err != nil {
			t.Fatalf("%s not valid SVG: %v", rel, err)
		}
	}

	readme, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readme), "docs/architecture.svg") {
		t.Fatal("README does not reference the architecture diagram")
	}

	// The diagrams must have been committed, not left dirty.
	st := exec.Command("git", "status", "--porcelain")
	st.Dir = dir
	out, _ := st.Output()
	if strings.Contains(string(out), "docs/") {
		t.Fatalf("docs left uncommitted: %s", out)
	}
}
