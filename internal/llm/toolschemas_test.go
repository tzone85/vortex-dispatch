package llm_test

import (
	"encoding/json"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/agent"
	"github.com/tzone85/vortex-dispatch/internal/llm"
)

func TestToolSchemaFor_ReturnsSchemaForLLMRoles(t *testing.T) {
	roles := []agent.Role{
		agent.RoleTechLead,
		agent.RoleSupervisor,
		agent.RoleManager,
	}
	for _, role := range roles {
		t.Run(string(role), func(t *testing.T) {
			schema := llm.ToolSchemaFor(role)
			if schema == nil {
				t.Fatalf("expected non-nil schema for role %s", role)
			}
			if schema.Name == "" {
				t.Error("schema name is empty")
			}
		})
	}
}

func TestToolSchemaFor_ReturnsNilForCLIRoles(t *testing.T) {
	roles := []agent.Role{
		agent.RoleJunior,
		agent.RoleIntermediate,
		agent.RoleSenior,
		agent.RoleQA,
	}
	for _, role := range roles {
		t.Run(string(role), func(t *testing.T) {
			schema := llm.ToolSchemaFor(role)
			if schema != nil {
				t.Errorf("expected nil schema for CLI role %s, got %+v", role, schema)
			}
		})
	}
}

func TestToolSchemaFor_SchemasAreValidJSON(t *testing.T) {
	roles := []agent.Role{
		agent.RoleTechLead,
		agent.RoleSupervisor,
		agent.RoleManager,
	}
	for _, role := range roles {
		t.Run(string(role), func(t *testing.T) {
			schema := llm.ToolSchemaFor(role)
			if schema == nil {
				t.Skip("no schema")
			}
			data, err := json.Marshal(schema)
			if err != nil {
				t.Fatalf("schema is not JSON-serializable: %v", err)
			}
			if len(data) == 0 {
				t.Error("serialized schema is empty")
			}
		})
	}
}

func TestToolSchemaFor_TechLeadHasCreateStories(t *testing.T) {
	schema := llm.ToolSchemaFor(agent.RoleTechLead)
	if schema == nil {
		t.Fatal("expected non-nil schema for TechLead")
	}
	if schema.Name != "create_stories" {
		t.Errorf("expected name 'create_stories', got %q", schema.Name)
	}
}

func TestToolSchemaFor_SupervisorHasReportStatus(t *testing.T) {
	schema := llm.ToolSchemaFor(agent.RoleSupervisor)
	if schema == nil {
		t.Fatal("expected non-nil schema for Supervisor")
	}
	if schema.Name != "report_status" {
		t.Errorf("expected name 'report_status', got %q", schema.Name)
	}
}

func TestToolSchemaFor_ManagerHasDiagnoseFailure(t *testing.T) {
	schema := llm.ToolSchemaFor(agent.RoleManager)
	if schema == nil {
		t.Fatal("expected non-nil schema for Manager")
	}
	if schema.Name != "diagnose_failure" {
		t.Errorf("expected name 'diagnose_failure', got %q", schema.Name)
	}
}
