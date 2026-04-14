package agent

import (
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/config"
)

// TestModelConfig_AllRoles verifies ModelConfig returns the correct config
// for every defined role, including the default fallback.
func TestModelConfig_AllRoles(t *testing.T) {
	models := config.ModelsConfig{
		TechLead:     config.ModelConfig{Provider: "anthropic", Model: "opus"},
		Senior:       config.ModelConfig{Provider: "anthropic", Model: "sonnet"},
		Intermediate: config.ModelConfig{Provider: "google", Model: "gemma"},
		Junior:       config.ModelConfig{Provider: "google", Model: "mini"},
		QA:           config.ModelConfig{Provider: "anthropic", Model: "sonnet-qa"},
		Supervisor:   config.ModelConfig{Provider: "anthropic", Model: "sonnet-sv"},
		Manager:      config.ModelConfig{Provider: "anthropic", Model: "sonnet-mgr"},
	}

	tests := []struct {
		role          Role
		expectedModel string
	}{
		{RoleTechLead, "opus"},
		{RoleSenior, "sonnet"},
		{RoleIntermediate, "gemma"},
		{RoleJunior, "mini"},
		{RoleQA, "sonnet-qa"},
		{RoleSupervisor, "sonnet-sv"},
		{RoleManager, "sonnet-mgr"},
	}

	for _, tt := range tests {
		mc := tt.role.ModelConfig(models)
		if mc.Model != tt.expectedModel {
			t.Errorf("role %s: expected model %s, got %s", tt.role, tt.expectedModel, mc.Model)
		}
	}
}

// TestModelConfig_UnknownRole verifies unknown roles fall back to Junior config.
func TestModelConfig_UnknownRole(t *testing.T) {
	models := config.ModelsConfig{
		Junior: config.ModelConfig{Model: "fallback-model"},
	}
	unknownRole := Role("unknown_role")
	mc := unknownRole.ModelConfig(models)
	if mc.Model != "fallback-model" {
		t.Errorf("unknown role should fall back to Junior config, got model %s", mc.Model)
	}
}

// TestExecutionMode_UnknownRole verifies unknown roles default to ExecAPI.
func TestExecutionMode_UnknownRole(t *testing.T) {
	unknownRole := Role("unknown_role")
	mode := unknownRole.ExecutionMode()
	if mode != ExecAPI {
		t.Errorf("unknown role should default to ExecAPI, got %s", mode)
	}
}

// TestRouteByComplexity_Boundaries tests exact boundary values.
func TestRouteByComplexity_Boundaries(t *testing.T) {
	routing := config.RoutingConfig{
		JuniorMaxComplexity:       3,
		IntermediateMaxComplexity: 5,
	}

	tests := []struct {
		complexity int
		expected   Role
	}{
		{0, RoleJunior},       // below junior
		{3, RoleJunior},       // exactly junior max
		{4, RoleIntermediate}, // above junior, within intermediate
		{5, RoleIntermediate}, // exactly intermediate max
		{6, RoleSenior},       // above intermediate
		{100, RoleSenior},     // way above
	}

	for _, tt := range tests {
		role := RouteByComplexity(tt.complexity, routing)
		if role != tt.expected {
			t.Errorf("complexity %d: expected %s, got %s", tt.complexity, tt.expected, role)
		}
	}
}

// TestRouteByComplexity_ZeroThresholds tests routing with zero thresholds.
func TestRouteByComplexity_ZeroThresholds(t *testing.T) {
	routing := config.RoutingConfig{
		JuniorMaxComplexity:       0,
		IntermediateMaxComplexity: 0,
	}
	// Complexity 0 == JuniorMax, so junior
	role := RouteByComplexity(0, routing)
	if role != RoleJunior {
		t.Errorf("complexity 0 with zero thresholds: expected Junior, got %s", role)
	}
	// Complexity 1 > IntermediateMax(0), so senior
	role = RouteByComplexity(1, routing)
	if role != RoleSenior {
		t.Errorf("complexity 1 with zero thresholds: expected Senior, got %s", role)
	}
}
