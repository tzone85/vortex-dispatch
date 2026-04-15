package config

import "testing"

func TestDefaultConfig_MaxConcurrentAgents(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Routing.MaxConcurrentAgents != 5 {
		t.Errorf("MaxConcurrentAgents default = %d, want 5", cfg.Routing.MaxConcurrentAgents)
	}
}

func TestValidate_MaxConcurrentAgents_TooLow(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Routing.MaxConcurrentAgents = 0
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for MaxConcurrentAgents=0")
	}
}

func TestValidate_MaxConcurrentAgents_TooHigh(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Routing.MaxConcurrentAgents = 100
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for MaxConcurrentAgents=100")
	}
}

func TestValidate_MaxConcurrentAgents_Valid(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Routing.MaxConcurrentAgents = 10
	if err := cfg.Validate(); err != nil {
		t.Errorf("unexpected validation error: %v", err)
	}
}
