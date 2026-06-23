package cli

import (
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/llm"
)

// TestWiring_CodexProvider_BuildsClient verifies the "codex" provider (and its
// aliases) is wired into buildLLMClient and produces a Codex-with-fallback
// client, rather than falling through to the unsupported-provider error.
func TestWiring_CodexProvider_BuildsClient(t *testing.T) {
	for _, provider := range []string{"codex", "codex-cli", "openai-cli", "gpt-cli"} {
		client, err := buildLLMClient(provider, nil)
		if err != nil {
			t.Fatalf("provider %q should build, got error: %v", provider, err)
		}
		if _, ok := client.(*llm.CodexWithFallback); !ok {
			t.Fatalf("provider %q should yield *llm.CodexWithFallback, got %T", provider, client)
		}
	}
}

// TestWiring_CodexProvider_Planning verifies the codex provider is also
// available to the planning client builder.
func TestWiring_CodexProvider_Planning(t *testing.T) {
	client, err := buildPlanningClient("codex", false)
	if err != nil {
		t.Fatalf("planning client for codex should build, got: %v", err)
	}
	if client == nil {
		t.Fatal("planning client for codex is nil")
	}
}

func TestWiring_UnknownProvider_StillErrors(t *testing.T) {
	if _, err := buildLLMClient("definitely-not-a-provider", nil); err == nil {
		t.Fatal("unknown provider should still error")
	}
}
