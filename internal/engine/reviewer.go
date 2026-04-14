package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/tzone85/vortex-dispatch/internal/llm"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// ReviewComment represents a single code review comment.
type ReviewComment struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Severity string `json:"severity"` // "critical", "major", "minor", "info"
	Comment  string `json:"comment"`
}

// ReviewResult holds the outcome of a code review.
type ReviewResult struct {
	Passed   bool            `json:"passed"`
	Comments []ReviewComment `json:"comments"`
	Summary  string          `json:"summary"`
}

// Reviewer performs AI-powered code review on story branch diffs using the
// Senior model.
type Reviewer struct {
	llmClient  llm.Client
	eventStore state.EventStore
	projStore  state.ProjectionStore
	model      string
	maxTokens  int
}

// NewReviewer creates a Reviewer wired to the given LLM client, model
// configuration, event store, and projection store.
func NewReviewer(client llm.Client, model string, maxTokens int, es state.EventStore, ps state.ProjectionStore) *Reviewer {
	return &Reviewer{
		llmClient:  client,
		eventStore: es,
		projStore:  ps,
		model:      model,
		maxTokens:  maxTokens,
	}
}

// Review takes a story ID, title, acceptance criteria, and the git diff of
// the branch changes. It calls the Senior LLM for code review and emits
// either STORY_REVIEW_PASSED or STORY_REVIEW_FAILED.
// worktreePath is optional — if provided, the reviewer gets the full file
// tree to avoid hallucinating about missing files.
func (r *Reviewer) Review(ctx context.Context, storyID, title, acceptanceCriteria, diff string, extra ...string) (ReviewResult, error) {
	if diff == "" {
		return ReviewResult{}, fmt.Errorf("empty diff for story %s", storyID)
	}

	// Build optional file tree context.
	fileTreeCtx := ""
	if len(extra) > 0 && extra[0] != "" {
		fileTreeCtx = fmt.Sprintf("\nExisting file tree (files already on disk, not just the diff):\n%s\n", extra[0])
	}

	// Build optional blast-radius context from codegraph analysis.
	blastRadiusCtx := ""
	if len(extra) > 1 && extra[1] != "" {
		blastRadiusCtx = "\n" + extra[1]
	}

	prompt := fmt.Sprintf(`Review this code change for the following story:

Story: %s
Acceptance Criteria: %s
%s%s
Diff:
%s

IMPORTANT REVIEW GUIDELINES:
- Only judge the diff contents. Files that exist in the file tree but are NOT in the diff were already present before this change.
- Do NOT reject because files appear "missing" if they are listed in the file tree above.
- Focus on whether the NEW code in the diff is correct, well-structured, and meets the acceptance criteria.
- Pass the review if the code makes reasonable progress toward the acceptance criteria, even if not 100%% perfect.

Review the code for:
1. Correctness - does the diff content meet the acceptance criteria?
2. Code quality - clean, readable, well-structured?
3. Test coverage - are changes tested?
4. Security - any vulnerabilities?
5. Performance - any obvious issues?
6. Blast radius - if blast radius analysis is provided above, check whether high-risk callers or dependents might break.

Respond with JSON:
{
  "passed": true/false,
  "comments": [{"file": "path", "line": 0, "severity": "critical|major|minor|info", "comment": "..."}],
  "summary": "brief summary"
}`, title, acceptanceCriteria, fileTreeCtx, blastRadiusCtx, diff)

	resp, err := r.llmClient.Complete(ctx, llm.CompletionRequest{
		Model:     r.model,
		MaxTokens: r.maxTokens,
		System:    "You are a Senior code reviewer for an AI-orchestrated development pipeline. Review code changes and provide structured feedback. Be pragmatic — pass code that makes solid progress even if minor issues exist. Only reject for critical functional failures. Respond only with JSON.",
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: prompt}},
	})
	if err != nil {
		return ReviewResult{}, fmt.Errorf("reviewer LLM call: %w", err)
	}

	var result ReviewResult
	cleaned := extractJSON(resp.Content)
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return ReviewResult{}, fmt.Errorf("parse review response: %w", err)
	}

	// Emit appropriate event
	eventType := state.EventStoryReviewPassed
	if !result.Passed {
		eventType = state.EventStoryReviewFailed
	}

	evt := state.NewEvent(eventType, "reviewer", storyID, map[string]any{
		"passed":        result.Passed,
		"comment_count": len(result.Comments),
		"summary":       result.Summary,
	})
	if err := r.eventStore.Append(evt); err != nil {
		return result, fmt.Errorf("emit review event: %w", err)
	}
	if err := r.projStore.Project(evt); err != nil {
		return result, fmt.Errorf("project review event: %w", err)
	}

	return result, nil
}
