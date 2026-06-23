package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// DefaultCodexModel is the model Codex uses on a ChatGPT/Codex subscription.
// Bare API IDs like "gpt-5" / "gpt-5-codex" are rejected on a ChatGPT account
// ("not supported when using Codex with a ChatGPT account") — gpt-5.5 is the
// model the subscription serves.
const DefaultCodexModel = "gpt-5.5"

// CodexCLIClient implements the Client interface by invoking the OpenAI Codex
// CLI (`codex exec`) non-interactively. This routes completions through the
// user's ChatGPT/Codex subscription rather than per-token API credits — the
// GPT analogue of ClaudeCLIClient.
type CodexCLIClient struct {
	cliPath string // path to codex binary, default "codex"
}

// NewCodexCLIClient creates a client that invokes the Codex CLI. The codex
// binary must be on $PATH.
func NewCodexCLIClient() *CodexCLIClient { return &CodexCLIClient{cliPath: "codex"} }

// NewCodexCLIClientWithPath creates a client that invokes the Codex CLI at the
// specified path (used in tests with a fake codex script).
func NewCodexCLIClientWithPath(cliPath string) *CodexCLIClient {
	return &CodexCLIClient{cliPath: cliPath}
}

// Complete builds a prompt from the request and invokes
// `codex exec -m <model> --skip-git-repo-check -s read-only --color never
// --ignore-user-config -o <file> -` with the prompt piped via stdin. The clean
// final agent message is read from the -o file rather than parsed out of the
// noisy event stream on stdout.
func (c *CodexCLIClient) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	prompt := buildCLIPrompt(req)

	outFile, err := os.CreateTemp("", "vxd-codex-*.txt")
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("codex: create temp output file: %w", err)
	}
	outPath := outFile.Name()
	_ = outFile.Close()
	defer func() { _ = os.Remove(outPath) }() // best-effort cleanup of the session-scoped output file

	model := req.Model
	if model == "" {
		model = DefaultCodexModel
	}

	// read-only sandbox: the reviewer/QA/manager roles only need text out and
	// must never mutate the workspace. --ignore-user-config skips the operator's
	// ~/.codex/config.toml (MCP servers, hooks, skills) which add noise and can
	// fail. --skip-git-repo-check allows running outside a git repo.
	args := []string{
		"exec",
		"-m", model,
		"--skip-git-repo-check",
		"-s", "read-only",
		"--color", "never",
		"--ignore-user-config",
		"-o", outPath,
		"-", // read the prompt from stdin
	}

	cmd := exec.CommandContext(ctx, c.cliPath, args...)
	cmd.Stdin = strings.NewReader(prompt)
	// Strip OPENAI_API_KEY so Codex uses the subscription, not API credits.
	cmd.Env = FilterCodexEnv(os.Environ())
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	// Codex exits 0 even when the model request fails (e.g. an unsupported
	// model 400s), printing an ERROR JSON line to the event stream. Success is
	// therefore determined by the -o file having content, not by exit code.
	last := ""
	if b, readErr := os.ReadFile(outPath); readErr == nil {
		last = strings.TrimSpace(string(b))
	}

	if last == "" {
		combined := stdout.String() + "\n" + stderr.String()
		if msg := extractCodexError(combined); msg != "" {
			return CompletionResponse{}, classifyCLIError(fmt.Errorf("codex exec failed"), []byte(msg))
		}
		if runErr != nil {
			return CompletionResponse{}, classifyCLIError(runErr, []byte(combined))
		}
		return CompletionResponse{}, fmt.Errorf("codex CLI produced no output")
	}

	return CompletionResponse{
		Content: trimCodeFences(last),
		Model:   model,
	}, nil
}

// extractCodexError scans Codex's event output for an error envelope of the
// form {"type":"error",...,"error":{"message":"..."}} and returns the message.
func extractCodexError(output string) string {
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "ERROR:")
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") || !strings.Contains(line, "\"error\"") {
			continue
		}
		var env struct {
			Type  string `json:"type"`
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(line), &env) == nil && env.Error.Message != "" {
			return env.Error.Message
		}
	}
	return ""
}

// CodexWithFallback tries the Codex CLI (GPT via subscription) first and, on any
// error, falls back to a secondary client (Claude CLI on Opus 4.7). The fallback
// model is fixed because the secondary provider uses different model IDs than
// Codex — so a failed gpt-5.5 call is retried with the Anthropic model rather
// than being re-sent with an ID Anthropic does not recognize.
type CodexWithFallback struct {
	primary       Client
	fallback      Client
	fallbackModel string
}

// NewCodexWithFallback wires a Codex primary to a fallback client + model.
func NewCodexWithFallback(primary, fallback Client, fallbackModel string) *CodexWithFallback {
	return &CodexWithFallback{primary: primary, fallback: fallback, fallbackModel: fallbackModel}
}

// Complete calls the primary; on error it retries on the fallback client with
// the fallback model substituted.
func (c *CodexWithFallback) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	resp, err := c.primary.Complete(ctx, req)
	if err == nil {
		return resp, nil
	}
	if c.fallback == nil {
		return CompletionResponse{}, err
	}
	fbReq := req
	fbReq.Model = c.fallbackModel
	return c.fallback.Complete(ctx, fbReq)
}
