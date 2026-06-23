package llm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFakeCodex writes an executable shell script that stands in for the codex
// binary. The body runs with the real `codex exec ...` argv, so scripts can
// locate the `-o <path>` output file the same way the client passes it.
func writeFakeCodex(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "codex")
	script := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}
	return path
}

// findOutFlag extracts the value after -o from the argv — shared snippet for
// the fake scripts.
const findOutFlag = `out=""; prev=""; for a in "$@"; do if [ "$prev" = "-o" ]; then out="$a"; fi; prev="$a"; done`

func TestCodexCLIClient_Success(t *testing.T) {
	fake := writeFakeCodex(t, findOutFlag+`
printf '%s' "  Hello from Codex  " > "$out"`)

	c := NewCodexCLIClientWithPath(fake)
	resp, err := c.Complete(context.Background(), CompletionRequest{
		Model:    "gpt-5.5",
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "Hello from Codex" {
		t.Fatalf("expected trimmed content, got %q", resp.Content)
	}
	if resp.Model != "gpt-5.5" {
		t.Fatalf("expected model gpt-5.5, got %q", resp.Model)
	}
}

func TestCodexCLIClient_DefaultModel(t *testing.T) {
	fake := writeFakeCodex(t, findOutFlag+`
printf '%s' "ok" > "$out"`)

	c := NewCodexCLIClientWithPath(fake)
	resp, err := c.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Model != DefaultCodexModel {
		t.Fatalf("empty req.Model should default to %q, got %q", DefaultCodexModel, resp.Model)
	}
}

func TestCodexCLIClient_ErrorEnvelope(t *testing.T) {
	// Codex exits 0 but writes nothing to -o and prints an ERROR line when the
	// model is rejected. The client must surface that as an error, not success.
	fake := writeFakeCodex(t, findOutFlag+`
echo 'ERROR: {"type":"error","status":400,"error":{"type":"invalid_request_error","message":"The '"'"'gpt-5'"'"' model is not supported when using Codex with a ChatGPT account."}}'
: > "$out"`)

	c := NewCodexCLIClientWithPath(fake)
	_, err := c.Complete(context.Background(), CompletionRequest{
		Model:    "gpt-5",
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for rejected model, got nil")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("error should carry the codex message, got %v", err)
	}
}

func TestCodexCLIClient_EmptyOutput(t *testing.T) {
	fake := writeFakeCodex(t, findOutFlag+`
: > "$out"`)

	c := NewCodexCLIClientWithPath(fake)
	_, err := c.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for empty codex output, got nil")
	}
}

func TestExtractCodexError(t *testing.T) {
	out := `hook: SessionStart
ERROR: {"type":"error","status":400,"error":{"type":"invalid_request_error","message":"model not supported"}}`
	if got := extractCodexError(out); got != "model not supported" {
		t.Fatalf("expected extracted message, got %q", got)
	}
	if got := extractCodexError("just some logs\nno error here"); got != "" {
		t.Fatalf("expected empty for non-error output, got %q", got)
	}
}

func TestCodexWithFallback_FallsBackWithFallbackModel(t *testing.T) {
	primary := NewErrorClient(&APIError{StatusCode: 429, Message: "rate limited", Retryable: true})
	fallback := NewReplayClient(CompletionResponse{Content: "from opus", Model: "claude-opus-4-7"})

	c := NewCodexWithFallback(primary, fallback, "claude-opus-4-7")
	resp, err := c.Complete(context.Background(), CompletionRequest{Model: "gpt-5.5"})
	if err != nil {
		t.Fatalf("fallback should succeed: %v", err)
	}
	if resp.Content != "from opus" {
		t.Fatalf("expected fallback content, got %q", resp.Content)
	}
	// The fallback must be called with the fallback model, NOT gpt-5.5.
	if got := fallback.CallAt(0).Model; got != "claude-opus-4-7" {
		t.Fatalf("fallback called with wrong model: %q", got)
	}
}

func TestCodexWithFallback_PrimarySucceeds_NoFallback(t *testing.T) {
	primary := NewReplayClient(CompletionResponse{Content: "from codex", Model: "gpt-5.5"})
	fallback := NewReplayClient(CompletionResponse{Content: "should not be used"})

	c := NewCodexWithFallback(primary, fallback, "claude-opus-4-7")
	resp, err := c.Complete(context.Background(), CompletionRequest{Model: "gpt-5.5"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "from codex" {
		t.Fatalf("expected codex content, got %q", resp.Content)
	}
	if fallback.CallCount() != 0 {
		t.Fatalf("fallback must not be called when primary succeeds, got %d calls", fallback.CallCount())
	}
}

func TestFilterCodexEnv(t *testing.T) {
	in := []string{"PATH=/usr/bin", "OPENAI_API_KEY=sk-secret", "CODEX_API_KEY=ck", "HOME=/home/x"}
	out := FilterCodexEnv(in)
	joined := strings.Join(out, "\n")
	if strings.Contains(joined, "OPENAI_API_KEY") || strings.Contains(joined, "CODEX_API_KEY") {
		t.Fatalf("API keys should be stripped, got %v", out)
	}
	if !strings.Contains(joined, "PATH=/usr/bin") || !strings.Contains(joined, "HOME=/home/x") {
		t.Fatalf("non-secret env should be preserved, got %v", out)
	}
}
