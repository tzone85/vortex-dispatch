package llm

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"testing"
)

// TestClaudeCLIClient_UsageSurfaced pins that the CLI envelope's usage block
// flows into CompletionResponse.Usage — the data subscription-mode cost
// metering needs to record real token volume (with est_usd=0). Runs the
// Complete path against a fake `claude` binary that emits a realistic
// --output-format json envelope.
func TestClaudeCLIClient_UsageSurfaced(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary is unix-only")
	}
	if _, err := exec.LookPath("claude"); err == nil {
		t.Skip("real claude CLI installed; refusing to shadow it in this test")
	}

	binDir := t.TempDir()
	script := "#!/bin/sh\necho '{\"result\":\"ok\",\"is_error\":false,\"usage\":{\"input_tokens\":123,\"output_tokens\":45}}'\n"
	fake := binDir + "/claude"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}

	client := NewClaudeCLIClientWithPath(fake)
	resp, err := client.Complete(context.Background(), CompletionRequest{
		Model:    "claude-haiku-4-5",
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("content = %q, want ok", resp.Content)
	}
	if resp.Usage.InputTokens != 123 || resp.Usage.OutputTokens != 45 {
		t.Errorf("usage = %+v, want {123 45} — envelope usage must surface for cost metering", resp.Usage)
	}
}
