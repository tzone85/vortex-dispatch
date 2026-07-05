package cli

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"
	"github.com/tzone85/vortex-dispatch/internal/agent"
	"github.com/tzone85/vortex-dispatch/internal/engine"
	"github.com/tzone85/vortex-dispatch/internal/figma"
	"github.com/tzone85/vortex-dispatch/internal/llm"
	"github.com/tzone85/vortex-dispatch/internal/repolearn"
)

func newReqCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "req [requirement]",
		Short: "Submit a new requirement for autonomous implementation",
		Long: `Decomposes a requirement into stories via the Tech Lead LLM and dispatches agents.

When review_mode is "auto" (the default), planning is followed immediately by
agent dispatch — one command, unattended. Use --no-dispatch or set
review_mode to "manual" or "plan_only" to stop after planning.

The requirement text can be provided as:
  - A positional argument:  vxd req "Add a health check endpoint"
  - A file (--file/-f):     vxd req --file requirements.md
  - Stdin:                  cat spec.md | vxd req --file -

Product & marketing made easy:
  vxd req "Launch marketing site: hero + 4 features + pricing table + footer for Vortex"
  vxd req --file marketing-brief.md   # turn brief into site, emails, or analytics dashboard`,

		Args: cobra.MaximumNArgs(1),
		RunE: runReq,
	}
	cmd.Flags().StringP("file", "f", "", "read requirement from a file (use - for stdin)")
	cmd.Flags().Bool("godmode", false, "skip per-tool permission prompts during agent execution (does NOT bypass review_mode plan gate or auto_merge PR gate — use review_mode=auto and auto_merge=true for fully unattended operation)")
	cmd.Flags().Bool("dry-run", false, "Simulate LLM responses for pipeline testing (no API calls)")
	cmd.Flags().Bool("no-dispatch", false, "stop after planning; do not auto-dispatch agents (plan-only mode)")
	cmd.Flags().Bool("background", false, "self-daemonize after planning: fork a detached child process and exit; tail logs with 'vxd logs <req-id>'")
	cmd.Flags().Bool("no-dashboard", false, "skip the always-on dashboard auto-spawn for this run (overrides dashboard.auto_start config)")
	cmd.SilenceUsage = true
	return cmd
}

// forkReqDaemon forks a detached child process that runs `vxd resume <reqID>`.
// The child is placed in its own process group (Setsid) so that macOS
// app-nap and parent-shell teardown cannot kill it.
//
// stdout+stderr of the child are redirected to logPath.
// The function returns the child PID (or -1 on error).
//
// This is a pure construction function — it does NOT exec. Tests can call it
// without side effects by inspecting the returned Cmd instead of running it.
func forkReqDaemon(self, reqID, logPath string, extraArgs []string) *exec.Cmd {
	// Build the child argv: vxd resume <reqID> [extraArgs...]
	argv := append([]string{"resume", reqID}, extraArgs...)
	cmd := exec.Command(self, argv...) // nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command -- re-execs vxd's own binary (os.Executable) to self-daemonize

	// Detach from the current process group (platform-specific: Setsid on
	// Unix, CREATE_NEW_PROCESS_GROUP on Windows).
	applyDaemonDetach(cmd)

	// Redirect stdin from /dev/null, stdout+stderr to the log file.
	// The file is opened lazily by exec.Cmd on Start().
	cmd.Stdin = nil
	cmd.Dir = "."

	// We set log file via ExtraFiles + dup trick, but the simpler approach
	// is to open the file in the parent and pass it as stdout/stderr.
	// We do that in the caller (runReq) because we need the project dir.
	return cmd
}

func runReq(cmd *cobra.Command, args []string) error {
	if err := runDispatchPreflight(cmd); err != nil {
		return err
	}

	requirement, err := resolveRequirement(cmd, args)
	if err != nil {
		return err
	}

	s, err := loadStores(cmd)
	if err != nil {
		return err
	}
	defer s.Close()

	// Planning tries API first (handles large prompts), falls back to CLI
	// if API fails (no credits, auth issues, etc.). Dry-run skips the real
	// client entirely so the command stays runnable in CI / sandboxed envs
	// where no LLM is reachable.
	godmode, _ := cmd.Flags().GetBool("godmode")
	if !godmode {
		godmode = s.Config.Planning.Godmode
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	var client llm.Client
	if dryRun {
		client = llm.NewDryRunClient(500 * time.Millisecond)
		fmt.Fprintf(cmd.OutOrStdout(), "[DRY RUN] Using simulated LLM responses\n")
	} else {
		client, err = buildPlanningClient(s.Config.Models.TechLead.Provider, godmode)
		if err != nil {
			return err
		}
	}

	// Generate requirement ID
	reqID := ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String()

	// Determine repo path (current directory)
	repoPath, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("determine working directory: %w", err)
	}

	// Figma design references make this run interactive-ONCE: pulling the
	// design needs an operator credential. Fail fast here — before any LLM
	// spend — with the exact interactive step when it is missing. With a
	// credential in place the run stays fire-and-forget.
	if refs := figma.ParseURLs(requirement); len(refs) > 0 {
		if dryRun {
			fmt.Fprintf(cmd.OutOrStdout(), "[DRY RUN] Figma: %d design reference(s) detected — skipping pull\n", len(refs))
		} else {
			token, source, tokErr := figma.ResolveToken(expandHome(s.Config.Workspace.StateDir))
			if tokErr != nil {
				return tokErr
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Figma: %d design reference(s) detected — pulling design context (auth: %s)\n", len(refs), source)
			dc, pullErr := figma.BuildDesignContext(cmd.Context(), newFigmaClient(token), refs, filepath.Join(repoPath, figma.DirName))
			if pullErr != nil {
				return fmt.Errorf("figma pull: %w", pullErr)
			}
			if dc != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Figma: design context + %d render(s) written to %s/ — the planner and frontend agents will build against them\n", len(dc.Images), figma.DirName)
			}
		}
	}

	planner := engine.NewPlanner(client, s.Config, s.Events, s.Proj)
	planner.SetProjectDir(s.ProjectDir)

	ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Minute)
	defer cancel()

	out := cmd.OutOrStdout()

	// Run Pass 3 (LLM deep analysis) if profile exists but Pass 3 hasn't been done.
	// This is the only place where an LLM client is available during the learn pipeline.
	if s.ProjectDir != "" {
		if profile, err := repolearn.LoadProfile(s.ProjectDir); err == nil && !profile.PassCompleted(3) {
			fmt.Fprintf(out, "Running deep analysis (Pass 3) on repo profile...\n")
			modelCfg := s.Config.Models.Senior
			if deepErr := repolearn.ScanDeep(ctx, profile, client, modelCfg.Model); deepErr != nil {
				log.Printf("[req] Pass 3 deep analysis failed: %v", deepErr)
			} else {
				// ScanDeep returns nil even on LLM failure (stores error as signal).
				// Check if the LLM actually succeeded vs. stored an error signal.
				hasError := false
				for _, sig := range profile.Signals {
					if sig.Kind == "llm_error" {
						hasError = true
						fmt.Fprintf(out, "Deep analysis: LLM unavailable, skipped.\n")
						break
					}
				}
				if !hasError {
					if saveErr := repolearn.SaveProfile(s.ProjectDir, profile); saveErr != nil {
						log.Printf("[req] failed to save updated profile: %v", saveErr)
					} else {
						fmt.Fprintf(out, "Deep analysis complete.\n")
					}
				}
			}
		}
	}

	fmt.Fprintf(out, "Planning requirement: %s\n", requirement)
	fmt.Fprintf(out, "Requirement ID: %s\n", reqID)
	fmt.Fprintf(out, "Planning... (this may take 2-3 minutes for complex requirements)\n\n")

	result, err := planner.Plan(ctx, reqID, requirement, repoPath)
	if err != nil {
		return fmt.Errorf("planning failed: %w", err)
	}

	// Print plan summary
	fmt.Fprintf(out, "Plan created with %d stories:\n\n", len(result.Stories))

	totalComplexity := 0
	for i, story := range result.Stories {
		deps := "none"
		if len(story.DependsOn) > 0 {
			deps = fmt.Sprintf("%v", story.DependsOn)
		}
		fmt.Fprintf(out, "  %d. [%s] %s (complexity: %d, deps: %s)\n",
			i+1, story.ID, story.Title, story.Complexity, deps)
		totalComplexity += story.Complexity
	}

	fmt.Fprintf(out, "\nTotal complexity: %d story points\n", totalComplexity)

	// Auto-spawn the always-on dashboard so the user can watch this
	// requirement land in a browser without typing `vxd status <id>`.
	// Failures here are logged and swallowed — they MUST NOT block dispatch.
	// The seam (ensureDashboardForReq) is package-level for the wiring test
	// to substitute a stub instead of forking the real binary.
	noDashboard, _ := cmd.Flags().GetBool("no-dashboard")
	if !dryRun && !noDashboard && s.Config.Dashboard.AutoStart {
		printDashboardBanner(cmd, s, reqID)
	}

	// Auto-dispatch: when review_mode is "auto" (the default), chain directly
	// into the dispatch loop so `vxd req` is truly one-command autonomous.
	// --no-dispatch or a non-auto review_mode skips this and prints guidance.
	noDispatch, _ := cmd.Flags().GetBool("no-dispatch")
	if noDispatch {
		fmt.Fprintf(out, "Run 'vxd resume %s' to start dispatch.\n", reqID)
		return nil
	}

	reviewGate := engine.NewReviewGate(s.Events)
	effectiveMode := reviewGate.ResolveMode(reqID, s.Config.Merge)
	if effectiveMode != "auto" {
		fmt.Fprintf(out, "review_mode=%s: run 'vxd approve-plan %s' then 'vxd resume %s' to start.\n",
			effectiveMode, reqID, reqID)
		return nil
	}

	// --background: self-daemonize by forking a detached child that runs
	// `vxd resume <reqID>`. The parent prints the PID and log path, then exits 0.
	// This prevents macOS app-nap and parent-shell teardown from killing the run.
	background, _ := cmd.Flags().GetBool("background")
	if background {
		logDir := filepath.Join(s.ProjectDir, "logs")
		if err := os.MkdirAll(logDir, 0o755); err != nil {
			return fmt.Errorf("create log dir: %w", err)
		}
		lp := reqLogPath(s.ProjectDir, reqID)

		self, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolve self path: %w", err)
		}

		// Carry forward --godmode and --dry-run to the child.
		var childExtra []string
		if godmode {
			childExtra = append(childExtra, "--godmode")
		}
		if dryRun {
			childExtra = append(childExtra, "--dry-run")
		}

		child := forkReqDaemon(self, reqID, lp, childExtra)

		// Open log file and attach to child's stdout+stderr.
		lf, err := os.OpenFile(lp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return fmt.Errorf("open log file: %w", err)
		}
		child.Stdout = lf
		child.Stderr = lf

		devNull, err := os.Open(os.DevNull)
		if err != nil {
			lf.Close()
			return fmt.Errorf("open /dev/null: %w", err)
		}
		child.Stdin = devNull

		if err := child.Start(); err != nil {
			lf.Close()
			devNull.Close()
			return fmt.Errorf("fork daemon: %w", err)
		}
		// Close our copies of the file handles; child has its own fd via Start().
		lf.Close()
		devNull.Close()

		fmt.Fprintf(out, "Requirement %s dispatched (daemon pid %d).\n", reqID, child.Process.Pid)
		fmt.Fprintf(out, "Tail logs: vxd logs %s\n", reqID)
		fmt.Fprintf(out, "Log file:  %s\n", lp)
		return nil
	}

	fmt.Fprintf(out, "\nreview_mode=auto — starting dispatch...\n\n")
	return runResume(cmd, []string{reqID})
}

// resolveRequirement reads the requirement text from either the --file flag,
// stdin (when --file is "-"), or the positional argument.
func resolveRequirement(cmd *cobra.Command, args []string) (string, error) {
	filePath, _ := cmd.Flags().GetString("file")

	switch {
	case filePath != "" && len(args) > 0:
		return "", fmt.Errorf("provide either a positional argument or --file, not both")
	case filePath == "-":
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		text := strings.TrimSpace(string(data))
		if text == "" {
			return "", fmt.Errorf("stdin was empty")
		}
		return text, nil
	case filePath != "":
		data, err := os.ReadFile(filePath)
		if err != nil {
			return "", fmt.Errorf("read file %s: %w", filePath, err)
		}
		text := strings.TrimSpace(string(data))
		if text == "" {
			return "", fmt.Errorf("file %s is empty", filePath)
		}
		return text, nil
	case len(args) > 0:
		return args[0], nil
	default:
		return "", fmt.Errorf("provide a requirement as an argument or via --file")
	}
}

// planningFallbackClient wraps an API client and falls back to a CLI client
// when the API fails. If the CLI also fails (e.g. prompt too long), it returns
// a helpful error suggesting the user shorten their requirement.
type planningFallbackClient struct {
	apiClient llm.Client
	cliClient llm.Client
}

func (p *planningFallbackClient) Complete(ctx context.Context, req llm.CompletionRequest) (llm.CompletionResponse, error) {
	// Try Claude CLI first — uses subscription (no per-token cost),
	// runs agentic mode with file reads and tool use.
	if p.cliClient != nil {
		resp, err := p.cliClient.Complete(ctx, req)
		if err == nil && resp.Content != "" {
			return resp, nil
		}
		if err != nil {
			log.Printf("[planning] CLI call failed (%v), falling back to API", err)
		}
	}

	// Fall back to API (per-token, handles large prompts natively).
	if p.apiClient != nil {
		resp, err := p.apiClient.Complete(ctx, req)
		if err == nil {
			return resp, nil
		}
		log.Printf("[planning] API call also failed: %v", err)
	}

	// Both failed — try CLI one more time for detailed error.
	// (Legacy fallback path)
	if p.cliClient != nil {
		resp, err := p.cliClient.Complete(ctx, req)
		if err == nil && resp.Content != "" {
			return resp, nil
		}
		if err != nil {
			log.Printf("[planning] CLI call also failed: %v", err)
			errMsg := err.Error()
			// Distinguish billing errors from prompt-too-long errors
			if strings.Contains(strings.ToLower(errMsg), "credit") ||
				strings.Contains(strings.ToLower(errMsg), "balance") ||
				strings.Contains(strings.ToLower(errMsg), "billing") {
				return llm.CompletionResponse{}, fmt.Errorf(
					"planning failed: %v. Top up API credits or authenticate Claude CLI with an active subscription", err)
			}
			return llm.CompletionResponse{}, fmt.Errorf("planning failed: %w", err)
		}
		if resp.Content == "" {
			log.Printf("[planning] CLI returned empty response")
			return llm.CompletionResponse{}, fmt.Errorf(
				"planning failed: CLI returned empty response. Try writing your requirement to a file and submitting with: vxd req --file requirement.md")
		}
	}

	return llm.CompletionResponse{}, fmt.Errorf("no LLM client available for planning — set ANTHROPIC_API_KEY or install claude CLI")
}

// buildPlanningClient creates a client that tries API first, then falls back
// to CLI. Planning prompts can be large, so the API is preferred.
func buildPlanningClient(provider string, godmode bool) (llm.Client, error) {
	var apiClient llm.Client
	var cliClient llm.Client

	switch provider {
	case "anthropic", "cli", "claude-cli":
		// Try to build API client (may fail if no key).
		if apiKey := resolveAPIKey("ANTHROPIC_API_KEY"); apiKey != "" {
			apiClient = llm.NewRetryClient(llm.NewAnthropicClient(apiKey), 3)
		}
		// Try to build CLI client.
		if _, err := exec.LookPath("claude"); err == nil {
			c := llm.NewClaudeCLIClient()
			if godmode {
				c = c.WithSkipPermissions()
			}
			cliClient = c
		}
	case "openai":
		if apiKey := resolveAPIKey("OPENAI_API_KEY"); apiKey != "" {
			apiClient = llm.NewRetryClient(llm.NewOpenAIClient(apiKey), 3)
		}
	case "codex", "codex-cli", "openai-cli", "gpt-cli":
		// Codex CLI (GPT via subscription) as primary, Claude CLI as the
		// planning fallback if installed.
		cliClient = llm.NewCodexWithFallback(llm.NewCodexCLIClient(), codexFallbackClient(godmode), codexFallbackModel)
	case "google":
		if apiKey := resolveAPIKey("GOOGLE_AI_API_KEY"); apiKey != "" {
			google := llm.NewToolCallAdapter(llm.NewGoogleAIClient(apiKey), llm.ToolSchemaFor(agent.RoleTechLead))
			apiClient = llm.NewRetryClient(google, 2)
		}
		if _, err := exec.LookPath("claude"); err == nil {
			c := llm.NewClaudeCLIClient()
			if godmode {
				c = c.WithSkipPermissions()
			}
			cliClient = c
		}
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s", provider)
	}

	if apiClient == nil && cliClient == nil {
		return nil, fmt.Errorf("no LLM available: set ANTHROPIC_API_KEY or install claude CLI")
	}

	return &planningFallbackClient{apiClient: apiClient, cliClient: cliClient}, nil
}

// buildLLMClient creates an LLM client based on the provider name.
// For the "anthropic" provider, it prefers the Claude Code CLI (which uses
// the user's subscription at no per-token cost) and falls back to direct API
// calls only when the CLI is not installed.
func buildLLMClient(provider string, schema *llm.ToolSchema, godmode ...bool) (llm.Client, error) {
	skipPerms := len(godmode) > 0 && godmode[0]

	switch provider {
	case "google":
		apiKey := resolveAPIKey("GOOGLE_AI_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("GOOGLE_AI_API_KEY environment variable is required")
		}
		google := llm.NewToolCallAdapter(llm.NewGoogleAIClient(apiKey), schema)
		primary := llm.NewRetryClient(google, 2)

		var fallback llm.Client
		if _, err := exec.LookPath("claude"); err == nil {
			c := llm.NewClaudeCLIClient()
			if skipPerms {
				c = c.WithSkipPermissions()
			}
			fallback = c
		} else if ak := resolveAPIKey("ANTHROPIC_API_KEY"); ak != "" {
			fallback = llm.NewRetryClient(llm.NewAnthropicClient(ak), 3)
		}

		if fallback != nil {
			return llm.NewFallbackClient(primary, fallback), nil
		}
		return primary, nil
	case "cli", "claude-cli", "anthropic_cli", "anthropic-cli":
		c := llm.NewClaudeCLIClient()
		if skipPerms {
			c = c.WithSkipPermissions()
		}
		return c, nil
	case "anthropic":
		// Prefer Claude CLI (uses subscription, no API credits).
		if _, err := exec.LookPath("claude"); err == nil {
			c := llm.NewClaudeCLIClient()
			if skipPerms {
				c = c.WithSkipPermissions()
			}
			return c, nil
		}
		// Fall back to direct API if CLI not available.
		apiKey := resolveAPIKey("ANTHROPIC_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("claude CLI not found and ANTHROPIC_API_KEY not set")
		}
		return llm.NewRetryClient(llm.NewAnthropicClient(apiKey), 3), nil
	case "openai":
		apiKey := resolveAPIKey("OPENAI_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("OPENAI_API_KEY environment variable is required")
		}
		return llm.NewRetryClient(llm.NewOpenAIClient(apiKey), 3), nil
	case "codex", "codex-cli", "openai-cli", "gpt-cli":
		// Codex CLI runs GPT (gpt-5.5) through the user's ChatGPT/Codex
		// subscription at no per-token cost. On any Codex failure (CLI missing,
		// rate limit, model error) fall back to the capable Anthropic model so
		// the role keeps working.
		return llm.NewCodexWithFallback(llm.NewCodexCLIClient(), codexFallbackClient(skipPerms), codexFallbackModel), nil
	default:
		return nil, fmt.Errorf("unsupported LLM provider %q (accepted: anthropic, cli, claude-cli, anthropic-cli, anthropic_cli, google, openai, codex)", provider)
	}
}

// codexFallbackModel is the Anthropic model a Codex role falls back to when the
// Codex CLI is unavailable or errors.
const codexFallbackModel = "claude-opus-4-7"

// codexFallbackClient returns the Claude CLI (subscription) when available, else
// the Anthropic API client, else nil (Codex runs with no fallback).
func codexFallbackClient(skipPerms bool) llm.Client {
	if _, err := exec.LookPath("claude"); err == nil {
		c := llm.NewClaudeCLIClient()
		if skipPerms {
			c = c.WithSkipPermissions()
		}
		return c
	}
	if ak := resolveAPIKey("ANTHROPIC_API_KEY"); ak != "" {
		return llm.NewRetryClient(llm.NewAnthropicClient(ak), 3)
	}
	return nil
}
