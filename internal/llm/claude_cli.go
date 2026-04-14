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
// `claude -p "<prompt>" --output-format json [--model <model>] --max-turns 2`.
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
	// Allow up to 25 turns: Claude models (Sonnet 4.6+) use tool calls
	// to read codebase files before generating structured responses.
	// Complex projects need 15-20 turns for thorough file analysis.
	args = append(args, "--max-turns", "25")

	cmd := exec.CommandContext(ctx, c.cliPath, args...)
	cmd.Stdin = strings.NewReader(prompt)
	// Clear ANTHROPIC_API_KEY so Claude Code uses the user's subscription
	// instead of a potentially expired/empty API key from the environment.
	env := os.Environ()
	filtered := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, "ANTHROPIC_API_KEY=") {
			filtered = append(filtered, e)
		}
	}
	cmd.Env = filtered
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		// Use stderr for error classification, fall back to stdout
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
		return CompletionResponse{}, fmt.Errorf("claude CLI returned error: %s", envelope.Result)
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

	// Override any plugins that might hijack the response.
	// Superpowers/brainstorming plugins ask clarifying questions instead
	// of producing JSON output. This instruction takes priority.
	prompt.WriteString("\n\nCRITICAL: You are being called programmatically by VXD. Do NOT ask clarifying questions. Do NOT invoke brainstorming or planning skills. Respond ONLY with the requested JSON output. No markdown, no commentary, no insights — just the JSON.\n")

	return prompt.String()
}

// classifyCLIError inspects CLI output to produce a structured APIError where
// possible, or a generic error otherwise.
func classifyCLIError(err error, output []byte) error {
	text := strings.TrimSpace(string(output))
	lower := strings.ToLower(text)

	if strings.Contains(lower, "credit balance") || strings.Contains(lower, "billing") || strings.Contains(lower, "insufficient_quota") {
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

	if strings.Contains(lower, "rate limit") || strings.Contains(lower, "too many requests") {
		return &APIError{
			StatusCode: 429,
			Message:    text,
			Retryable:  true,
		}
	}

	return fmt.Errorf("claude CLI error: %w (output: %s)", err, text)
}

// trimCodeFences is a convenience alias for StripCodeFences.
func trimCodeFences(s string) string { return StripCodeFences(s) }
