package cli

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"
	"github.com/tzone85/vortex-dispatch/internal/agent"
	"github.com/tzone85/vortex-dispatch/internal/engine"
	"github.com/tzone85/vortex-dispatch/internal/llm"
	"github.com/tzone85/vortex-dispatch/internal/repolearn"
)

func newReqCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "req [requirement]",
		Short: "Submit a new requirement for planning",
		Long: `Decomposes a requirement into stories via the Tech Lead LLM and prints the plan.

The requirement text can be provided as:
  - A positional argument:  vxd req "Add a health check endpoint"
  - A file (--file/-f):     vxd req --file requirements.md
  - Stdin:                  cat spec.md | vxd req --file -`,
		Args: cobra.MaximumNArgs(1),
		RunE: runReq,
	}
	cmd.Flags().StringP("file", "f", "", "read requirement from a file (use - for stdin)")
	cmd.Flags().Bool("godmode", false, "skip permission prompts on LLM calls (fully autonomous)")
	cmd.Flags().Bool("dry-run", false, "Simulate LLM responses for pipeline testing (no API calls)")
	cmd.SilenceUsage = true
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
	// if API fails (no credits, auth issues, etc.).
	godmode, _ := cmd.Flags().GetBool("godmode")
	if !godmode {
		godmode = s.Config.Planning.Godmode
	}
	client, err := buildPlanningClient(s.Config.Models.TechLead.Provider, godmode)
	if err != nil {
		return err
	}

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	if dryRun {
		client = llm.NewDryRunClient(500 * time.Millisecond)
		fmt.Fprintf(cmd.OutOrStdout(), "[DRY RUN] Using simulated LLM responses\n")
	}

	// Generate requirement ID
	reqID := ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String()

	// Determine repo path (current directory)
	repoPath, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("determine working directory: %w", err)
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
	fmt.Fprintf(out, "Requirement ID: %s\n\n", reqID)

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
	fmt.Fprintf(out, "Run 'vxd status --req %s' to track progress.\n", reqID)

	return nil
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
	// Try API first (handles large prompts natively).
	if p.apiClient != nil {
		resp, err := p.apiClient.Complete(ctx, req)
		if err == nil {
			return resp, nil
		}
		log.Printf("[planning] API call failed (%v), falling back to Claude CLI", err)
	}

	// Fall back to CLI.
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
		if apiKey := os.Getenv("ANTHROPIC_API_KEY"); apiKey != "" {
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
		if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
			apiClient = llm.NewRetryClient(llm.NewOpenAIClient(apiKey), 3)
		}
	case "google":
		if apiKey := os.Getenv("GOOGLE_AI_API_KEY"); apiKey != "" {
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
		apiKey := os.Getenv("GOOGLE_AI_API_KEY")
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
		} else if ak := os.Getenv("ANTHROPIC_API_KEY"); ak != "" {
			fallback = llm.NewRetryClient(llm.NewAnthropicClient(ak), 3)
		}

		if fallback != nil {
			return llm.NewFallbackClient(primary, fallback), nil
		}
		return primary, nil
	case "cli", "claude-cli":
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
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("claude CLI not found and ANTHROPIC_API_KEY not set")
		}
		return llm.NewRetryClient(llm.NewAnthropicClient(apiKey), 3), nil
	case "openai":
		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("OPENAI_API_KEY environment variable is required")
		}
		return llm.NewRetryClient(llm.NewOpenAIClient(apiKey), 3), nil
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s", provider)
	}
}
