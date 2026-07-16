package preflight

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/devdb/docker"
	"github.com/tzone85/vortex-dispatch/internal/devdb/ghost"
	"github.com/tzone85/vortex-dispatch/internal/engine"
	"github.com/tzone85/vortex-dispatch/internal/security"
)

// --- CRITICAL checks ---

// CheckTmux verifies tmux is installed and reachable on PATH.
// On Windows tmux is not natively available; the message points the operator
// to WSL2, which is the supported path for the agent execution pipeline.
func CheckTmux() Result {
	out, err := exec.Command("tmux", "-V").Output()
	if err != nil {
		msg := "tmux not found on PATH — install with: brew install tmux"
		if runtime.GOOS == "windows" {
			msg = "tmux is not available on native Windows. The agent execution pipeline requires tmux; " +
				"run VXD inside WSL2 (Ubuntu) where you can `sudo apt install tmux`. Read-only commands " +
				"(estimate, status, metrics, report, projects, config) still work on native Windows."
		}
		return Result{Name: "tmux", Severity: SeverityCritical, Passed: false, Message: msg}
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
	// #nosec G703 -- stateDir derives from $HOME on the operator's own host; no untrusted input reaches this path
	if _, err := os.Stat(stateDir); os.IsNotExist(err) {
		return Result{Name: "state_dir", Severity: SeverityInfo, Passed: true,
			Message: fmt.Sprintf("State dir: %s (will be created on first run)", stateDir)}
	}
	tmp := filepath.Join(stateDir, ".preflight-test")
	// #nosec G703 G306 -- writability probe with throwaway content under $HOME
	if err := os.WriteFile(tmp, []byte("test"), 0644); err != nil {
		return Result{Name: "state_dir", Severity: SeverityInfo, Passed: false,
			Message: fmt.Sprintf("State dir not writable: %s", stateDir)}
	}
	_ = os.Remove(tmp) // #nosec G703 -- best-effort cleanup of the probe file under $HOME
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

// CheckSecurityScanners warns when security scanner binaries the per-story
// security gate relies on are missing from PATH. The gate degrades gracefully
// (a missing tool is skipped, never fatal), so this check is the only surface
// that tells the operator scan coverage is reduced. lookPath is injected so
// the check can be unit-tested; pass exec.LookPath for the real host.
func CheckSecurityScanners(lookPath func(string) (string, error)) Result {
	var installed, missing []string
	var hints []string
	seen := map[string]bool{}
	for _, s := range security.KnownScanners() {
		if seen[s.Bin] {
			continue
		}
		seen[s.Bin] = true
		if _, err := lookPath(s.Bin); err == nil {
			installed = append(installed, s.Bin)
			continue
		}
		missing = append(missing, s.Bin)
		if hint := security.InstallHint(s.Bin); hint != "" {
			hints = append(hints, hint)
		}
	}
	if len(missing) == 0 {
		return Result{Name: "security_scanners", Severity: SeverityWarning, Passed: true,
			Message: fmt.Sprintf("Security scanners installed: %s", strings.Join(installed, ", "))}
	}
	return Result{Name: "security_scanners", Severity: SeverityWarning, Passed: false,
		Message: fmt.Sprintf(
			"Security scanners missing: %s — the security gate will skip them (reduced coverage). install: %s",
			strings.Join(missing, ", "), strings.Join(hints, " && "))}
}

// CheckQAModelInert warns when models.qa is bound to a non-default LLM: the
// QA stage is command-based (lint/build/test, engine/qa.go) and never calls
// an LLM, so the binding is inert — the operator expects a review pass that
// won't happen. Delegates to CheckQAModelInertWith after loading the active
// config the same way CheckBillingConfig does.
func CheckQAModelInert() Result {
	cfg := config.DefaultConfig()
	if c, err := config.LoadFromFile("vxd.yaml"); err == nil {
		cfg = c
	} else if home := os.Getenv("HOME"); home != "" {
		if c, err := config.LoadFromFile(filepath.Join(home, ".vxd", "config.yaml")); err == nil {
			cfg = c
		}
	}
	return CheckQAModelInertWith(cfg)
}

// CheckQAModelInertWith is the config-injected core of CheckQAModelInert,
// split out so it can be unit-tested without touching the filesystem.
func CheckQAModelInertWith(cfg config.Config) Result {
	for _, w := range cfg.Warnings() {
		if strings.Contains(w, "models.qa") {
			return Result{Name: "qa_model", Severity: SeverityWarning, Passed: false,
				Message: w + " Docs: README.md Configuration table (models.reviewer)."}
		}
	}
	return Result{Name: "qa_model", Severity: SeverityWarning, Passed: true,
		Message: "models.qa binding OK (QA stage is command-based; no inert LLM binding)"}
}

// --- Check sets ---

// DispatchChecks returns the checks run before every dispatch operation.
// These cover the minimum requirements for agents to function correctly.
// The exact count is pinned by TestDispatchChecks_Count and the docs
// (README, CLAUDE.md, AGENTS.md) — update all of them together.
func DispatchChecks() []Check {
	tmuxServerCheck := func() Result { return CheckTmuxServer(exec.LookPath, RunTmux) }
	return []Check{
		CheckTmux, tmuxServerCheck, CheckClaudeCLI, CheckGitRepo, CheckLLMAvailable,
		CheckAnthropicKeyConflict, CheckDiskSpace,
		CheckGHCLI, CheckNetwork, CheckStaleSessions, CheckGoogleAPIKey,
	}
}

// AllChecks returns the full check set including informational ones shown by
// `vxd preflight`. Count pinned by TestAllChecks_Count and the docs.
func AllChecks() []Check {
	binaryCheck := func() Result { return CheckBinaryPath("") }
	scannerCheck := func() Result { return CheckSecurityScanners(exec.LookPath) }
	return append(DispatchChecks(),
		binaryCheck, scannerCheck,
		CheckConfig, CheckProject, CheckStateDir, CheckBillingConfig, CheckOllama,
		CheckQAModelInert,
	)
}

// CheckDevDBProviderReachable verifies the configured devdb provider is reachable.
// Skipped (INFO) when provider is "" or "null".
func CheckDevDBProviderReachable(cfg config.Config) Result {
	name := "devdb-provider"
	switch cfg.DevDB.Provider {
	case "", "null":
		return Result{Name: name, Severity: SeverityInfo, Passed: true,
			Message: "devdb disabled (provider=null) — story DBs not provisioned"}
	case "docker":
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		p := docker.NewProvider(docker.Config{
			Image:          cfg.DevDB.Docker.Image,
			ContainerName:  cfg.DevDB.Docker.ContainerName,
			TemplateVolume: cfg.DevDB.Docker.TemplateVolume,
			Network:        cfg.DevDB.Docker.Network,
			HostPortRange:  cfg.DevDB.Docker.HostPortRange,
			Host:           cfg.DevDB.Docker.Host,
		})
		if err := p.Ping(ctx); err != nil {
			return Result{Name: name, Severity: SeverityCritical, Passed: false,
				Message: fmt.Sprintf("devdb docker provider unreachable: %v", err)}
		}
		return Result{Name: name, Severity: SeverityInfo, Passed: true,
			Message: "devdb docker provider reachable"}
	case "ghost":
		apiKey, err := ghost.ResolveAPIKey(cfg.DevDB.Ghost.APIKeyEnv, "")
		if err != nil {
			return Result{Name: name, Severity: SeverityCritical, Passed: false,
				Message: fmt.Sprintf("devdb ghost: %v", err)}
		}
		p, err := ghost.New(ghost.Config{APIKey: apiKey, SpaceID: cfg.DevDB.Ghost.SpaceID})
		if err != nil {
			return Result{Name: name, Severity: SeverityCritical, Passed: false,
				Message: fmt.Sprintf("devdb ghost: %v", err)}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := p.Ping(ctx); err != nil {
			return Result{Name: name, Severity: SeverityCritical, Passed: false,
				Message: fmt.Sprintf("devdb ghost provider unreachable: %v", err)}
		}
		return Result{Name: name, Severity: SeverityInfo, Passed: true,
			Message: "devdb ghost provider reachable"}
	default:
		return Result{Name: name, Severity: SeverityCritical, Passed: false,
			Message: fmt.Sprintf("devdb provider %q is not recognised", cfg.DevDB.Provider)}
	}
}

// CheckDevDBTemplateExists verifies that the configured template DB is present
// in the provider. WARNING (not CRITICAL) so first-time setup can proceed.
func CheckDevDBTemplateExists(cfg config.Config) Result {
	name := "devdb-template"
	if cfg.DevDB.Provider == "" || cfg.DevDB.Provider == "null" {
		return Result{Name: name, Severity: SeverityInfo, Passed: true,
			Message: "devdb disabled — no template required"}
	}
	if cfg.DevDB.Template == "" {
		return Result{Name: name, Severity: SeverityInfo, Passed: true,
			Message: "no template configured (devdb.template empty) — stories get empty DBs"}
	}
	if cfg.DevDB.Provider != "docker" {
		// Ghost template lookup is performed inline at Fork time (Provider.Fork
		// calls ListDBs and returns ErrTemplateMiss when not found). A dedicated
		// preflight check for ghost templates is out of scope for SP2.
		return Result{Name: name, Severity: SeverityInfo, Passed: true,
			Message: fmt.Sprintf("template check skipped for provider=%s (verified at fork time)", cfg.DevDB.Provider)}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p := docker.NewProvider(docker.Config{
		Image:          cfg.DevDB.Docker.Image,
		ContainerName:  cfg.DevDB.Docker.ContainerName,
		TemplateVolume: cfg.DevDB.Docker.TemplateVolume,
		Network:        cfg.DevDB.Docker.Network,
		HostPortRange:  cfg.DevDB.Docker.HostPortRange,
		Host:           cfg.DevDB.Docker.Host,
	})
	templates, err := p.ListTemplates(ctx)
	if err != nil {
		return Result{Name: name, Severity: SeverityWarning, Passed: false,
			Message: fmt.Sprintf("devdb template list failed: %v", err)}
	}
	for _, t := range templates {
		if t == cfg.DevDB.Template {
			return Result{Name: name, Severity: SeverityInfo, Passed: true,
				Message: fmt.Sprintf("devdb template %q is present", cfg.DevDB.Template)}
		}
	}
	return Result{Name: name, Severity: SeverityWarning, Passed: false,
		Message: fmt.Sprintf("devdb template %q not found in provider (run: vxd db template create %s)",
			cfg.DevDB.Template, cfg.DevDB.Template)}
}

// DispatchChecksWithConfig returns the dispatch check set with cfg-dependent
// devdb checks bound. Use this from CLI commands that load cfg.
func DispatchChecksWithConfig(cfg config.Config) []Check {
	extras := []Check{
		func() Result { return CheckDevDBProviderReachable(cfg) },
		func() Result { return CheckDevDBTemplateExists(cfg) },
	}
	return append(DispatchChecks(), extras...)
}

// AllChecksWithConfig returns the full check set including devdb.
func AllChecksWithConfig(cfg config.Config) []Check {
	extras := []Check{
		func() Result { return CheckDevDBProviderReachable(cfg) },
		func() Result { return CheckDevDBTemplateExists(cfg) },
	}
	return append(AllChecks(), extras...)
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
