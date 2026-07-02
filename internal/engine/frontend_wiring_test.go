package engine

import (
	"os"
	"strings"
	"testing"
)

// TestExecutor_WiresFrontendDetection guards against a dead-wire regression:
// the frontend design brief (agent.FrontendDesignBrief + PromptContext.
// IsFrontend) only fires if the executor actually calls detectFrontend and
// threads the flag into BOTH prompt paths (first dispatch via PromptContext
// and retries via TemplateContext). The prompt-injection unit tests cannot
// catch the executor never setting the flag.
func TestExecutor_WiresFrontendDetection(t *testing.T) {
	src, err := os.ReadFile("executor.go")
	if err != nil {
		t.Fatalf("read executor.go: %v", err)
	}
	code := string(src)

	if !strings.Contains(code, "detectFrontend(") {
		t.Error("executor.go must call detectFrontend — the design brief is otherwise dead code")
	}
	if got := strings.Count(code, "IsFrontend:"); got < 2 {
		t.Errorf("executor.go must thread IsFrontend into both the PromptContext and the retry TemplateContext, found %d assignment(s)", got)
	}
}
