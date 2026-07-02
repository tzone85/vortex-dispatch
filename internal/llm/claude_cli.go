package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ClaudeCLIClient implements the Client interface by invoking the Claude Code
// CLI tool instead of making direct API calls. This routes LLM completions
// through the user's Claude subscription rather than per-token API credits.
type ClaudeCLIClient struct {
	cliPath    string // path to claude binary, default "claude"
	skipPerms  bool   // if true, adds --dangerously-skip-permissions
}

// NewClaudeCLIClient creates a client that invokes Claude Code CLI for
// completions. The claude binary must be on $PATH or installed at the
// default location. By default, permission prompts are enabled (safe mode).
func NewClaudeCLIClient() *ClaudeCLIClient {
	return &ClaudeCLIClient{cliPath: "claude", skipPerms: false}
}

// NewClaudeCLIClientWithPath creates a client that invokes the Claude Code CLI
// at the specified path.
func NewClaudeCLIClientWithPath(cliPath string) *ClaudeCLIClient {
	return &ClaudeCLIClient{cliPath: cliPath, skipPerms: false}
}

// WithSkipPermissions returns a copy with --dangerously-skip-permissions
// enabled (godmode). Use when you want fully autonomous operation without
// approval prompts.
func (c *ClaudeCLIClient) WithSkipPermissions() *ClaudeCLIClient {
	return &ClaudeCLIClient{cliPath: c.cliPath, skipPerms: true}
}

// Complete builds a prompt from the request and invokes
// `claude -p "<prompt>" --output-format json [--model <model>] --max-turns 10`.
// It captures stdout as the completion content.
func (c *ClaudeCLIClient) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	prompt := buildCLIPrompt(req)

	args := []string{}
	if c.skipPerms {
		args = append(args, "--dangerously-skip-permissions")
	}
	// Pipe prompt via stdin to avoid shell argument length limits.
	args = append(args, "-p", "-", "--output-format", "json")
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	// Allow enough turns for deep tool use (file reads, codebase
	// analysis, dependency inspection) during planning. Legacy
	// framework upgrades (e.g., Laravel 5.5 → 12) can need 30+
	// turns of file reads before producing a plan.
	args = append(args, "--max-turns", "50")

	cmd := exec.CommandContext(ctx, c.cliPath, args...) // nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command -- cliPath is the claude binary from PATH; model name gated by ValidateModelName, prompt via stdin
	cmd.Stdin = strings.NewReader(prompt)
	// Strip ANTHROPIC_API_KEY so Claude Code uses the user's Max subscription
	// instead of a potentially expired/empty API key. Also strip CLAUDECODE to
	// prevent nested-session errors when VXD is invoked inside Claude Code.
	cmd.Env = FilterClaudeEnv(os.Environ())
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		// Claude CLI exits non-zero on max-turns but may still have produced
		// valid output. Check stdout for a parseable JSON result before failing.
		raw := strings.TrimSpace(stdout.String())
		if raw != "" && strings.HasPrefix(raw, "{") {
			var envelope struct {
				Result  string `json:"result"`
				IsError bool   `json:"is_error"`
			}
			if jsonErr := json.Unmarshal([]byte(raw), &envelope); jsonErr == nil && envelope.Result != "" {
				// Check for usage exhaustion in the result text.
				resultLower := strings.ToLower(envelope.Result)
				if strings.Contains(resultLower, "out of extra usage") ||
					strings.Contains(resultLower, "credit balance") {
					return CompletionResponse{}, &APIError{
						StatusCode: 400,
						Message:    envelope.Result,
						Retryable:  false,
					}
				}
				if !envelope.IsError {
					return CompletionResponse{
						Content: trimCodeFences(strings.TrimSpace(envelope.Result)),
						Model:   req.Model,
					}, nil
				}
			}
		}

		errOutput := stderr.String()
		if errOutput == "" {
			errOutput = stdout.String()
		}
		return CompletionResponse{}, classifyCLIError(err, []byte(errOutput))
	}

	// Parse the JSON envelope from --output-format json.
	// The actual LLM response is in the "result" field.
	// Claude Code may write the JSON to either stdout or stderr depending on version.
	raw := strings.TrimSpace(stdout.String())
	stderrStr := strings.TrimSpace(stderr.String())
	if raw == "" {
		raw = stderrStr
	}
	// If stdout had non-JSON content and stderr has the JSON, prefer stderr.
	if !strings.HasPrefix(raw, "{") && strings.HasPrefix(stderrStr, "{") {
		raw = stderrStr
	}

	// Debug: uncomment to trace CLI output
	// log.Printf("[claude-cli] stdout len=%d, stderr len=%d", len(stdout.String()), len(stderrStr))

	var envelope struct {
		Result  string `json:"result"`
		IsError bool   `json:"is_error"`
	}
	if jsonErr := json.Unmarshal([]byte(raw), &envelope); jsonErr != nil {
		// Not JSON — return raw output (fallback for older CLI versions)
		return CompletionResponse{
			Content: trimCodeFences(raw),
			Model:   req.Model,
		}, nil
	}

	if envelope.IsError {
		// The CLI can exit 0 yet carry an error envelope (e.g. a session-limit
		// 429 whose subtype is "success"). Route through classifyCLIError so
		// capacity/billing/auth conditions are typed, not flattened to a plain
		// error that the escalation chain can't recognise.
		return CompletionResponse{}, classifyCLIError(
			fmt.Errorf("claude CLI returned error envelope"), []byte(raw))
	}

	result := trimCodeFences(strings.TrimSpace(envelope.Result))

	return CompletionResponse{
		Content: result,
		Model:   req.Model,
	}, nil
}

// buildCLIPrompt concatenates the system prompt and user messages into a single
// string suitable for the CLI's -p flag.
func buildCLIPrompt(req CompletionRequest) string {
	var prompt strings.Builder

	if req.System != "" {
		prompt.WriteString(req.System)
		prompt.WriteString("\n\n")
	}
	for _, msg := range req.Messages {
		if msg.Role == RoleUser {
			prompt.WriteString(msg.Content)
			prompt.WriteString("\n")
		}
	}

	return prompt.String()
}

// classifyCLIError inspects CLI output to produce a structured APIError where
// possible, or a generic error otherwise.
func classifyCLIError(err error, output []byte) error {
	text := strings.TrimSpace(string(output))
	lower := strings.ToLower(text)

	if strings.Contains(lower, "credit balance") || strings.Contains(lower, "billing") ||
		strings.Contains(lower, "insufficient_quota") || strings.Contains(lower, "out of extra usage") {
		return &APIError{
			StatusCode: 400,
			Message:    text,
			Retryable:  false,
		}
	}

	if strings.Contains(lower, "authentication") || strings.Contains(lower, "unauthorized") {
		return &APIError{
			StatusCode: 401,
			Message:    text,
			Retryable:  false,
		}
	}

	// Capacity exhaustion: rate limit, Max session limit, or overloaded. These
	// are transient — the request succeeds after the limit resets — so they are
	// typed (Retryable) and recognised by IsCapacityError/IsRateLimited rather
	// than left as a plain error that cascades through the escalation chain.
	if ContainsCapacitySignature(text) {
		status := 429
		if strings.Contains(lower, "overloaded") || strings.Contains(text, `"api_error_status":529`) {
			status = 529
		}
		return &APIError{
			StatusCode: status,
			Message:    text,
			Retryable:  true,
		}
	}

	return fmt.Errorf("claude CLI error: %w (output: %s)", err, text)
}

// trimCodeFences is a convenience alias for StripCodeFences.
func trimCodeFences(s string) string { return StripCodeFences(s) }
