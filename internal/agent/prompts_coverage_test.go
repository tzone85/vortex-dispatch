package agent

import (
	"strings"
	"testing"
)

// TestSystemPrompt_ExistingCodebase_TechLead verifies CodebaseArchaeology
// playbook is injected for TechLead on existing codebases.
func TestSystemPrompt_ExistingCodebase_TechLead(t *testing.T) {
	ctx := PromptContext{
		RepoPath:           "/repo",
		TechStack:          "Go",
		IsExistingCodebase: true,
	}
	prompt := SystemPrompt(RoleTechLead, ctx)
	if !strings.Contains(prompt, "Codebase Archaeology") {
		t.Error("TechLead on existing codebase should include CodebaseArchaeology playbook")
	}
}

// TestSystemPrompt_ExistingCodebase_Senior verifies BugHunting + LegacyCode
// playbooks are injected for Senior on existing codebases.
func TestSystemPrompt_ExistingCodebase_Senior(t *testing.T) {
	ctx := PromptContext{
		IsExistingCodebase: true,
	}
	prompt := SystemPrompt(RoleSenior, ctx)
	if !strings.Contains(prompt, "Bug Hunting") {
		t.Error("Senior on existing codebase should include BugHunting playbook")
	}
	if !strings.Contains(prompt, "Legacy Code") {
		t.Error("Senior on existing codebase should include LegacyCode playbook")
	}
}

// TestSystemPrompt_ExistingCodebase_Intermediate verifies LegacyCode playbook
// is injected for Intermediate role on existing codebases.
func TestSystemPrompt_ExistingCodebase_Intermediate(t *testing.T) {
	ctx := PromptContext{
		IsExistingCodebase: true,
	}
	prompt := SystemPrompt(RoleIntermediate, ctx)
	if !strings.Contains(prompt, "Legacy Code") {
		t.Error("Intermediate on existing codebase should include LegacyCode playbook")
	}
}

// TestSystemPrompt_ExistingCodebase_Junior verifies LegacyCode playbook
// is injected for Junior role on existing codebases.
func TestSystemPrompt_ExistingCodebase_Junior(t *testing.T) {
	ctx := PromptContext{
		IsExistingCodebase: true,
	}
	prompt := SystemPrompt(RoleJunior, ctx)
	if !strings.Contains(prompt, "Legacy Code") {
		t.Error("Junior on existing codebase should include LegacyCode playbook")
	}
}

// TestSystemPrompt_BugFix_Senior_NewCodebase verifies BugHunting is injected
// for Senior on bug fixes in a NEW codebase (not existing — avoids duplicate).
func TestSystemPrompt_BugFix_Senior_NewCodebase(t *testing.T) {
	ctx := PromptContext{
		IsBugFix:           true,
		IsExistingCodebase: false,
	}
	prompt := SystemPrompt(RoleSenior, ctx)
	if !strings.Contains(prompt, "Bug Hunting") {
		t.Error("Senior on bug fix (new codebase) should include BugHunting playbook")
	}
}

// TestSystemPrompt_BugFix_Intermediate_NewCodebase verifies BugHunting is
// injected for Intermediate on bug fixes in a NEW codebase.
func TestSystemPrompt_BugFix_Intermediate_NewCodebase(t *testing.T) {
	ctx := PromptContext{
		IsBugFix:           true,
		IsExistingCodebase: false,
	}
	prompt := SystemPrompt(RoleIntermediate, ctx)
	if !strings.Contains(prompt, "Bug Hunting") {
		t.Error("Intermediate on bug fix (new codebase) should include BugHunting playbook")
	}
}

// TestSystemPrompt_BugFix_Senior_ExistingCodebase_NoDuplicate verifies that
// BugHunting is NOT duplicated when IsExistingCodebase is true (already injected).
func TestSystemPrompt_BugFix_Senior_ExistingCodebase_NoDuplicate(t *testing.T) {
	ctx := PromptContext{
		IsBugFix:           true,
		IsExistingCodebase: true,
	}
	prompt := SystemPrompt(RoleSenior, ctx)
	// BugHunting should appear exactly once (from existing codebase path)
	count := strings.Count(prompt, "Bug Hunting")
	if count != 1 {
		t.Errorf("BugHunting should appear exactly once, got %d occurrences", count)
	}
}

// TestSystemPrompt_Infrastructure verifies InfrastructureDebugging playbook
// is injected for all roles when IsInfrastructure is true.
func TestSystemPrompt_Infrastructure(t *testing.T) {
	roles := []Role{RoleTechLead, RoleSenior, RoleIntermediate, RoleJunior, RoleQA, RoleSupervisor}
	for _, role := range roles {
		ctx := PromptContext{IsInfrastructure: true}
		prompt := SystemPrompt(role, ctx)
		if !strings.Contains(prompt, "Infrastructure Debugging") {
			t.Errorf("role %s with IsInfrastructure should include Infrastructure playbook", role)
		}
	}
}

// TestSystemPrompt_NoPlaybooks_Greenfield verifies no playbooks are injected
// for a simple greenfield project context.
func TestSystemPrompt_NoPlaybooks_Greenfield(t *testing.T) {
	ctx := PromptContext{
		RepoPath:  "/repo",
		TechStack: "Go",
	}
	prompt := SystemPrompt(RoleJunior, ctx)
	if strings.Contains(prompt, "Codebase Archaeology") ||
		strings.Contains(prompt, "Bug Hunting") ||
		strings.Contains(prompt, "Legacy Code") ||
		strings.Contains(prompt, "Infrastructure Debugging") {
		t.Error("greenfield context should not include any diagnostic playbooks")
	}
}

// TestSystemPrompt_CombinedFlags verifies multiple flags work together.
func TestSystemPrompt_CombinedFlags(t *testing.T) {
	ctx := PromptContext{
		IsExistingCodebase: true,
		IsInfrastructure:   true,
	}
	prompt := SystemPrompt(RoleSenior, ctx)
	if !strings.Contains(prompt, "Bug Hunting") {
		t.Error("should contain BugHunting for existing codebase")
	}
	if !strings.Contains(prompt, "Infrastructure Debugging") {
		t.Error("should contain Infrastructure playbook")
	}
}

// TestSystemPrompt_Supervisor verifies Supervisor prompt renders correctly.
func TestSystemPrompt_Supervisor(t *testing.T) {
	ctx := PromptContext{}
	prompt := SystemPrompt(RoleSupervisor, ctx)
	if !strings.Contains(prompt, "Supervisor") {
		t.Error("Supervisor prompt should contain 'Supervisor'")
	}
}

// TestGoalPrompt_Basic verifies basic goal prompt generation.
func TestGoalPrompt_Basic(t *testing.T) {
	ctx := PromptContext{
		StoryID:            "s-001",
		StoryTitle:         "Add user auth",
		StoryDescription:   "Create login endpoint",
		AcceptanceCriteria: "- JWT token returned",
	}
	prompt := GoalPrompt(RoleJunior, ctx)
	if !strings.Contains(prompt, "s-001") {
		t.Error("goal prompt should contain story ID")
	}
	if !strings.Contains(prompt, "Add user auth") {
		t.Error("goal prompt should contain story title")
	}
	if !strings.Contains(prompt, "Create login endpoint") {
		t.Error("goal prompt should contain description")
	}
	if !strings.Contains(prompt, "JWT token returned") {
		t.Error("goal prompt should contain acceptance criteria")
	}
	if !strings.Contains(prompt, "IMPORTANT INSTRUCTIONS") {
		t.Error("goal prompt should contain important instructions section")
	}
}

// TestGoalPrompt_WithWaveContext verifies wave context injection.
func TestGoalPrompt_WithWaveContext(t *testing.T) {
	ctx := PromptContext{
		StoryID:            "s-002",
		StoryTitle:         "Add profile page",
		StoryDescription:   "Create profile view",
		AcceptanceCriteria: "- Profile renders",
		WaveContext:        "Story s-001 created the auth module with JWT middleware.",
	}
	prompt := GoalPrompt(RoleJunior, ctx)
	if !strings.Contains(prompt, "What Prior Stories Built") {
		t.Error("should contain wave context header")
	}
	if !strings.Contains(prompt, "auth module with JWT middleware") {
		t.Error("should contain the wave context content")
	}
	if !strings.Contains(prompt, "MUST be compatible") {
		t.Error("should contain compatibility instruction")
	}
}

// TestGoalPrompt_WithoutWaveContext verifies wave context is not added when empty.
func TestGoalPrompt_WithoutWaveContext(t *testing.T) {
	ctx := PromptContext{
		StoryID:            "s-001",
		StoryTitle:         "First story",
		StoryDescription:   "Set up project",
		AcceptanceCriteria: "- Project builds",
	}
	prompt := GoalPrompt(RoleJunior, ctx)
	if strings.Contains(prompt, "What Prior Stories Built") {
		t.Error("should NOT contain wave context header when WaveContext is empty")
	}
}

// TestGoalPrompt_ExistingCodebase verifies the existing codebase workflow
// is injected into the goal prompt.
func TestGoalPrompt_ExistingCodebase(t *testing.T) {
	ctx := PromptContext{
		StoryID:            "s-003",
		StoryTitle:         "Fix bug",
		StoryDescription:   "Fix null pointer",
		AcceptanceCriteria: "- No crash",
		IsExistingCodebase: true,
	}
	prompt := GoalPrompt(RoleSenior, ctx)
	if !strings.Contains(prompt, "EXISTING CODEBASE") {
		t.Error("should include existing codebase workflow")
	}
	if !strings.Contains(prompt, "ORIENT") {
		t.Error("should include ORIENT step")
	}
	if !strings.Contains(prompt, "BASELINE") {
		t.Error("should include BASELINE step")
	}
}

// TestGoalPrompt_BugFix verifies bug fix workflow injection.
func TestGoalPrompt_BugFix(t *testing.T) {
	ctx := PromptContext{
		StoryID:            "s-004",
		StoryTitle:         "Fix login crash",
		StoryDescription:   "Null pointer on login",
		AcceptanceCriteria: "- No crash on login",
		IsBugFix:           true,
	}
	prompt := GoalPrompt(RoleSenior, ctx)
	if !strings.Contains(prompt, "BUG FIX") {
		t.Error("should include bug fix workflow")
	}
	if !strings.Contains(prompt, "REPRODUCE") {
		t.Error("should include REPRODUCE step")
	}
	if !strings.Contains(prompt, "ROOT CAUSE") {
		t.Error("should include ROOT CAUSE step")
	}
}

// TestGoalPrompt_Infrastructure verifies infrastructure workflow injection.
func TestGoalPrompt_Infrastructure(t *testing.T) {
	ctx := PromptContext{
		StoryID:            "s-005",
		StoryTitle:         "Add Docker support",
		StoryDescription:   "Create Dockerfile",
		AcceptanceCriteria: "- Docker image builds",
		IsInfrastructure:   true,
	}
	prompt := GoalPrompt(RoleSenior, ctx)
	if !strings.Contains(prompt, "INFRASTRUCTURE") {
		t.Error("should include infrastructure workflow")
	}
	if !strings.Contains(prompt, "docker ps") {
		t.Error("should include docker ps diagnostic command")
	}
}

// TestGoalPrompt_ReviewFeedback verifies review feedback injection.
func TestGoalPrompt_ReviewFeedback(t *testing.T) {
	ctx := PromptContext{
		StoryID:            "s-006",
		StoryTitle:         "Add auth",
		StoryDescription:   "Login endpoint",
		AcceptanceCriteria: "- Works",
		ReviewFeedback:     "Missing error handling in auth.go:42",
	}
	prompt := GoalPrompt(RoleSenior, ctx)
	if !strings.Contains(prompt, "Previous Review Feedback") {
		t.Error("should include review feedback header")
	}
	if !strings.Contains(prompt, "auth.go:42") {
		t.Error("should include the specific review feedback")
	}
}

// TestGoalPrompt_AllFlagsSet verifies all flags combine correctly.
func TestGoalPrompt_AllFlagsSet(t *testing.T) {
	ctx := PromptContext{
		StoryID:            "s-007",
		StoryTitle:         "Full story",
		StoryDescription:   "Everything",
		AcceptanceCriteria: "- All done",
		IsExistingCodebase: true,
		IsBugFix:           true,
		IsInfrastructure:   true,
		WaveContext:        "Prior story set up the DB schema.",
		ReviewFeedback:     "Add more tests",
	}
	prompt := GoalPrompt(RoleSenior, ctx)
	if !strings.Contains(prompt, "What Prior Stories Built") {
		t.Error("should include wave context")
	}
	if !strings.Contains(prompt, "EXISTING CODEBASE") {
		t.Error("should include existing codebase workflow")
	}
	if !strings.Contains(prompt, "BUG FIX") {
		t.Error("should include bug fix workflow")
	}
	if !strings.Contains(prompt, "INFRASTRUCTURE") {
		t.Error("should include infrastructure workflow")
	}
	if !strings.Contains(prompt, "Previous Review Feedback") {
		t.Error("should include review feedback")
	}
}

// TestReplacePlaceholders verifies all placeholder substitutions.
func TestReplacePlaceholders(t *testing.T) {
	tmpl := "{team_name} {repo_path} {tech_stack} {story_id} {story_title} " +
		"{story_description} {acceptance_criteria} {lint_command} {build_command} {test_command}"
	ctx := PromptContext{
		TeamName:           "alpha",
		RepoPath:           "/code",
		TechStack:          "Python",
		StoryID:            "st-1",
		StoryTitle:         "Title",
		StoryDescription:   "Desc",
		AcceptanceCriteria: "AC",
		LintCommand:        "lint",
		BuildCommand:       "build",
		TestCommand:        "test",
	}
	result := replacePlaceholders(tmpl, ctx)
	for _, expected := range []string{"alpha", "/code", "Python", "st-1", "Title", "Desc", "AC", "lint", "build", "test"} {
		if !strings.Contains(result, expected) {
			t.Errorf("expected %q in result, got: %s", expected, result)
		}
	}
	// No unreplaced placeholders
	if strings.Contains(result, "{") {
		t.Errorf("result still contains unreplaced placeholders: %s", result)
	}
}
