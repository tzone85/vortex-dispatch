package llm

import "github.com/tzone85/vortex-dispatch/internal/agent"

// ToolSchema defines a function-calling tool that can be injected into
// Gemma model prompts. The Parameters field uses JSON Schema format.
type ToolSchema struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Parameters  SchemaObject `json:"parameters"`
}

// SchemaObject represents a JSON Schema object type.
type SchemaObject struct {
	Type       string                    `json:"type"`
	Properties map[string]SchemaProperty `json:"properties"`
	Required   []string                  `json:"required,omitempty"`
}

// SchemaProperty represents a single property in a JSON Schema object.
type SchemaProperty struct {
	Type        string                    `json:"type"`
	Description string                    `json:"description,omitempty"`
	Items       *SchemaProperty           `json:"items,omitempty"`
	Properties  map[string]SchemaProperty `json:"properties,omitempty"`
	Enum        []string                  `json:"enum,omitempty"`
}

// ToolSchemaFor returns the tool schema for the given role, or nil if the
// role does not make direct LLM calls (e.g., Junior/Intermediate/Senior use
// CLI runtimes, QA uses local commands).
func ToolSchemaFor(role agent.Role) *ToolSchema {
	switch role {
	case agent.RoleTechLead:
		return techLeadSchema()
	case agent.RoleSupervisor:
		return supervisorSchema()
	case agent.RoleManager:
		return managerSchema()
	default:
		return nil
	}
}

func techLeadSchema() *ToolSchema {
	return &ToolSchema{
		Name:        "create_stories",
		Description: "Decompose a requirement into implementable stories with dependency ordering",
		Parameters: SchemaObject{
			Type: "object",
			Properties: map[string]SchemaProperty{
				"stories": {
					Type:        "array",
					Description: "Array of decomposed stories",
					Items: &SchemaProperty{
						Type: "object",
						Properties: map[string]SchemaProperty{
							"id":                 {Type: "string", Description: "Short identifier (e.g., s-001)"},
							"title":              {Type: "string", Description: "Brief title"},
							"description":        {Type: "string", Description: "What to implement, including exact file paths"},
							"acceptance_criteria": {Type: "string", Description: "3-6 discrete, human-readable criteria, one per line starting with '- '. Each line is a single plain-language verifiable outcome (intent first, exact command in parentheses at the end). A human reading only this must understand what the story delivers."},
							"complexity":          {Type: "integer", Description: "Fibonacci score (1, 2, 3, 5, 8, 13)"},
							"depends_on":          {Type: "array", Description: "Story IDs this depends on", Items: &SchemaProperty{Type: "string"}},
							"owned_files":         {Type: "array", Description: "Exact file paths this story creates or modifies", Items: &SchemaProperty{Type: "string"}},
							"wave_hint":           {Type: "string", Description: "sequential or parallel", Enum: []string{"sequential", "parallel"}},
						},
					},
				},
			},
			Required: []string{"stories"},
		},
	}
}

func supervisorSchema() *ToolSchema {
	return &ToolSchema{
		Name:        "report_status",
		Description: "Report on whether stories are on track to fulfill the requirement",
		Parameters: SchemaObject{
			Type: "object",
			Properties: map[string]SchemaProperty{
				"on_track":     {Type: "boolean", Description: "Whether stories are on track"},
				"concerns":     {Type: "array", Description: "List of concerns", Items: &SchemaProperty{Type: "string"}},
				"reprioritize": {Type: "array", Description: "Story IDs to reprioritize", Items: &SchemaProperty{Type: "string"}},
			},
			Required: []string{"on_track", "concerns", "reprioritize"},
		},
	}
}

func managerSchema() *ToolSchema {
	return &ToolSchema{
		Name:        "diagnose_failure",
		Description: "Diagnose why a story failed and choose a corrective action",
		Parameters: SchemaObject{
			Type: "object",
			Properties: map[string]SchemaProperty{
				"diagnosis": {Type: "string", Description: "Human-readable explanation of failure"},
				"category":  {Type: "string", Description: "Failure category", Enum: []string{"environment", "structural", "complexity", "transient", "unknown"}},
				"action":    {Type: "string", Description: "Corrective action", Enum: []string{"retry", "rewrite", "split", "escalate_to_techlead"}},
				"retry_config": {
					Type:        "object",
					Description: "Config for retry action",
					Properties: map[string]SchemaProperty{
						"target_role":    {Type: "string"},
						"reset_tier":     {Type: "integer"},
						"worktree_reset": {Type: "boolean"},
						"env_fixes":      {Type: "array", Items: &SchemaProperty{Type: "string"}},
					},
				},
				"rewrite_config": {
					Type:        "object",
					Description: "Config for rewrite action",
					Properties: map[string]SchemaProperty{
						"title":              {Type: "string"},
						"description":        {Type: "string"},
						"acceptance_criteria": {Type: "string"},
						"complexity":          {Type: "integer"},
						"owned_files":         {Type: "array", Items: &SchemaProperty{Type: "string"}},
					},
				},
				"split_config": {
					Type:        "object",
					Description: "Config for split action",
					Properties: map[string]SchemaProperty{
						"children":         {Type: "array", Description: "Child story definitions"},
						"dependency_edges": {Type: "array", Description: "Dependency pairs"},
					},
				},
			},
			Required: []string{"diagnosis", "category", "action"},
		},
	}
}
