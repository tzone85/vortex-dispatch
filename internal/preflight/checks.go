package preflight

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/engine"
)

// --- CRITICAL checks ---

// CheckTmux verifies tmux is installed and reachable on PATH.
func CheckTmux() Result {
	out, err := exec.Command("tmux", "-V").Output()
	if err != nil {
		return Result{Name: "tmux", Severity: SeverityCritical, Passed: false,
			Message: "tmux not found on PATH — install with: brew install tmux"}
	}
	version := strings.TrimSpace(string(out))
	return Result{Name: "tmux", Severity: SeverityCritical, Passed: true,
		Message: fmt.Sprintf("tmux installed (%s)", version)}
}

// CheckClaudeCLI verifies the claude CLI binary is installed and on PATH.
func CheckClaudeCLI() Result {
	path, err := exec.LookPath("claude")
	if err != nil {
		return Result{Name: "claude", Severity: SeverityCritical, Passed: false,
			Message: "claude CLI not found — install from https://claude.ai/download"}
	}
	return Result{Name: "claude", Severity: SeverityCritical, Passed: true,
		Message: fmt.Sprintf("claude CLI installed (%s)", path)}
}

// CheckGitRepo verifies the current directory is inside a valid git repository
// with at least one commit.
func CheckGitRepo() Result {
	toplevel, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return Result{Name: "git", Severity: SeverityCritical, Passed: false,
			Message: "Not in a git repository"}
	}
	_, err = exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return Result{Name: "git", Severity: SeverityCritical, Passed: false,
			Message: "Git repo has no commits"}
	}
	repoName := filepath.Base(strings.TrimSpace(string(toplevel)))
	return Result{Name: "git", Severity: SeverityCritical, Passed: true,
		Message: fmt.Sprintf("Git repo valid (%s)", repoName)}
}

// CheckLLMAvailable verifies at least one LLM source is configured:
// Anthropic API key, claude CLI, or Google AI API key.
func CheckLLMAvailable() Result {
	var sources []string
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		sources = append(sources, "Anthropic API")
	}
	if _, err := exec.LookPath("claude"); err == nil {
		sources = append(sources, "Claude CLI")
	}
	if os.Getenv("GOOGLE_AI_API_KEY") != "" {
		sources = append(sources, "Google AI")
	}
	if len(sources) == 0 {
		return Result{Name: "llm", Severity: SeverityCritical, Passed: false,
			Message: "No LLM available — set ANTHROPIC_API_KEY or install claude CLI"}
	}
	return Result{Name: "llm", Severity: SeverityCritical, Passed: true,
		Message: fmt.Sprintf("LLM available (%s)", strings.Join(sources, " + "))}
}

// CheckAnthropicKeyConflict warns when ANTHROPIC_API_KEY is set alongside
// Claude CLI. The API key takes priority over the Max subscription (OAuth),
// so agents will fail with "credit balance too low" if the key is exhausted
// even when the user has an active Max subscription.
func CheckAnthropicKeyConflict() Result {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	_, cliErr := exec.LookPath("claude")
	hasCLI := cliErr == nil

	if apiKey != "" && hasCLI {
		return Result{Name: "anthropic_key", Severity: SeverityWarning, Passed: false,
			Message: "ANTHROPIC_API_KEY is set alongside Claude CLI — agents will use API credits instead of Max subscription. Run 'unset ANTHROPIC_API_KEY' if you have a Max subscription"}
	}
	return Result{Name: "anthropic_key", Severity: SeverityWarning, Passed: true,
		Message: "No API key conflict"}
}

// --- WARNING checks ---

// CheckGHCLI verifies gh CLI is installed and authenticated.
// PR creation and auto-merge require a working gh session.
func CheckGHCLI() Result {
	if _, err := exec.LookPath("gh"); err != nil {
		return Result{Name: "gh", Severity: SeverityWarning, Passed: false,
			Message: "gh CLI not found — PR creation disabled"}
	}
	out, err := exec.Command("gh", "auth", "status").CombinedOutput()
	if err != nil {
		return Result{Name: "gh", Severity: SeverityWarning, Passed: false,
			Message: "gh CLI not authenticated — PR auto-merge disabled"}
	}
	username := parseGHUsername(string(out))
	return Result{Name: "gh", Severity: SeverityWarning, Passed: true,
		Message: fmt.Sprintf("gh CLI authenticated (%s)", username)}
}

// CheckNetwork performs a DNS lookup to verify network connectivity.
// Times out after 3 seconds.
func CheckNetwork() Result {
	done := make(chan error, 1)
	go func() {
		_, err := net.LookupHost("api.anthropic.com")
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			return Result{Name: "network", Severity: SeverityWarning, Passed: false,
				Message: "Network unavailable — LLM API calls may fail"}
		}
		return Result{Name: "network", Severity: SeverityWarning, Passed: true,
			Message: "Network connectivity (DNS OK)"}
	case <-time.After(3 * time.Second):
		return Result{Name: "network", Severity: SeverityWarning, Passed: false,
			Message: "Network check timed out (3s) — LLM API calls may fail"}
	}
}

// CheckStaleSessions reports any lingering vxd-* tmux sessions that may
// indicate a previous run did not clean up properly.
func CheckStaleSessions() Result {
	out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output()
	if err != nil {
		return Result{Name: "stale_sessions", Severity: SeverityWarning, Passed: true,
			Message: "No stale tmux sessions"}
	}
	var stale []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.HasPrefix(line, "vxd-") {
			stale = append(stale, line)
		}
	}
	if len(stale) == 0 {
		return Result{Name: "stale_sessions", Severity: SeverityWarning, Passed: true,
			Message: "No stale tmux sessions"}
	}
	return Result{Name: "stale_sessions", Severity: SeverityWarning, Passed: false,
		Message: fmt.Sprintf("%d stale tmux sessions found (vxd-*) — run: tmux kill-server to clean up", len(stale))}
}

// CheckGoogleAPIKey reports whether the GOOGLE_AI_API_KEY env var is set.
// Without it, Gemma execution roles are unavailable.
func CheckGoogleAPIKey() Result {
	if os.Getenv("GOOGLE_AI_API_KEY") != "" {
		return Result{Name: "google_api_key", Severity: SeverityWarning, Passed: true,
			Message: "GOOGLE_AI_API_KEY set"}
	}
	return Result{Name: "google_api_key", Severity: SeverityWarning, Passed: false,
		Message: "GOOGLE_AI_API_KEY not set — Gemma execution roles unavailable"}
}

// --- INFO checks ---

// CheckConfig loads and validates the active config file (repo vxd.yaml or
// global ~/.vxd/config.yaml). Reports defaults if no config file is found.
func CheckConfig() Result {
	cfgPaths := []struct {
		path string
		src  string
	}{
		{"vxd.yaml", "repo"},
		{filepath.Join(os.Getenv("HOME"), ".vxd", "config.yaml"), "global"},
	}
	for _, cp := range cfgPaths {
		if _, err := os.Stat(cp.path); err == nil {
			cfg, err := config.LoadFromFile(cp.path)
			if err != nil {
				return Result{Name: "config", Severity: SeverityInfo, Passed: false,
					Message: fmt.Sprintf("Config error in %s: %v", cp.path, err)}
			}
			if err := cfg.Validate(); err != nil {
				return Result{Name: "config", Severity: SeverityInfo, Passed: false,
					Message: fmt.Sprintf("Config validation failed: %v", err)}
			}
			return Result{Name: "config", Severity: SeverityInfo, Passed: true,
				Message: fmt.Sprintf("Config: %s (%s)", filepath.Base(cp.path), cp.src)}
		}
	}
	return Result{Name: "config", Severity: SeverityInfo, Passed: true,
		Message: "Config: defaults (no file found)"}
}

// CheckProject resolves the active project name from VXD_PROJECT env var or
// the current git repository root directory name.
func CheckProject() Result {
	if proj := os.Getenv("VXD_PROJECT"); proj != "" {
		return Result{Name: "project", Severity: SeverityInfo, Passed: true,
			Message: fmt.Sprintf("Project: %s (VXD_PROJECT env)", proj)}
	}
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return Result{Name: "project", Severity: SeverityInfo, Passed: false,
			Message: "Project: unknown (not in git repo)"}
	}
	name := engine.SanitizeProjectName(filepath.Base(strings.TrimSpace(string(out))))
	return Result{Name: "project", Severity: SeverityInfo, Passed: true,
		Message: fmt.Sprintf("Project: %s (auto-detected from git)", name)}
}

// CheckStateDir verifies the VXD state directory exists and is writable,
// or reports that it will be created on first run.
func CheckStateDir() Result {
	home := os.Getenv("HOME")
	stateDir := filepath.Join(home, ".vxd", "projects")
	if _, err := os.Stat(stateDir); os.IsNotExist(err) {
		return Result{Name: "state_dir", Severity: SeverityInfo, Passed: true,
			Message: fmt.Sprintf("State dir: %s (will be created on first run)", stateDir)}
	}
	tmp := filepath.Join(stateDir, ".preflight-test")
	if err := os.WriteFile(tmp, []byte("test"), 0644); err != nil {
		return Result{Name: "state_dir", Severity: SeverityInfo, Passed: false,
			Message: fmt.Sprintf("State dir not writable: %s", stateDir)}
	}
	os.Remove(tmp)
	return Result{Name: "state_dir", Severity: SeverityInfo, Passed: true,
		Message: fmt.Sprintf("State dir: %s", stateDir)}
}

// CheckBillingConfig reports whether a billing rate has been configured.
func CheckBillingConfig() Result {
	cfg := config.DefaultConfig()
	if c, err := config.LoadFromFile("vxd.yaml"); err == nil {
		cfg = c
	} else if home := os.Getenv("HOME"); home != "" {
		if c, err := config.LoadFromFile(filepath.Join(home, ".vxd", "config.yaml")); err == nil {
			cfg = c
		}
	}
	if cfg.Billing.DefaultRate <= 0 {
		return Result{Name: "billing", Severity: SeverityInfo, Passed: false,
			Message: "Billing: no rate configured"}
	}
	return Result{Name: "billing", Severity: SeverityInfo, Passed: true,
		Message: fmt.Sprintf("Billing: $%.0f/hr %s", cfg.Billing.DefaultRate, cfg.Billing.Currency)}
}

// CheckOllama checks whether Ollama is installed and its server is running.
// Ollama is optional for VXD (only required for NXD), so this is informational.
func CheckOllama() Result {
	_, err := exec.LookPath("ollama")
	if err != nil {
		return Result{Name: "ollama", Severity: SeverityInfo, Passed: true,
			Message: "Ollama not installed (optional for VXD)"}
	}

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://localhost:11434/api/version")
	if err != nil {
		return Result{Name: "ollama", Severity: SeverityInfo, Passed: true,
			Message: "Ollama installed but server not running"}
	}
	defer resp.Body.Close()

	var versionResp struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&versionResp); err != nil || versionResp.Version == "" {
		return Result{Name: "ollama", Severity: SeverityInfo, Passed: true,
			Message: "Ollama installed, server running"}
	}
	return Result{Name: "ollama", Severity: SeverityInfo, Passed: true,
		Message: fmt.Sprintf("Ollama installed, server running (v%s)", versionResp.Version)}
}

// CheckBinaryPath warns when the running vxd binary is outside ~/.local/bin/,
// which means PATH order is wrong or a stale build exists at ~/go/bin/.
// The check accepts an explicit executablePath so it can be unit-tested without
// actually invoking os.Executable. Pass "" to use the real executable path.
func CheckBinaryPath(executablePath string) Result {
	var err error
	if executablePath == "" {
		executablePath, err = os.Executable()
		if err != nil {
			return Result{Name: "binary_path", Severity: SeverityWarning, Passed: true,
				Message: "binary_path: could not determine executable path (non-fatal)"}
		}
		executablePath, _ = filepath.EvalSymlinks(executablePath)
	}

	canonical := filepath.Join(os.Getenv("HOME"), ".local", "bin")
	if strings.HasPrefix(executablePath, canonical) {
		return Result{Name: "binary_path", Severity: SeverityWarning, Passed: true,
			Message: fmt.Sprintf("binary at correct location (%s)", executablePath)}
	}

	remediation := fmt.Sprintf("rm %s", executablePath)
	return Result{Name: "binary_path", Severity: SeverityWarning, Passed: false,
		Message: fmt.Sprintf(
			"vxd is running from %s (expected %s) — you may be running a stale build. "+
				"Fix: %s  OR  ensure %s appears before %s in your PATH",
			executablePath, canonical, remediation, canonical, filepath.Dir(executablePath),
		)}
}

// --- Check sets ---

// DispatchChecks returns the 9 checks run before every dispatch operation.
// These cover the minimum requirements for agents to function correctly.
func DispatchChecks() []Check {
	return []Check{
		CheckTmux, CheckClaudeCLI, CheckGitRepo, CheckLLMAvailable,
		CheckAnthropicKeyConflict,
		CheckGHCLI, CheckNetwork, CheckStaleSessions, CheckGoogleAPIKey,
	}
}

// AllChecks returns all 14 checks including informational ones shown by
// `vxd preflight`.
func AllChecks() []Check {
	binaryCheck := func() Result { return CheckBinaryPath("") }
	return append(DispatchChecks(),
		binaryCheck,
		CheckConfig, CheckProject, CheckStateDir, CheckBillingConfig, CheckOllama,
	)
}

// parseGHUsername extracts the authenticated GitHub username from the output
// of `gh auth status`. Returns "unknown" if the username cannot be parsed.
func parseGHUsername(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "account") {
			parts := strings.Fields(line)
			for i, p := range parts {
				if p == "account" && i+1 < len(parts) {
					return parts[i+1]
				}
			}
		}
	}
	return "unknown"
}
