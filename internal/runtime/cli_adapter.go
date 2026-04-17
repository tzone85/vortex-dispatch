package runtime

import (
	"fmt"
	"os"
	"path/filepath"
)

// CLIAdapter implements Adapter for CLI-based agent runtimes.
// It translates a SessionConfig into a PreparedExecution without performing
// any I/O — all file writes and process spawning are deferred to the Runner.
type CLIAdapter struct {
	name    string
	command string
	args    []string
	models  []string
}

// NewCLIAdapter creates an adapter for a CLI-based agent runtime.
func NewCLIAdapter(name, command string, args, models []string) *CLIAdapter {
	return &CLIAdapter{
		name:    name,
		command: command,
		args:    args,
		models:  models,
	}
}

// Name returns the adapter's identifier.
func (a *CLIAdapter) Name() string { return a.name }

// SupportedModels returns models this adapter can handle.
func (a *CLIAdapter) SupportedModels() []string { return a.models }

// Prepare builds the full command string and environment without executing.
// This mirrors the logic in CLIRuntime.BuildCommand but returns a
// PreparedExecution instead of performing I/O directly.
func (a *CLIAdapter) Prepare(cfg SessionConfig) (PreparedExecution, error) {
	cmdStr := a.command
	for _, arg := range a.args {
		if err := ValidateShellArg(arg); err != nil {
			return PreparedExecution{}, fmt.Errorf("invalid runtime arg: %w", err)
		}
		cmdStr += " " + QuoteShellArg(arg)
	}
	if cfg.Model != "" {
		if err := ValidateModelName(cfg.Model); err != nil {
			return PreparedExecution{}, fmt.Errorf("invalid model name: %w", err)
		}
		cmdStr += fmt.Sprintf(" --model %q", cfg.Model)
	}

	// Combine system prompt and goal into a single prompt string.
	prompt := cfg.Goal
	if cfg.SystemPrompt != "" {
		prompt = cfg.SystemPrompt + "\n\n---\n\n" + cfg.Goal
	}

	setupFiles := make(map[string]string)

	// Write prompt to a file and pass via -p flag with piped stdin.
	// Claude Code in -p mode with subscription (no ANTHROPIC_API_KEY)
	// runs multi-turn agentic tasks with tool use.
	if prompt != "" && cfg.WorkDir != "" {
		promptDir := filepath.Join(cfg.WorkDir, ".vxd-prompts")
		promptFile := filepath.Join(promptDir, "prompt.txt")
		setupFiles[promptFile] = prompt
		// Use -p with stdin pipe for agentic mode.
		// Log output to file via shell redirection (not tee, which breaks stdin).
		if cfg.LogFile != "" {
			cmdStr = fmt.Sprintf("cat %q | %s -p --output-format json > %q 2>&1", promptFile, cmdStr, cfg.LogFile)
		} else {
			cmdStr = fmt.Sprintf("cat %q | %s -p --output-format json", promptFile, cmdStr)
		}
	} else {
		// No prompt — just log output if requested.
		if cfg.LogFile != "" {
			cmdStr += fmt.Sprintf(" > %q 2>&1", cfg.LogFile)
		}
	}

	// Build env map: pass through PATH (for php, composer, npm, etc.),
	// non-Anthropic API keys, and session-specific vars.
	env := make(map[string]string)
	// Propagate PATH so agents can find php, composer, npm, node, etc.
	if path := os.Getenv("PATH"); path != "" {
		env["PATH"] = path
	}
	for _, key := range []string{"OPENAI_API_KEY", "GOOGLE_API_KEY", "GEMINI_API_KEY", "HOME"} {
		if val := os.Getenv(key); val != "" {
			env[key] = val
		}
	}
	for key, val := range cfg.EnvVars {
		env[key] = val
	}

	// Prepend env exports. Unset ANTHROPIC_API_KEY so Claude Code uses
	// the user's subscription (free) instead of exhausted API credits.
	// Unset CLAUDECODE to prevent nested-session errors.
	var envExports string
	for key, val := range env {
		envExports += fmt.Sprintf("export %s=%q; ", key, val)
	}
	cmdStr = envExports + "unset ANTHROPIC_API_KEY CLAUDECODE; " + cmdStr

	// Add CLAUDE.md to setup files so agents don't brainstorm/plan.
	if cfg.WorkDir != "" {
		claudeMDPath := filepath.Join(cfg.WorkDir, "CLAUDE.md")
		setupFiles[claudeMDPath] = claudeMDContent
	}

	return PreparedExecution{
		Command:     cmdStr,
		WorkDir:     cfg.WorkDir,
		Env:         env,
		SessionName: cfg.SessionName,
		LogFile:     cfg.LogFile,
		SetupFiles:  setupFiles,
	}, nil
}
