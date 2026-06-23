package cli

import (
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/llm"
)

// fakeReviewerClient is a stand-in senior client to prove it's returned when no
// dedicated reviewer provider is configured.
type fakeReviewerClient struct{ llm.Client }

func TestResolveReviewerClient_DedicatedCodexProvider(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Models.Senior = config.ModelConfig{Provider: "anthropic", Model: "claude-opus-4-7", MaxTokens: 8000}
	cfg.Models.Reviewer = config.ModelConfig{Provider: "codex", Model: "gpt-5.5", MaxTokens: 8000}

	senior := &fakeReviewerClient{}
	client, mc := resolveReviewerClient(cfg, senior, false, false)

	if _, ok := client.(*llm.CodexWithFallback); !ok {
		t.Fatalf("expected codex reviewer client, got %T", client)
	}
	if mc.Model != "gpt-5.5" {
		t.Fatalf("expected reviewer model gpt-5.5, got %q", mc.Model)
	}
}

func TestResolveReviewerClient_FallsBackToSeniorWhenUnset(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Models.Senior = config.ModelConfig{Provider: "anthropic", Model: "claude-opus-4-7", MaxTokens: 8000}
	// Reviewer left empty.

	senior := &fakeReviewerClient{}
	client, mc := resolveReviewerClient(cfg, senior, false, false)

	if client != senior {
		t.Fatalf("expected senior client reused when reviewer unset, got %T", client)
	}
	if mc.Model != "claude-opus-4-7" {
		t.Fatalf("expected senior model, got %q", mc.Model)
	}
}

func TestResolveReviewerClient_DryRunReusesSenior(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Models.Senior = config.ModelConfig{Provider: "anthropic", Model: "claude-opus-4-7", MaxTokens: 8000}
	cfg.Models.Reviewer = config.ModelConfig{Provider: "codex", Model: "gpt-5.5", MaxTokens: 8000}

	senior := &fakeReviewerClient{}
	client, mc := resolveReviewerClient(cfg, senior, false, true) // dryRun=true

	if client != senior {
		t.Fatalf("dry-run must reuse senior client, got %T", client)
	}
	if mc.Model != "claude-opus-4-7" {
		t.Fatalf("dry-run should use senior model, got %q", mc.Model)
	}
}
