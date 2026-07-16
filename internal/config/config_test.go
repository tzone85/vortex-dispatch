package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/config"
)

func TestDefaultConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	if cfg.Workspace.Backend != "sqlite" {
		t.Fatalf("expected backend 'sqlite', got %s", cfg.Workspace.Backend)
	}
	if cfg.Routing.JuniorMaxComplexity != 3 {
		t.Fatalf("expected junior max 3, got %d", cfg.Routing.JuniorMaxComplexity)
	}
	if cfg.Routing.IntermediateMaxComplexity != 5 {
		t.Fatalf("expected intermediate max 5, got %d", cfg.Routing.IntermediateMaxComplexity)
	}
	if cfg.Merge.AutoMerge != true {
		t.Fatal("expected auto_merge true")
	}
	if cfg.Merge.BaseBranch != "main" {
		t.Fatalf("expected base_branch 'main', got %s", cfg.Merge.BaseBranch)
	}
	if cfg.Monitor.PollIntervalMs != 10000 {
		t.Fatalf("expected poll_interval 10000, got %d", cfg.Monitor.PollIntervalMs)
	}
}

func TestDefaultConfig_Validates(t *testing.T) {
	cfg := config.DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config should be valid: %v", err)
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vxd.config.yaml")
	os.WriteFile(path, []byte(`
version: "1.0"
workspace:
  backend: sqlite
  log_level: debug
routing:
  junior_max_complexity: 5
  intermediate_max_complexity: 8
`), 0644)

	cfg, err := config.LoadFromFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Workspace.Backend != "sqlite" {
		t.Fatalf("expected 'sqlite', got %s", cfg.Workspace.Backend)
	}
	if cfg.Workspace.LogLevel != "debug" {
		t.Fatalf("expected 'debug', got %s", cfg.Workspace.LogLevel)
	}
	if cfg.Routing.JuniorMaxComplexity != 5 {
		t.Fatalf("expected 5, got %d", cfg.Routing.JuniorMaxComplexity)
	}
	// Defaults should still be present for unset fields
	if cfg.Monitor.PollIntervalMs != 10000 {
		t.Fatalf("expected default poll_interval 10000, got %d", cfg.Monitor.PollIntervalMs)
	}
}

func TestLoadFromFile_InvalidBackend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vxd.config.yaml")
	os.WriteFile(path, []byte(`
workspace:
  backend: postgres
`), 0644)

	_, err := config.LoadFromFile(path)
	if err == nil {
		t.Fatal("expected validation error for invalid backend")
	}
}

func TestLoadFromFile_InvalidLogLevel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vxd.config.yaml")
	os.WriteFile(path, []byte(`
workspace:
  log_level: verbose
`), 0644)

	_, err := config.LoadFromFile(path)
	if err == nil {
		t.Fatal("expected validation error for invalid log level")
	}
}

func TestValidation_ComplexityRange(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Routing.JuniorMaxComplexity = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for complexity 0")
	}

	cfg = config.DefaultConfig()
	cfg.Routing.JuniorMaxComplexity = 14
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for complexity 14")
	}
}

func TestValidation_IntermediateGteJunior(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Routing.JuniorMaxComplexity = 5
	cfg.Routing.IntermediateMaxComplexity = 3
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error: intermediate < junior")
	}
}

func TestLoadFromFile_WithRuntimes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vxd.config.yaml")
	os.WriteFile(path, []byte(`
runtimes:
  claude-code:
    command: claude
    args: ["--dangerously-skip-permissions"]
    models: ["opus-4", "sonnet-4"]
    detection:
      idle_pattern: "^\\$\\s*$"
      permission_pattern: "\\[Y/n\\]"
`), 0644)

	cfg, err := config.LoadFromFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	rt, ok := cfg.Runtimes["claude-code"]
	if !ok {
		t.Fatal("expected claude-code runtime")
	}
	if rt.Command != "claude" {
		t.Fatalf("expected command 'claude', got %s", rt.Command)
	}
	if len(rt.Models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(rt.Models))
	}
}

func TestConfig_PlanningDefaults(t *testing.T) {
	cfg := config.DefaultConfig()

	if len(cfg.Planning.SequentialFilePatterns) != 3 {
		t.Fatalf("expected 3 sequential file patterns, got %d", len(cfg.Planning.SequentialFilePatterns))
	}
	expectedPatterns := []string{"package.json", "*.config.*", "src/core/*"}
	for i, p := range expectedPatterns {
		if cfg.Planning.SequentialFilePatterns[i] != p {
			t.Fatalf("expected pattern %q at index %d, got %q", p, i, cfg.Planning.SequentialFilePatterns[i])
		}
	}

	if cfg.Planning.MaxStoryComplexity != 5 {
		t.Fatalf("expected max story complexity 5, got %d", cfg.Planning.MaxStoryComplexity)
	}

	if cfg.Planning.DesignApproach != "ddd-tdd" {
		t.Fatalf("expected design approach ddd-tdd, got %q", cfg.Planning.DesignApproach)
	}
}

func TestConfig_ValidateRejectsUnknownDesignApproach(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Planning.DesignApproach = "waterfall"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for unknown planning design approach")
	}
	if !strings.Contains(err.Error(), "planning.design_approach") {
		t.Fatalf("expected planning.design_approach error, got %v", err)
	}
}

func TestLoadFromFile_FileNotFound(t *testing.T) {
	_, err := config.LoadFromFile("/nonexistent/path.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestDefaultConfig_IncludesModels(t *testing.T) {
	cfg := config.DefaultConfig()

	if cfg.Models.TechLead.Provider != "anthropic" {
		t.Fatalf("expected tech_lead provider 'anthropic', got %s", cfg.Models.TechLead.Provider)
	}
	// Junior defaults to the Claude CLI subscription path (claude-haiku-4-5);
	// the previous "google"/"gemma-4-27b-it" default was a 404 model. See
	// TestDefaultConfig_NoInvalidJuniorModel.
	if cfg.Models.Junior.Provider != "anthropic" {
		t.Fatalf("expected junior provider 'anthropic', got %s", cfg.Models.Junior.Provider)
	}
	if cfg.Models.TechLead.MaxTokens != 16000 {
		t.Fatalf("expected tech_lead max_tokens 16000, got %d", cfg.Models.TechLead.MaxTokens)
	}
}

func TestDefaultConfig_IncludesRuntimes(t *testing.T) {
	cfg := config.DefaultConfig()

	if len(cfg.Runtimes) != 4 {
		t.Fatalf("expected 4 runtimes, got %d", len(cfg.Runtimes))
	}
	rt, ok := cfg.Runtimes["claude-code"]
	if !ok {
		t.Fatal("expected claude-code runtime in defaults")
	}
	if rt.Command != "claude" {
		t.Fatalf("expected command 'claude', got %s", rt.Command)
	}
}

func TestDefaultConfig_StuckThresholdIs600(t *testing.T) {
	cfg := config.DefaultConfig()
	const want = 600
	if cfg.Monitor.StuckThresholdS != want {
		t.Fatalf("expected default stuck_threshold_s=%d (10 min), got %d", want, cfg.Monitor.StuckThresholdS)
	}
}

func TestDefaultConfig_StuckThresholdConfigurable(t *testing.T) {
	// Verify the field can be overridden via YAML.
	dir := t.TempDir()
	path := filepath.Join(dir, "vxd.yaml")
	yaml := "version: \"1.0\"\nmonitor:\n  stuck_threshold_s: 300\n"
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	if cfg.Monitor.StuckThresholdS != 300 {
		t.Fatalf("expected stuck_threshold_s=300 after override, got %d", cfg.Monitor.StuckThresholdS)
	}
}

func TestDefaultConfig_IncludesPRTemplate(t *testing.T) {
	cfg := config.DefaultConfig()

	if cfg.Merge.PRTemplate == "" {
		t.Fatal("expected non-empty PR template in defaults")
	}
}

func TestDefaultYAML_RoundTrip(t *testing.T) {
	data, err := config.DefaultYAML()
	if err != nil {
		t.Fatalf("DefaultYAML: %v", err)
	}

	// Write to a temp file and load it back — should produce a valid config
	dir := t.TempDir()
	path := filepath.Join(dir, "vxd.yaml")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := config.LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile on generated YAML: %v", err)
	}

	// Verify key fields survived the roundtrip
	if cfg.Version != "1.0" {
		t.Fatalf("expected version '1.0', got %s", cfg.Version)
	}
	if cfg.Workspace.Backend != "sqlite" {
		t.Fatalf("expected backend 'sqlite', got %s", cfg.Workspace.Backend)
	}
	if cfg.Models.TechLead.Provider != "anthropic" {
		t.Fatalf("expected tech_lead provider 'anthropic', got %s", cfg.Models.TechLead.Provider)
	}
	if len(cfg.Runtimes) != 4 {
		t.Fatalf("expected 4 runtimes, got %d", len(cfg.Runtimes))
	}
	if cfg.Merge.PRTemplate == "" {
		t.Fatal("expected non-empty PR template after roundtrip")
	}
}

func TestLoadConfigChain_RepoFileFirst(t *testing.T) {
	dir := t.TempDir()

	// Create a repo-level vxd.yaml with a custom log level
	repoYAML := `
version: "1.0"
workspace:
  state_dir: "~/.vxd"
  backend: dolt
  log_level: debug
  log_retention_days: 30
routing:
  junior_max_complexity: 3
  intermediate_max_complexity: 5
  max_retries_before_escalation: 2
  max_qa_failures_before_escalation: 3
  max_senior_retries: 2
  max_manager_attempts: 2
cleanup:
  worktree_prune: immediate
  branch_retention_days: 7
  log_archive: dolt
merge:
  auto_merge: true
  base_branch: main
`
	repoPath := filepath.Join(dir, "vxd.yaml")
	os.WriteFile(repoPath, []byte(repoYAML), 0o644)

	cfg, err := config.LoadConfigChain(repoPath, filepath.Join(dir, "nonexistent", "config.yaml"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Workspace.LogLevel != "debug" {
		t.Errorf("expected debug log level from repo config, got %q", cfg.Workspace.LogLevel)
	}
}

func TestLoadConfigChain_FallsToGlobal(t *testing.T) {
	dir := t.TempDir()

	// No repo config, but global config exists
	globalYAML := `
version: "1.0"
workspace:
  state_dir: "~/.vxd"
  backend: dolt
  log_level: warn
  log_retention_days: 30
routing:
  junior_max_complexity: 3
  intermediate_max_complexity: 5
  max_retries_before_escalation: 2
  max_qa_failures_before_escalation: 3
  max_senior_retries: 2
  max_manager_attempts: 2
cleanup:
  worktree_prune: immediate
  branch_retention_days: 7
  log_archive: dolt
merge:
  auto_merge: true
  base_branch: main
`
	globalPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(globalPath, []byte(globalYAML), 0o644)

	cfg, err := config.LoadConfigChain(filepath.Join(dir, "nonexistent.yaml"), globalPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Workspace.LogLevel != "warn" {
		t.Errorf("expected warn log level from global config, got %q", cfg.Workspace.LogLevel)
	}
}

func TestLoadConfigChain_FallsToDefault(t *testing.T) {
	dir := t.TempDir()

	cfg, err := config.LoadConfigChain(
		filepath.Join(dir, "nope.yaml"),
		filepath.Join(dir, "also-nope.yaml"),
	)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// Should match DefaultConfig values
	def := config.DefaultConfig()
	if cfg.Workspace.LogLevel != def.Workspace.LogLevel {
		t.Errorf("expected default log level %q, got %q", def.Workspace.LogLevel, cfg.Workspace.LogLevel)
	}
	if cfg.Workspace.Backend != def.Workspace.Backend {
		t.Errorf("expected default backend %q, got %q", def.Workspace.Backend, cfg.Workspace.Backend)
	}
}

func TestDefaultYAML_HasHeader(t *testing.T) {
	data, err := config.DefaultYAML()
	if err != nil {
		t.Fatalf("DefaultYAML: %v", err)
	}

	header := "# VXD configuration"
	if len(data) < len(header) || string(data[:len(header)]) != header {
		t.Fatalf("expected YAML to start with %q, got %q", header, string(data[:40]))
	}
}

func TestDefaultConfig_IncludesBilling(t *testing.T) {
	cfg := config.DefaultConfig()

	if cfg.Billing.DefaultRate != 150.0 {
		t.Fatalf("expected default rate 150.0, got %f", cfg.Billing.DefaultRate)
	}
	if cfg.Billing.Currency != "USD" {
		t.Fatalf("expected currency 'USD', got %s", cfg.Billing.Currency)
	}
	if len(cfg.Billing.HoursPerPoint) != 6 {
		t.Fatalf("expected 6 hour tiers, got %d", len(cfg.Billing.HoursPerPoint))
	}
	low, high := cfg.Billing.HoursPerPoint[3][0], cfg.Billing.HoursPerPoint[3][1]
	if low != 2.0 || high != 3.0 {
		t.Fatalf("expected 3pt = [2.0, 3.0], got [%f, %f]", low, high)
	}
	if cfg.Billing.LLMCosts.Mode != "subscription" {
		t.Fatalf("expected mode 'subscription', got %s", cfg.Billing.LLMCosts.Mode)
	}
}

func TestValidation_BillingRate(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Billing.DefaultRate = -10
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for negative rate")
	}
}

func TestValidation_BillingCurrency(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Billing.Currency = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for empty currency")
	}
}

func TestValidation_BillingLLMMode(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Billing.LLMCosts.Mode = "free"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for invalid LLM cost mode")
	}
}

func TestValidation_BillingHoursPerPoint(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Billing.HoursPerPoint[3] = [2]float64{5.0, 2.0} // low > high
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for hours_per_point low > high")
	}
}

func TestValidation_ReviewMode(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Merge.ReviewMode = "invalid"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for invalid review mode")
	}
}

func TestValidation_ReviewMode_ValidValues(t *testing.T) {
	for _, mode := range []string{"", "auto", "manual", "plan_only"} {
		cfg := config.DefaultConfig()
		cfg.Merge.ReviewMode = mode
		if err := cfg.Validate(); err != nil {
			t.Fatalf("expected no error for review mode %q, got: %v", mode, err)
		}
	}
}

func TestValidate_DevDB_NullByDefault(t *testing.T) {
	cfg := config.DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Errorf("default config (devdb null) should validate, got %v", err)
	}
}

func TestValidate_DevDB_GhostRequiresTemplate(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DevDB = config.DevDBConfig{Provider: "ghost"}
	if err := cfg.Validate(); err == nil {
		t.Error("provider=ghost without template should fail validation")
	}
}

func TestValidate_DevDB_DockerRequiresTemplate(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DevDB = config.DevDBConfig{Provider: "docker"}
	if err := cfg.Validate(); err == nil {
		t.Error("provider=docker without template should fail validation")
	}
}

func TestValidate_DevDB_UnknownProvider(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DevDB = config.DevDBConfig{Provider: "potato", Template: "x"}
	if err := cfg.Validate(); err == nil {
		t.Error("unknown provider should fail validation")
	}
}

func TestValidate_DevDB_DockerWithTemplate(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DevDB = config.DevDBConfig{Provider: "docker", Template: "tpl"}
	if err := cfg.Validate(); err != nil {
		t.Errorf("docker + template should validate, got %v", err)
	}
}

// TestSLAConfig_AcceptsStringKeys verifies that max_minutes_per_complexity
// accepts both bare integer keys and YAML-quoted string keys without error.
func TestSLAConfig_AcceptsStringKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vxd.yaml")

	// Write config with quoted string keys (the "5": 60 form that breaks plain map[int]int).
	os.WriteFile(path, []byte(`
version: "1.0"
workspace:
  backend: sqlite
  log_level: info
sla:
  max_minutes_per_complexity:
    "1": 60
    "3": 120
    "5": 240
`), 0644)

	cfg, err := config.LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile with quoted string keys: %v", err)
	}
	if cfg.SLA.MaxMinutesPerComplexity[1] != 60 {
		t.Errorf("expected [1]=60, got %d", cfg.SLA.MaxMinutesPerComplexity[1])
	}
	if cfg.SLA.MaxMinutesPerComplexity[5] != 240 {
		t.Errorf("expected [5]=240, got %d", cfg.SLA.MaxMinutesPerComplexity[5])
	}

	// Also verify bare integer keys still work.
	path2 := filepath.Join(dir, "vxd2.yaml")
	os.WriteFile(path2, []byte(`
version: "1.0"
workspace:
  backend: sqlite
  log_level: info
sla:
  max_minutes_per_complexity:
    1: 60
    5: 240
`), 0644)

	cfg2, err := config.LoadFromFile(path2)
	if err != nil {
		t.Fatalf("LoadFromFile with bare int keys: %v", err)
	}
	if cfg2.SLA.MaxMinutesPerComplexity[1] != 60 {
		t.Errorf("bare int key: expected [1]=60, got %d", cfg2.SLA.MaxMinutesPerComplexity[1])
	}
	if cfg2.SLA.MaxMinutesPerComplexity[5] != 240 {
		t.Errorf("bare int key: expected [5]=240, got %d", cfg2.SLA.MaxMinutesPerComplexity[5])
	}
}

// TestConfigValidate_QAModelInertWarning pins the models.qa advisory: an
// operator-set (non-default) QA binding produces a warning that the QA stage
// is command-based, while the shipped default binding stays silent.
func TestConfigValidate_QAModelInertWarning(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Models.QA = config.ModelConfig{Provider: "codex", Model: "gpt-5.5"}

	warnings := cfg.Warnings()
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "models.qa") && strings.Contains(w, "inert") {
			found = true
			if !strings.Contains(w, "models.reviewer") {
				t.Errorf("warning should point to models.reviewer as the LLM review path: %q", w)
			}
		}
	}
	if !found {
		t.Fatalf("non-default models.qa binding must produce an inert-binding warning, got %v", warnings)
	}

	// Default config: no warnings (the shipped default binding is not an
	// operator statement of intent — warning every install is noise).
	if w := config.DefaultConfig().Warnings(); len(w) != 0 {
		t.Errorf("config.DefaultConfig().Warnings() = %v, want none", w)
	}

	// Empty provider (role unset entirely) also stays silent.
	cfg.Models.QA = config.ModelConfig{}
	if w := cfg.Warnings(); len(w) != 0 {
		t.Errorf("empty models.qa must not warn, got %v", w)
	}
}

// TestImproveDefaultDisabled pins the experimental self-improvement pipeline
// gate: OFF unless an operator explicitly opts in (WEAKNESSES.md P0-01).
func TestImproveDefaultDisabled(t *testing.T) {
	if config.DefaultConfig().Improve.Enabled {
		t.Fatal("DefaultConfig().Improve.Enabled must be false — the pipeline is experimental and opt-in")
	}
}
