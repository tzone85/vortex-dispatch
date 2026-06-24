package config

import "testing"

// TestDefaultConfig_NoInvalidJuniorModel pins the fix for a dead junior tier:
// the default junior/intermediate/supervisor model was "gemma-4-27b-it", which
// does not exist on the Google AI API (404 ModelNotFound). Every low-complexity
// story therefore spawned an agent that died in seconds producing no code, and
// only limped forward by escalating to the senior tier after wasted attempts.
//
// The default must be a real, agentic model on the primary (Claude CLI
// subscription) path so vxd works out-of-the-box without a Google AI key.
func TestDefaultConfig_NoInvalidJuniorModel(t *testing.T) {
	cfg := DefaultConfig()
	m := cfg.Models

	roles := map[string]ModelConfig{
		"tech_lead":    m.TechLead,
		"senior":       m.Senior,
		"intermediate": m.Intermediate,
		"junior":       m.Junior,
		"qa":           m.QA,
		"supervisor":   m.Supervisor,
		"manager":      m.Manager,
	}

	for role, mc := range roles {
		if mc.Model == "gemma-4-27b-it" {
			t.Errorf("role %q still defaults to the invalid model gemma-4-27b-it (404 on Google AI)", role)
		}
		if mc.Model == "" {
			t.Errorf("role %q has an empty default model", role)
		}
		if mc.Provider == "" {
			t.Errorf("role %q has an empty default provider", role)
		}
	}

	// The execution tiers must default to the Claude CLI subscription path so a
	// fresh install needs only `claude` configured (no Google AI key/quota).
	for role, mc := range map[string]ModelConfig{
		"junior":       m.Junior,
		"intermediate": m.Intermediate,
		"supervisor":   m.Supervisor,
	} {
		if mc.Provider != "anthropic" {
			t.Errorf("role %q should default to provider anthropic, got %q", role, mc.Provider)
		}
		if mc.Model != "claude-haiku-4-5" {
			t.Errorf("role %q should default to claude-haiku-4-5, got %q", role, mc.Model)
		}
	}
}
