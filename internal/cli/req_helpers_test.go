package cli

import (
	"os"
	"strings"
	"testing"
)

// buildPlanningClient: drive each branch by manipulating env keys + PATH.
// We can't easily verify the returned client's behaviour without making
// real API calls, but we can verify the routing logic (which client got
// picked, and the error message when nothing is available).

func TestBuildPlanningClient_AnthropicNoCreds(t *testing.T) {
	// Clear both API key and PATH so neither api nor cli backend is
	// available — should surface the "no LLM available" message.
	saveAndClearEnv(t, "ANTHROPIC_API_KEY")
	savePath := os.Getenv("PATH")
	t.Cleanup(func() { _ = os.Setenv("PATH", savePath) })
	_ = os.Setenv("PATH", "/no/such/dir")

	_, err := buildPlanningClient("anthropic", false)
	if err == nil {
		t.Fatal("expected error when no client available")
	}
	if !strings.Contains(err.Error(), "no LLM available") {
		t.Errorf("expected 'no LLM available' in error, got: %v", err)
	}
}

func TestBuildPlanningClient_AnthropicWithAPIKey(t *testing.T) {
	save := os.Getenv("ANTHROPIC_API_KEY")
	t.Cleanup(func() { _ = os.Setenv("ANTHROPIC_API_KEY", save) })
	_ = os.Setenv("ANTHROPIC_API_KEY", "sk-test")

	client, err := buildPlanningClient("anthropic", false)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if client == nil {
		t.Error("expected non-nil client when API key present")
	}
}

func TestBuildPlanningClient_GodmodePersists(t *testing.T) {
	// We can only verify the call doesn't error — godmode threads
	// through to CLIClient construction which has its own assertion in
	// the llm package tests.
	save := os.Getenv("ANTHROPIC_API_KEY")
	t.Cleanup(func() { _ = os.Setenv("ANTHROPIC_API_KEY", save) })
	_ = os.Setenv("ANTHROPIC_API_KEY", "sk-test")

	client, err := buildPlanningClient("anthropic", true)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if client == nil {
		t.Error("expected non-nil client with godmode + API key")
	}
}

func TestBuildPlanningClient_OpenAINoCreds(t *testing.T) {
	saveAndClearEnv(t, "OPENAI_API_KEY")
	savePath := os.Getenv("PATH")
	t.Cleanup(func() { _ = os.Setenv("PATH", savePath) })
	_ = os.Setenv("PATH", "/no/such/dir")

	_, err := buildPlanningClient("openai", false)
	if err == nil {
		t.Error("expected error when no openai key")
	}
}

func TestBuildPlanningClient_GoogleNoCreds(t *testing.T) {
	saveAndClearEnv(t, "GOOGLE_AI_API_KEY")
	savePath := os.Getenv("PATH")
	t.Cleanup(func() { _ = os.Setenv("PATH", savePath) })
	_ = os.Setenv("PATH", "/no/such/dir")

	_, err := buildPlanningClient("google", false)
	if err == nil {
		t.Error("expected error when no google key and no claude CLI")
	}
}

// saveAndClearEnv stashes a single env var, clears it, and restores on
// test cleanup. Used by the buildPlanningClient tests to control which
// backend is reachable.
func saveAndClearEnv(t *testing.T, key string) {
	t.Helper()
	prev, had := os.LookupEnv(key)
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, prev)
		} else {
			_ = os.Unsetenv(key)
		}
	})
	_ = os.Unsetenv(key)
}
