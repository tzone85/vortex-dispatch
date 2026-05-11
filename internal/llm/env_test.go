package llm_test

import (
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/llm"
)

func TestFilterClaudeEnv_StripsAnthropicAPIKey(t *testing.T) {
	env := []string{
		"ANTHROPIC_API_KEY=sk-ant-dangerous-placeholder",
		"HOME=/root",
		"PATH=/usr/bin",
	}
	filtered := llm.FilterClaudeEnv(env)
	for _, e := range filtered {
		if strings.HasPrefix(e, "ANTHROPIC_API_KEY=") {
			t.Errorf("ANTHROPIC_API_KEY must be removed, found: %s", e)
		}
	}
	// Unrelated vars must be preserved.
	if !containsPrefix(filtered, "HOME=") {
		t.Error("HOME should be preserved")
	}
	if !containsPrefix(filtered, "PATH=") {
		t.Error("PATH should be preserved")
	}
}

func TestFilterClaudeEnv_StripsClaudeCode(t *testing.T) {
	env := []string{
		"CLAUDECODE=1",
		"HOME=/root",
		"PATH=/usr/bin",
	}
	filtered := llm.FilterClaudeEnv(env)
	for _, e := range filtered {
		if strings.HasPrefix(e, "CLAUDECODE=") {
			t.Errorf("CLAUDECODE must be removed, found: %s", e)
		}
	}
	if !containsPrefix(filtered, "HOME=") {
		t.Error("HOME should be preserved")
	}
}

func TestFilterClaudeEnv_StripsBothKeys(t *testing.T) {
	env := []string{
		"ANTHROPIC_API_KEY=sk-ant-dangerous-placeholder",
		"CLAUDECODE=1",
		"GOOGLE_API_KEY=google-placeholder",
		"HOME=/root",
	}
	filtered := llm.FilterClaudeEnv(env)
	for _, e := range filtered {
		if strings.HasPrefix(e, "ANTHROPIC_API_KEY=") {
			t.Errorf("ANTHROPIC_API_KEY must be removed: %s", e)
		}
		if strings.HasPrefix(e, "CLAUDECODE=") {
			t.Errorf("CLAUDECODE must be removed: %s", e)
		}
	}
	// GOOGLE_API_KEY and HOME must survive.
	if !containsPrefix(filtered, "GOOGLE_API_KEY=") {
		t.Error("GOOGLE_API_KEY should be preserved")
	}
	if !containsPrefix(filtered, "HOME=") {
		t.Error("HOME should be preserved")
	}
}

func TestFilterClaudeEnv_EmptyEnv(t *testing.T) {
	filtered := llm.FilterClaudeEnv(nil)
	if len(filtered) != 0 {
		t.Errorf("expected empty result for nil input, got %d entries", len(filtered))
	}
}

func TestFilterClaudeEnv_NoTargetKeys(t *testing.T) {
	env := []string{"HOME=/root", "PATH=/usr/bin"}
	filtered := llm.FilterClaudeEnv(env)
	if len(filtered) != len(env) {
		t.Errorf("expected %d entries, got %d", len(env), len(filtered))
	}
}

// TestFilterClaudeEnv_DoesNotMutateInput verifies immutability — the original
// slice is not modified.
func TestFilterClaudeEnv_DoesNotMutateInput(t *testing.T) {
	env := []string{
		"ANTHROPIC_API_KEY=sk-ant-placeholder",
		"CLAUDECODE=1",
		"HOME=/root",
	}
	original := make([]string, len(env))
	copy(original, env)

	llm.FilterClaudeEnv(env)

	for i, e := range env {
		if e != original[i] {
			t.Errorf("input slice mutated at index %d: %q != %q", i, e, original[i])
		}
	}
}

func containsPrefix(ss []string, prefix string) bool {
	for _, s := range ss {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}
