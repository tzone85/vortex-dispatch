package improve_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/improve"
)

// TestDraftProposal_StripsClaudeCode verifies that CLAUDECODE is removed from
// the child process environment when DraftProposal spawns the claude CLI,
// preventing nested-session errors (ENV-2 fix).
func TestDraftProposal_StripsClaudeCode(t *testing.T) {
	// Write a fake claude script that prints only the security-sensitive vars
	// to stdout, so we can assert they are absent regardless of output length.
	// We filter to just the two keys we care about to avoid sanitizeProposal
	// truncating the output before those lines appear.
	dir := t.TempDir()
	fakeScript := `#!/bin/sh
# Print only CLAUDECODE and ANTHROPIC_API_KEY lines from the environment.
# If neither appears, print a safe marker so DraftProposal doesn't return
# an empty-output error.
/usr/bin/env | grep -E '^(CLAUDECODE|ANTHROPIC_API_KEY)=' || echo "SENTINEL_NO_SENSITIVE_KEYS_FOUND"
`
	scriptPath := filepath.Join(dir, "fake-claude")
	if err := os.WriteFile(scriptPath, []byte(fakeScript), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}

	t.Setenv("CLAUDECODE", "1")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-placeholder-for-test")

	drafter := improve.NewProposalDrafter(scriptPath, dir)
	opp := improve.Opportunity{
		ID:      "opp-env-test-001",
		Title:   "Test ENV strip",
		Company: "TestCo",
	}

	result, err := drafter.DraftProposal(context.Background(), opp)
	if err != nil {
		t.Fatalf("DraftProposal: %v", err)
	}

	// The fake script outputs only the two sensitive keys (if present) or the
	// sentinel. After the fix, neither key should appear in the child env.
	if strings.Contains(result, "CLAUDECODE=") {
		t.Errorf("CLAUDECODE must be stripped from child env, found in output: %q", result)
	}
	if strings.Contains(result, "ANTHROPIC_API_KEY=") {
		t.Errorf("ANTHROPIC_API_KEY must be stripped from child env, found in output: %q", result)
	}
}

func TestBuildProposalPrompt_IncludesOpportunityData(t *testing.T) {
	opp := improve.Opportunity{
		ID:      "opp-2026-04-09-001",
		Title:   "Build REST API for fintech startup",
		Company: "Acme Corp",
		Budget:  "$5000-$10000",
		Skills:  []string{"Go", "PostgreSQL", "REST"},
		Notes:   "Need a payment webhook handler and Postgres sync",
	}

	prompt := improve.BuildProposalPrompt(opp)
	if !strings.Contains(prompt, "Build REST API") {
		t.Error("prompt should include opportunity title")
	}
	if !strings.Contains(prompt, "Acme Corp") {
		t.Error("prompt should include company name")
	}
	if !strings.Contains(prompt, "$5000-$10000") {
		t.Error("prompt should include budget")
	}
	if !strings.Contains(prompt, "Go") {
		t.Error("prompt should include skills")
	}
	if !strings.Contains(prompt, "short sentences") || !strings.Contains(prompt, "contractions") {
		t.Error("prompt should include humanized tone instructions")
	}
	if !strings.Contains(prompt, "DRAFT") {
		t.Error("prompt should mention DRAFT status")
	}
}

func TestBuildProposalPrompt_IncludesStructure(t *testing.T) {
	opp := improve.Opportunity{Title: "Test Job", Company: "TestCo"}
	prompt := improve.BuildProposalPrompt(opp)

	requiredSections := []string{"Understanding", "Approach", "Relevant Experience", "Timeline", "Next Steps"}
	for _, section := range requiredSections {
		if !strings.Contains(prompt, section) {
			t.Errorf("prompt should include section %q", section)
		}
	}
}

func TestProposalDrafter_DraftProposal_WritesPromptFile(t *testing.T) {
	dir := t.TempDir()
	drafter := improve.NewProposalDrafter("echo", dir) // echo as mock Claude

	opp := improve.Opportunity{
		ID:      "opp-2026-04-09-001",
		Title:   "Build REST API",
		Company: "Acme Corp",
		Budget:  "$5000",
		Skills:  []string{"Go"},
	}

	result, err := drafter.DraftProposal(context.Background(), opp)
	if err != nil {
		t.Fatalf("draft: %v", err)
	}
	// echo will output whatever arguments we pass, so result won't be empty
	if result == "" {
		t.Error("expected non-empty proposal draft")
	}

	// Verify prompt file was written
	promptFile := filepath.Join(dir, "proposal-opp-2026-04-09-001.md")
	if _, err := os.Stat(promptFile); os.IsNotExist(err) {
		t.Error("expected prompt file to be written")
	}
}

func TestMinHourlyFloor(t *testing.T) {
	if improve.EstimateMinBudget(50, "S") != "$150-$400" {
		t.Errorf("unexpected S budget: %s", improve.EstimateMinBudget(50, "S"))
	}
	if improve.EstimateMinBudget(50, "M") != "$400-$2500" {
		t.Errorf("unexpected M budget: %s", improve.EstimateMinBudget(50, "M"))
	}
	if improve.EstimateMinBudget(50, "L") != "$2500-$12000" {
		t.Errorf("unexpected L budget: %s", improve.EstimateMinBudget(50, "L"))
	}
}

func TestDraftProposalsForTop_RespectsMaxLimit(t *testing.T) {
	dir := t.TempDir()
	drafter := improve.NewProposalDrafter("echo", dir)

	opps := []improve.Opportunity{
		{ID: "opp-1", Title: "Job 1", Rank: 47},
		{ID: "opp-2", Title: "Job 2", Rank: 35},
		{ID: "opp-3", Title: "Job 3", Rank: 20},
		{ID: "opp-4", Title: "Job 4", Rank: 15},
	}

	results := drafter.DraftProposalsForTop(context.Background(), opps, 2)
	if len(results) != 2 {
		t.Errorf("expected 2 proposals (max), got %d", len(results))
	}
}

func TestDraftProposalsForTop_SetsTimestamp(t *testing.T) {
	dir := t.TempDir()
	drafter := improve.NewProposalDrafter("echo", dir)

	opps := []improve.Opportunity{
		{ID: "opp-1", Title: "Job 1", Rank: 47},
	}

	before := time.Now()
	results := drafter.DraftProposalsForTop(context.Background(), opps, 3)
	after := time.Now()

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ProposalDraftedAt == nil {
		t.Fatal("expected non-nil ProposalDraftedAt")
	}
	if results[0].ProposalDraftedAt.Before(before) || results[0].ProposalDraftedAt.After(after) {
		t.Error("ProposalDraftedAt should be between before and after")
	}
}
