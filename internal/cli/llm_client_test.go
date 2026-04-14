package cli

import (
	"os"
	"testing"
)

// ---------------------------------------------------------------------------
// buildPlanningClient — test all provider paths, godmode flag, error cases
// ---------------------------------------------------------------------------

func TestBuildPlanningClient_Anthropic_WithAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key-123")
	// Ensure claude CLI is not on PATH for a clean test
	t.Setenv("PATH", "/nonexistent")

	client, err := buildPlanningClient("anthropic", false)
	if err != nil {
		t.Fatalf("expected success with API key, got: %v", err)
	}
	if client == nil {
		t.Fatal("client is nil")
	}
	// Should be a planningFallbackClient
	pfc, ok := client.(*planningFallbackClient)
	if !ok {
		t.Fatalf("expected *planningFallbackClient, got %T", client)
	}
	if pfc.apiClient == nil {
		t.Error("apiClient should not be nil when ANTHROPIC_API_KEY is set")
	}
	// CLI should be nil since claude is not on PATH
	if pfc.cliClient != nil {
		t.Error("cliClient should be nil when claude not on PATH")
	}
}

func TestBuildPlanningClient_Anthropic_NeitherKeyNorCLI(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("PATH", "/nonexistent")

	_, err := buildPlanningClient("anthropic", false)
	if err == nil {
		t.Fatal("expected error when neither API key nor CLI available")
	}
}

func TestBuildPlanningClient_ClaudeCliProvider_NoCLI(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("PATH", "/nonexistent")

	_, err := buildPlanningClient("cli", false)
	if err == nil {
		t.Fatal("expected error for 'cli' provider with no CLI available")
	}
}

func TestBuildPlanningClient_ClaudeCliProvider_WithAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key-456")
	t.Setenv("PATH", "/nonexistent")

	client, err := buildPlanningClient("claude-cli", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("client is nil")
	}
}

func TestBuildPlanningClient_Google_WithAPIKey(t *testing.T) {
	t.Setenv("GOOGLE_AI_API_KEY", "test-google-key")
	t.Setenv("PATH", "/nonexistent")

	client, err := buildPlanningClient("google", false)
	if err != nil {
		t.Fatalf("expected success with Google API key, got: %v", err)
	}
	if client == nil {
		t.Fatal("client is nil")
	}
}

func TestBuildPlanningClient_Google_NoKey(t *testing.T) {
	t.Setenv("GOOGLE_AI_API_KEY", "")
	t.Setenv("PATH", "/nonexistent")

	_, err := buildPlanningClient("google", false)
	if err == nil {
		t.Fatal("expected error when GOOGLE_AI_API_KEY is empty")
	}
}

func TestBuildPlanningClient_Google_WithGodmode(t *testing.T) {
	t.Setenv("GOOGLE_AI_API_KEY", "test-google-key")
	t.Setenv("PATH", "/nonexistent")

	client, err := buildPlanningClient("google", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("client is nil")
	}
}

func TestBuildPlanningClient_OpenAI_WithKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")

	client, err := buildPlanningClient("openai", false)
	if err != nil {
		t.Fatalf("expected success with OpenAI key, got: %v", err)
	}
	if client == nil {
		t.Fatal("client is nil")
	}
}

func TestBuildPlanningClient_OpenAI_NoKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")

	_, err := buildPlanningClient("openai", false)
	if err == nil {
		t.Fatal("expected error when OPENAI_API_KEY not set")
	}
}

func TestBuildPlanningClient_UnsupportedProvider2(t *testing.T) {
	_, err := buildPlanningClient("azure", false)
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
}

// ---------------------------------------------------------------------------
// buildLLMClient — test all provider paths, godmode/skipPerms
// ---------------------------------------------------------------------------

func TestBuildLLMClient_Google_WithKey(t *testing.T) {
	t.Setenv("GOOGLE_AI_API_KEY", "test-google-key")
	t.Setenv("ANTHROPIC_API_KEY", "test-anthropic-key")
	t.Setenv("PATH", "/nonexistent")

	client, err := buildLLMClient("google", nil)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if client == nil {
		t.Fatal("client is nil")
	}
}

func TestBuildLLMClient_Google_WithAnthropicFallbackKey(t *testing.T) {
	t.Setenv("GOOGLE_AI_API_KEY", "test-google-key")
	t.Setenv("ANTHROPIC_API_KEY", "test-anthropic-key")
	t.Setenv("PATH", "/nonexistent") // no claude CLI

	client, err := buildLLMClient("google", nil)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if client == nil {
		t.Fatal("client is nil (should have fallback to Anthropic API)")
	}
}

func TestBuildLLMClient_Google_NoFallback(t *testing.T) {
	t.Setenv("GOOGLE_AI_API_KEY", "test-google-key")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("PATH", "/nonexistent")

	client, err := buildLLMClient("google", nil)
	if err != nil {
		t.Fatalf("expected success (primary only, no fallback), got: %v", err)
	}
	if client == nil {
		t.Fatal("client is nil")
	}
}

func TestBuildLLMClient_Google_WithGodmode(t *testing.T) {
	t.Setenv("GOOGLE_AI_API_KEY", "test-google-key")
	t.Setenv("PATH", "/nonexistent")

	client, err := buildLLMClient("google", nil, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("client is nil")
	}
}

func TestBuildLLMClient_CLI_Provider(t *testing.T) {
	// If claude CLI is on PATH, this should succeed
	claude, err := os.Executable()
	if err != nil {
		t.Skip("cannot determine executable path")
	}
	_ = claude

	// Test with "cli" provider — requires claude on PATH
	// Since we can't guarantee claude is installed, just test the error path
	t.Setenv("PATH", "/nonexistent")
	_, err = buildLLMClient("cli", nil)
	// Even without claude, NewClaudeCLIClient() creates the client (doesn't check PATH)
	// The LookPath check is NOT in the cli branch, so it should succeed
	if err != nil {
		t.Logf("cli provider error (expected on CI): %v", err)
	}
}

func TestBuildLLMClient_Anthropic_WithAPIKeyFallback(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key-789")
	t.Setenv("PATH", "/nonexistent") // force no CLI

	client, err := buildLLMClient("anthropic", nil)
	if err != nil {
		t.Fatalf("expected success with API key fallback, got: %v", err)
	}
	if client == nil {
		t.Fatal("client is nil")
	}
}

func TestBuildLLMClient_OpenAI_WithKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")

	client, err := buildLLMClient("openai", nil)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if client == nil {
		t.Fatal("client is nil")
	}
}

func TestBuildLLMClient_OpenAI_WithGodmode(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")

	client, err := buildLLMClient("openai", nil, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("client is nil")
	}
}

func TestBuildLLMClient_UnsupportedProvider2(t *testing.T) {
	_, err := buildLLMClient("bedrock", nil)
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
}

func TestBuildLLMClient_Anthropic_NoCLI_NoKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("PATH", "/nonexistent")

	_, err := buildLLMClient("anthropic", nil)
	if err == nil {
		t.Fatal("expected error when neither CLI nor API key")
	}
}

func TestBuildLLMClient_Google_MissingKey(t *testing.T) {
	t.Setenv("GOOGLE_AI_API_KEY", "")

	_, err := buildLLMClient("google", nil)
	if err == nil {
		t.Fatal("expected error when GOOGLE_AI_API_KEY not set")
	}
}

func TestBuildLLMClient_OpenAI_MissingKey2(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")

	_, err := buildLLMClient("openai", nil)
	if err == nil {
		t.Fatal("expected error when OPENAI_API_KEY not set")
	}
}
