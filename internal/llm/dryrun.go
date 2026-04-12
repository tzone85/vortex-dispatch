package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// DryRunClient simulates LLM responses for testing the full VXD pipeline
// without live API calls. It inspects the system prompt to determine what
// role is calling and returns an appropriate canned response.
type DryRunClient struct {
	mu    sync.Mutex
	calls []CompletionRequest
	delay time.Duration // simulated latency
}

// NewDryRunClient creates a client that generates realistic canned responses.
// The optional delay parameter simulates API latency.
func NewDryRunClient(delay time.Duration) *DryRunClient {
	return &DryRunClient{delay: delay}
}

// Complete inspects the request to determine the caller role and returns a
// plausible canned response. It records every call for later inspection.
func (d *DryRunClient) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	d.mu.Lock()
	d.calls = append(d.calls, req)
	d.mu.Unlock()

	if d.delay > 0 {
		select {
		case <-time.After(d.delay):
		case <-ctx.Done():
			return CompletionResponse{}, ctx.Err()
		}
	}

	content := d.generateResponse(req)
	return CompletionResponse{
		Content: content,
		Model:   req.Model,
	}, nil
}

// CallCount returns the number of calls made to Complete.
func (d *DryRunClient) CallCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.calls)
}

// generateResponse inspects the system prompt to determine the caller role
// and returns an appropriate canned response.
func (d *DryRunClient) generateResponse(req CompletionRequest) string {
	system := strings.ToLower(req.System)
	user := ""
	if len(req.Messages) > 0 {
		user = req.Messages[0].Content
	}

	// Tech Lead planning -- return story decomposition
	if strings.Contains(system, "tech lead") || strings.Contains(system, "decompose") {
		return d.planningResponse(user)
	}

	// Code review / QA
	if strings.Contains(system, "review") || strings.Contains(system, "qa agent") {
		return d.reviewResponse()
	}

	// Manager diagnosis
	if strings.Contains(system, "manager") || strings.Contains(system, "diagnos") {
		return d.managerResponse()
	}

	// Supervisor check
	if strings.Contains(system, "supervisor") {
		return d.supervisorResponse()
	}

	// Deep analysis (repolearn)
	if strings.Contains(system, "architect") && strings.Contains(system, "analyse") {
		return d.deepAnalysisResponse()
	}

	// Default -- echo back a summary
	return fmt.Sprintf("[DRY RUN] Simulated response for prompt (%d chars)", len(user))
}

// planningResponse returns a valid JSON array of story objects that the
// planner can parse. It extracts a short title from the requirement text.
func (d *DryRunClient) planningResponse(requirement string) string {
	title := requirement
	if len(title) > 60 {
		title = title[:60]
	}

	stories := []map[string]any{
		{
			"id":                  "s-001",
			"title":               "Project scaffold and directory structure",
			"description":         "Create the directory structure with placeholder files for the feature.",
			"acceptance_criteria": "- Directory structure exists\n- go build ./... succeeds",
			"complexity":          1,
			"depends_on":          []string{},
			"owned_files":         []string{"internal/api/router.go"},
			"wave_hint":           "sequential",
		},
		{
			"id":                  "s-002",
			"title":               "Implement core logic",
			"description":         fmt.Sprintf("Implement the main business logic for: %s", title),
			"acceptance_criteria": "- Core functions implemented\n- Unit tests pass",
			"complexity":          3,
			"depends_on":          []string{"s-001"},
			"owned_files":         []string{"internal/api/handler.go", "internal/api/handler_test.go"},
			"wave_hint":           "parallel",
		},
		{
			"id":                  "s-003",
			"title":               "Add integration and wiring",
			"description":         "Wire the components together and add integration tests.",
			"acceptance_criteria": "- Integration tests pass\n- go test ./... passes",
			"complexity":          2,
			"depends_on":          []string{"s-002"},
			"owned_files":         []string{"internal/api/integration_test.go"},
			"wave_hint":           "sequential",
		},
	}

	data, _ := json.MarshalIndent(stories, "", "  ")
	return string(data)
}

// reviewResponse returns an APPROVED verdict that the reviewer checks for.
func (d *DryRunClient) reviewResponse() string {
	return `APPROVED

The implementation meets the acceptance criteria. Code quality is good.
- Functions are well-named and focused
- Error handling is present
- Tests cover the main paths`
}

// managerResponse returns a diagnosis for tier-2 escalation.
func (d *DryRunClient) managerResponse() string {
	return `DIAGNOSIS: The story failed due to a missing dependency.
RECOMMENDATION: Add the missing import and retry.
REWRITE: false`
}

// supervisorResponse returns a progress assessment.
func (d *DryRunClient) supervisorResponse() string {
	return `ASSESSMENT: Stories are progressing well. No drift detected.
REPRIORITIZE: false`
}

// deepAnalysisResponse returns a canned repo analysis for Pass 3.
func (d *DryRunClient) deepAnalysisResponse() string {
	return `1. PROJECT PURPOSE: A test project for validating VXD pipeline execution.
2. ARCHITECTURE: Simple HTTP server with layered package structure (cmd, internal).
3. KEY PATTERNS: Standard Go project layout, in-memory store, handler-based routing.
4. GOTCHAS: No tests exist yet -- agents should add comprehensive test coverage.`
}
