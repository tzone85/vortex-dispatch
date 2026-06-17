package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
		// Use POSIX single-quote escaping via QuoteShellArg for
		// consistency with every other argument site in this file.
		// %q would leave `$` and backticks active under `sh -c` if the
		// ValidateModelName allowlist ever widened to include them.
		cmdStr += " --model " + QuoteShellArg(cfg.Model)
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
		// Use QuoteShellArg (POSIX single-quote) for the paths, matching every
		// other arg site here and in registry.go. %q produces double-quotes,
		// which still allow $(...), backticks, and $VAR expansion under sh -c —
		// a latent injection if a path source (WorkDir/LogFile) ever carries
		// shell metacharacters.
		if cfg.LogFile != "" {
			cmdStr = fmt.Sprintf("cat %s | %s -p --output-format json > %s 2>&1", QuoteShellArg(promptFile), cmdStr, QuoteShellArg(cfg.LogFile))
		} else {
			cmdStr = fmt.Sprintf("cat %s | %s -p --output-format json", QuoteShellArg(promptFile), cmdStr)
		}
	} else {
		// No prompt — just log output if requested.
		if cfg.LogFile != "" {
			cmdStr += fmt.Sprintf(" > %s 2>&1", QuoteShellArg(cfg.LogFile))
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
	cmdStr = BuildEnvExports(env) + "unset ANTHROPIC_API_KEY CLAUDECODE; " + cmdStr

	// Dual-write CLAUDE.md + AGENTS.md so Claude Code, Codex, and
	// Gemini CLI all see the no-brainstorm directive. Each agent CLI
	// has its own discovery rule; covering both keeps the directive
	// effective across the runtime fleet.
	if cfg.WorkDir != "" {
		for _, name := range agentDirectiveFiles {
			setupFiles[filepath.Join(cfg.WorkDir, name)] = agentDirectiveContent
		}

		// Ensure VXD artifacts are gitignored BEFORE the agent starts.
		// Without this, the agent's own git commits include prompt files
		// and the directive files we just wrote.
		giPath := filepath.Join(cfg.WorkDir, ".gitignore")
		existing, _ := os.ReadFile(giPath)
		content := string(existing)
		vxdPatterns := []string{"CLAUDE.md", "AGENTS.md", ".vxd-prompts/", ".serena/", "firebase-debug.log"}
		var toAdd []string
		for _, pat := range vxdPatterns {
			if !strings.Contains(content, pat) {
				toAdd = append(toAdd, pat)
			}
		}
		if len(toAdd) > 0 {
			appendix := "\n# VXD agent artifacts (auto-added)\n" + strings.Join(toAdd, "\n") + "\n"
			setupFiles[giPath] = content + appendix
		}
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
