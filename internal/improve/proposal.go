package improve

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/llm"
)

// ProposalDrafter generates proposal drafts via Claude CLI.
type ProposalDrafter struct {
	claudePath string
	workDir    string
}

// NewProposalDrafter creates a proposal drafter with the Claude CLI path and work directory.
func NewProposalDrafter(claudePath, workDir string) *ProposalDrafter {
	return &ProposalDrafter{
		claudePath: claudePath,
		workDir:    workDir,
	}
}

// BuildProposalPrompt constructs the Claude prompt for proposal generation.
func BuildProposalPrompt(opp Opportunity) string {
	budgetGuidance := ""
	if opp.Budget != "" {
		budgetGuidance = fmt.Sprintf("Client's stated budget: %s. ", opp.Budget)
	}

	return fmt.Sprintf(`You are writing a freelance proposal for a client posting. This is a DRAFT that a human will review before sending.

**Opportunity:**
- Title: %s
- Company: %s
- Budget: %s
- Skills: %s
- Description: %s

**Proposal Structure (follow this exactly):**
1. Understanding - Restate the client's problem in their language
2. Approach - Tech stack, phases, timeline, what makes us different
3. Relevant Experience - Reference real software engineering experience, AI-augmented development capability
4. Timeline & Budget - %sPosition at 75th percentile, compete on quality not price. Minimum $50/hr equivalent.
5. Next Steps - Clear call to action, availability

**Tone Instructions:**
Write like a real human. Use short sentences. Use contractions. Be direct. No buzzwords. No corporate fluff. Sound like someone who has done this before — confident but not arrogant. Like explaining to a smart colleague over coffee.

Good example opening: "Hey — I read through your brief and I think I can help."
Bad example (NEVER do this): "Dear Sir/Madam, I am writing to express my interest in your esteemed project."

**IMPORTANT:**
- Mark the top of the proposal with: [DRAFT — Review before sending]
- Do NOT include any personal data beyond professional capability
- Keep the total proposal under 400 words
- End with a specific, actionable next step`,
		opp.Title, opp.Company, opp.Budget,
		strings.Join(opp.Skills, ", "), opp.Notes,
		budgetGuidance)
}

// EstimateMinBudget returns a budget range string based on hourly rate and effort.
func EstimateMinBudget(minHourlyRate int, effort string) string {
	rate := minHourlyRate
	switch effort {
	case "S":
		// 3-8 hours
		return fmt.Sprintf("$%d-$%d", rate*3, rate*8)
	case "M":
		// 8-50 hours (1-6 days)
		return fmt.Sprintf("$%d-$%d", rate*8, rate*50)
	case "L":
		// 50-240 hours (6-30 days)
		return fmt.Sprintf("$%d-$%d", rate*50, rate*240)
	default:
		return fmt.Sprintf("$%d+/hr", rate)
	}
}

// DraftProposal writes the prompt to a file, invokes Claude CLI, and returns the proposal text.
func (d *ProposalDrafter) DraftProposal(ctx context.Context, opp Opportunity) (string, error) {
	prompt := BuildProposalPrompt(opp)

	// Write prompt to file for audit trail
	if err := os.MkdirAll(d.workDir, 0o755); err != nil {
		return "", fmt.Errorf("create work dir: %w", err)
	}
	promptFile := filepath.Join(d.workDir, fmt.Sprintf("proposal-%s.md", opp.ID))
	if err := os.WriteFile(promptFile, []byte(prompt), 0o600); err != nil {
		return "", fmt.Errorf("write prompt file: %w", err)
	}

	// Call Claude CLI. Strip ANTHROPIC_API_KEY so Claude uses Max subscription
	// instead of API credits, and strip CLAUDECODE to prevent nested-session
	// errors when VXD is invoked inside Claude Code (ENV-2).
	cmd := exec.CommandContext(ctx, d.claudePath, "-p", prompt, "--output-format", "text") // nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command -- claudePath resolved from PATH; prompt is a single argv element, never shell-interpolated
	cmd.Dir = d.workDir
	cmd.Env = llm.FilterClaudeEnv(os.Environ())

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	// Claude CLI may return non-zero exit on "max turns" but still produce output.
	draft := strings.TrimSpace(stdout.String())
	if draft == "" && err != nil {
		return "", fmt.Errorf("claude CLI: %w (stderr: %s)", err, stderr.String())
	}
	if draft == "" {
		return "", fmt.Errorf("claude CLI produced empty output (stderr: %s)", stderr.String())
	}

	// Validate draft content
	draft = sanitizeProposal(draft)

	log.Printf("  [proposal] Drafted proposal for %q (%d chars)", opp.Title, len(draft))
	return draft, nil
}

// maxProposalLen caps generated proposals to prevent LLM bloat.
const maxProposalLen = 3000

// sanitizeProposal validates and cleans LLM-generated proposal content.
// Caps length, flags injection, and notes if output appears truncated.
func sanitizeProposal(draft string) string {
	// Cap length
	if len(draft) > maxProposalLen {
		draft = draft[:maxProposalLen] + "\n\n[Proposal truncated at 3000 chars — review and edit before sending]"
	}

	// Flag (but don't strip) injection patterns — human review is the gate
	if DetectPromptInjection(draft) {
		draft = "[WARNING: This proposal may contain suspicious patterns. Review carefully before sending.]\n\n" + draft
	}

	return draft
}

// DraftProposalsForTop drafts proposals for the top N opportunities by rank.
// Returns the opportunities with proposal_draft and proposal_drafted_at populated.
func (d *ProposalDrafter) DraftProposalsForTop(ctx context.Context, opps []Opportunity, maxProposals int) []Opportunity {
	sorted := SortByRank(opps)
	limit := maxProposals
	if limit > len(sorted) {
		limit = len(sorted)
	}

	var results []Opportunity
	for i := 0; i < limit; i++ {
		opp := sorted[i]
		log.Printf("  [%d/%d] Drafting proposal for %q (rank %d) ...", i+1, limit, opp.Title, opp.Rank)

		draft, err := d.DraftProposal(ctx, opp)
		if err != nil {
			log.Printf("  [%d/%d] Proposal FAILED for %q: %v", i+1, limit, opp.Title, err)
			continue
		}

		now := time.Now()
		opp.ProposalDraft = draft
		opp.ProposalDraftedAt = &now
		opp.Status = StatusProposalDrafted
		results = append(results, opp)
	}

	return results
}
