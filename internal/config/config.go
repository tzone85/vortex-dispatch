// Package config provides configuration types, loading, defaults, and
// validation for VXD (Vortex Dispatch).
package config

import (
	"fmt"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Config is the top-level VXD configuration.
type Config struct {
	Version      string                   `yaml:"version"`
	Workspace    WorkspaceConfig          `yaml:"workspace"`
	Models       ModelsConfig             `yaml:"models"`
	Routing      RoutingConfig            `yaml:"routing"`
	Monitor      MonitorConfig            `yaml:"monitor"`
	Cleanup      CleanupConfig            `yaml:"cleanup"`
	Merge        MergeConfig              `yaml:"merge"`
	Planning     PlanningConfig           `yaml:"planning"`
	Runtimes     map[string]RuntimeConfig `yaml:"runtimes"`
	Billing      BillingConfig            `yaml:"billing"`
	QA           QAConfig                 `yaml:"qa"`
	Security     SecurityConfig           `yaml:"security,omitempty"`
	SLA          SLAConfig                `yaml:"sla"`
	Secrets      SecretsConfig            `yaml:"secrets"`
	Notify       NotifyConfig             `yaml:"notify,omitempty"`
	Autoresearch AutoresearchConfig       `yaml:"autoresearch,omitempty"`
	DevDB        DevDBConfig              `yaml:"devdb,omitempty"`
	Dashboard    DashboardConfig          `yaml:"dashboard,omitempty"`
	Improve      ImproveConfig            `yaml:"improve,omitempty"`
}

// ImproveConfig gates the experimental self-improvement pipeline (the
// vxd-improve daily run). The pipeline has produced 0 code-actionable
// findings to date and its email delivery has never succeeded — it is
// research scaffolding, not a shipping capability, so it is OFF unless an
// operator explicitly opts in (here or via VXD_IMPROVE_ENABLED=1).
type ImproveConfig struct {
	Enabled bool `yaml:"enabled"` // default false — experimental, opt-in only
}

// DashboardConfig controls the always-on status surface. When AutoStart is
// true, `vxd req` forks a detached `vxd dashboard --web` daemon (or reuses an
// existing one) so submitted requirements are visible in a browser without
// any extra command. AutoOpen toggles whether `vxd req` also tries to open
// the user's default browser at the dashboard URL.
type DashboardConfig struct {
	AutoStart bool `yaml:"auto_start"` // default true
	AutoOpen  bool `yaml:"auto_open"`  // default true
	Port      int  `yaml:"port"`       // default 8787
}

// SecretsConfig configures the secrets provider.
type SecretsConfig struct {
	Provider   string `yaml:"provider"`    // "env" (default) | "vault"
	VaultAddr  string `yaml:"vault_addr"`  // e.g. http://127.0.0.1:8200
	VaultToken string `yaml:"vault_token"` // X-Vault-Token; consider using VAULT_TOKEN env instead
	VaultMount string `yaml:"vault_mount"` // KV v2 mount path, default "secret"
	VaultPath  string `yaml:"vault_path"`  // path within mount, default "vxd"
}

// NotifyConfig configures outbound webhook notifications.
type NotifyConfig struct {
	SlackWebhookURL  string `yaml:"slack_webhook_url,omitempty"`  // empty disables Slack
	NotifyOnSLA      bool   `yaml:"notify_on_sla,omitempty"`      // notify on STORY_SLA_BREACHED
	NotifyOnComplete bool   `yaml:"notify_on_complete,omitempty"` // notify on REQ_COMPLETED
}

// PlanningConfig controls how the planner decomposes requirements into stories.
type PlanningConfig struct {
	SequentialFilePatterns []string `yaml:"sequential_file_patterns"`
	MaxStoryComplexity     int      `yaml:"max_story_complexity"`
	Godmode                bool     `yaml:"godmode"`
	DesignApproach         string   `yaml:"design_approach"` // "ddd-tdd" (default), "tdd", "standard"
	// EmitScribeStory, when true (default), makes the planner append a final
	// "scribe" story that depends on all others and updates the project README
	// (greenfield-aware, edits only within vxd:scribe markers on existing
	// READMEs) plus links the generated docs. Clients can opt out per project.
	EmitScribeStory bool `yaml:"emit_scribe_story"`
	// EmitIntegrationStory, when true (default), makes the planner append a
	// final "integration" story (before the scribe) that depends on all code
	// stories, wires every component into the application entry point, bridges
	// interface mismatches with adapters, and writes an end-to-end smoke test
	// that boots the app and asserts the documented surface responds. Closes the
	// systemic gap where per-story unit tests pass but the whole never composes.
	EmitIntegrationStory bool `yaml:"emit_integration_story"`
}

// WorkspaceConfig holds workspace-level settings.
type WorkspaceConfig struct {
	StateDir         string `yaml:"state_dir"`
	Backend          string `yaml:"backend"`
	LogLevel         string `yaml:"log_level"`
	LogRetentionDays int    `yaml:"log_retention_days"`
}

// ModelConfig describes a single LLM model binding.
type ModelConfig struct {
	Provider  string `yaml:"provider"`
	Model     string `yaml:"model"`
	MaxTokens int    `yaml:"max_tokens"`
}

// ModelsConfig maps agent roles to their model bindings.
type ModelsConfig struct {
	TechLead     ModelConfig `yaml:"tech_lead"`
	Senior       ModelConfig `yaml:"senior"`
	Intermediate ModelConfig `yaml:"intermediate"`
	Junior       ModelConfig `yaml:"junior"`
	QA           ModelConfig `yaml:"qa"`
	Supervisor   ModelConfig `yaml:"supervisor"`
	Manager      ModelConfig `yaml:"manager"`
	// Reviewer is the model for the post-execution code-review LLM call.
	// Optional: when Provider is empty it falls back to Senior. Unlike Senior,
	// the reviewer is never spawned as a coding agent, so it can use a provider
	// (e.g. codex) that has no agent runtime.
	Reviewer ModelConfig `yaml:"reviewer"`
}

// RoutingConfig controls how tasks are assigned to agent tiers.
type RoutingConfig struct {
	JuniorMaxComplexity           int `yaml:"junior_max_complexity"`
	IntermediateMaxComplexity     int `yaml:"intermediate_max_complexity"`
	MaxRetriesBeforeEscalation    int `yaml:"max_retries_before_escalation"`
	MaxQAFailuresBeforeEscalation int `yaml:"max_qa_failures_before_escalation"`
	MaxSeniorRetries              int `yaml:"max_senior_retries"`
	MaxManagerAttempts            int `yaml:"max_manager_attempts"`
	MaxConcurrentAgents           int `yaml:"max_concurrent_agents"`
}

// MonitorConfig controls the supervisor monitoring loop.
type MonitorConfig struct {
	PollIntervalMs         int `yaml:"poll_interval_ms"`
	StuckThresholdS        int `yaml:"stuck_threshold_s"`
	ContextFreshnessTokens int `yaml:"context_freshness_tokens"`
	// PipelineTimeoutS bounds the whole post-execution pipeline (review + QA +
	// merge/conflict-resolution) per story. Slow LLM reviewers (e.g. the Codex
	// agent loop) plus rebase-conflict resolution under concurrent builds can
	// exceed a tight limit and trip "context deadline exceeded". Default 900s.
	PipelineTimeoutS int `yaml:"pipeline_timeout_s"`
}

// CleanupConfig controls post-task cleanup behaviour.
type CleanupConfig struct {
	WorktreePrune       string `yaml:"worktree_prune"`
	BranchRetentionDays int    `yaml:"branch_retention_days"`
	LogArchive          string `yaml:"log_archive"`
	// DeleteDanglingBranches, when true (default), removes the local + remote
	// branches of a requirement's non-merged stories once the requirement
	// completes. Deleting a remote branch auto-closes its open PR on GitHub, so
	// this leaves no dangling branches or PRs behind. Set false to keep them.
	DeleteDanglingBranches bool `yaml:"delete_dangling_branches"`
}

// MergeConfig controls how completed work is merged.
type MergeConfig struct {
	AutoMerge  bool   `yaml:"auto_merge"`
	ReviewMode string `yaml:"review_mode"`
	BaseBranch string `yaml:"base_branch"`
	PRTemplate string `yaml:"pr_template"`
}

// RuntimeDetection holds patterns used to detect runtime states.
type RuntimeDetection struct {
	IdlePattern       string `yaml:"idle_pattern"`
	PermissionPattern string `yaml:"permission_pattern"`
	PlanModePattern   string `yaml:"plan_mode_pattern,omitempty"`
}

// RuntimeConfig describes an external AI coding runtime.
type RuntimeConfig struct {
	Command   string           `yaml:"command"`
	Args      []string         `yaml:"args"`
	Models    []string         `yaml:"models"`
	Detection RuntimeDetection `yaml:"detection"`

	// Runner selects the execution target: "tmux" (default), "docker", or "ssh".
	// Phase 1 deployments use tmux. Phase 2 can use docker (containerized
	// agents) or ssh (remote agents).
	Runner string `yaml:"runner,omitempty"`

	// Docker holds container configuration when Runner == "docker".
	Docker DockerRunnerConfig `yaml:"docker,omitempty"`

	// SSH holds remote-execution configuration when Runner == "ssh".
	SSH SSHRunnerConfig `yaml:"ssh,omitempty"`
}

// DockerRunnerConfig configures the Docker execution target.
type DockerRunnerConfig struct {
	Image      string   `yaml:"image"`
	Network    string   `yaml:"network,omitempty"`
	ExtraFlags []string `yaml:"extra_flags,omitempty"`
}

// SSHRunnerConfig configures the SSH execution target.
type SSHRunnerConfig struct {
	Host       string   `yaml:"host"`
	KeyFile    string   `yaml:"key_file,omitempty"`
	RemoteDir  string   `yaml:"remote_dir,omitempty"`
	ExtraFlags []string `yaml:"extra_flags,omitempty"`
}

// QAConfig controls quality assurance checks.
type QAConfig struct {
	SuccessCriteria []SuccessCriterion `yaml:"success_criteria,omitempty"`
	// DisablePreMergeVerify turns OFF the repo-wide pre-merge QA gate. The gate
	// re-runs lint/build/test on the rebased worktree before merging and blocks
	// a story that turns a green base branch red (keeping main always-green).
	// Default (false) = gate ON. It never blocks when the base is already red.
	DisablePreMergeVerify bool `yaml:"disable_pre_merge_verify,omitempty"`
	// DisableCompletionGate turns OFF the requirement-completion verification
	// gate. The gate verifies the composed mainline (build + tests) after all
	// stories merge and only emits REQ_COMPLETED when it is green — otherwise it
	// auto-fixes a red build (see CompletionFixCycles) and, failing that, emits
	// REQ_BLOCKED. Default (false) = gate ON. When disabled, the legacy advisory
	// verification runs and the requirement always completes.
	DisableCompletionGate bool `yaml:"disable_completion_gate,omitempty"`
	// CompletionFixCycles is the number of automatic fix cycles the completion
	// gate runs against a red composed mainline before blocking. 0 uses the
	// default of 2. Set to a negative value to disable auto-fix (hard gate only:
	// verify once, block on red).
	CompletionFixCycles int `yaml:"completion_fix_cycles,omitempty"`
}

// SecurityConfig controls the security agent: the per-story pre-merge security
// gate, the standalone `vxd security scan`, and the self-upskilling knowledge
// base shared by both.
type SecurityConfig struct {
	// DisableGate turns OFF the per-story pre-merge security gate. The gate runs
	// the deterministic scanners + an LLM threat-model review on each story and
	// pauses the requirement (for human decision) when a finding meets/exceeds
	// GateSeverity. Default (false) = gate ON. The standalone scan command always
	// works regardless of this flag.
	DisableGate bool `yaml:"disable_gate,omitempty"`
	// GateSeverity is the block threshold: a finding at or above this severity
	// pauses the story. One of critical|high|medium|low. Empty ⇒ "high".
	GateSeverity string `yaml:"gate_severity,omitempty"`
	// RequireScanners makes the per-story gate STRICT about coverage: when any
	// applicable scanner is skipped (binary missing) or fails, the story is
	// blocked (pause for a human) instead of passing with reduced coverage.
	// Default false = graceful degradation (coverage loss is logged and shown
	// by the security_scanners preflight check, but never blocks a merge).
	RequireScanners bool `yaml:"require_scanners,omitempty"`
	// AutoLearn grows the knowledge base from confirmed high+ findings so future
	// builds inherit vulnerability classes seen in past ones. Default true.
	AutoLearn bool `yaml:"auto_learn"`
	// KBPath overrides where the knowledge base persists. Empty ⇒
	// <state_dir>/security/knowledge.json.
	KBPath string `yaml:"kb_path,omitempty"`
	// StrictShellCommands hardens ValidateShellCommand for config-supplied
	// command strings (qa.success_criteria commands, autoresearch metric
	// command): in addition to command substitution, it rejects pipes,
	// `;`/`&&`/`||` chaining, background `&`, and redirection. Multi-step
	// work is expressed via `command_list` instead. Default false for
	// backward compatibility — single-operator installs legitimately pipe
	// QA output. SaaS-hosted / multi-tenant deploys should set true.
	StrictShellCommands bool `yaml:"strict_shell_commands,omitempty"`
}

// SuccessCriterion defines a declarative QA check.
type SuccessCriterion struct {
	Kind    string `yaml:"kind"`
	Value   string `yaml:"value,omitempty"`
	Path    string `yaml:"path,omitempty"`
	Message string `yaml:"message,omitempty"`

	// Command is the shell command for migration_succeeds. Subject to
	// ValidateShellCommand in the mode selected by
	// security.strict_shell_commands.
	Command string `yaml:"command,omitempty"`
	// CommandList is the strict-mode-friendly alternative to Command:
	// VXD runs the entries sequentially (all must succeed), so multi-step
	// work needs no shell chaining metacharacters in the YAML. Mutually
	// exclusive with Command.
	CommandList []string `yaml:"command_list,omitempty"`
	// SQL, ExpectedRows, SchemaBaseline configure the DB-touching criteria
	// (sql_query_returns, schema_changed) shipped in SP5.
	SQL            string `yaml:"sql,omitempty"`
	ExpectedRows   *int   `yaml:"expected_rows,omitempty"`
	SchemaBaseline string `yaml:"schema_baseline,omitempty"`
}

// AutoresearchConfig configures the per-repo autoresearch experiment harness.
// See docs/superpowers/specs/2026-05-02-autoresearch-harness-design.md.
type AutoresearchConfig struct {
	Enabled        bool                `yaml:"enabled"`
	Metric         AutoresearchMetric  `yaml:"metric"`
	EditablePaths  []string            `yaml:"editable_paths"`
	ForbiddenPaths []string            `yaml:"forbidden_paths,omitempty"`
	Gate           string              `yaml:"gate"`        // "auto" | "winning" | "pr"
	Budget         string              `yaml:"budget"`      // duration string, e.g. "5m"
	Parallel       int                 `yaml:"parallel"`    // max concurrent experiments
	Continuous     bool                `yaml:"continuous"`  // run back-to-back vs scheduled batch only
	Schedule       AutoresearchSchedule `yaml:"schedule,omitempty"`
	Tripwire       AutoresearchTripwire `yaml:"tripwire,omitempty"`
	Bayes          AutoresearchBayes    `yaml:"bayes,omitempty"`
}

// AutoresearchMetric describes how to measure an experiment outcome.
type AutoresearchMetric struct {
	Command        string                  `yaml:"command"`
	Parser         AutoresearchMetricParser `yaml:"parser"`
	TieEpsilon     float64                  `yaml:"tie_epsilon"`
	TiebreakRubric string                   `yaml:"tiebreak_rubric,omitempty"`
}

// AutoresearchMetricParser declares how to extract a numeric score from
// the metric command's output.
type AutoresearchMetricParser struct {
	Kind          string `yaml:"kind"` // "regex" | "json_path" | "last_float" | "exit_code_inverse"
	Pattern       string `yaml:"pattern,omitempty"`
	LowerIsBetter bool   `yaml:"lower_is_better"`
}

// AutoresearchSchedule controls when the coordinator and evolver run.
type AutoresearchSchedule struct {
	Nightly AutoresearchNightly `yaml:"nightly,omitempty"`
	Evolver AutoresearchEvolver `yaml:"evolver,omitempty"`
}

// AutoresearchNightly defines a recurring batch window.
type AutoresearchNightly struct {
	Enabled bool   `yaml:"enabled"`
	Window  string `yaml:"window"` // e.g. "23:00-06:00"
}

// AutoresearchEvolver defines the program.md auto-evolution cron.
type AutoresearchEvolver struct {
	Enabled bool   `yaml:"enabled"`
	Cron    string `yaml:"cron"` // e.g. "0 3 * * 0"
}

// AutoresearchTripwire configures the LLM judge that catches metric-hacking.
type AutoresearchTripwire struct {
	Model      string `yaml:"model,omitempty"`
	FailClosed bool   `yaml:"fail_closed,omitempty"` // documented for clarity; always treated as true
}

// AutoresearchBayes configures the per-class Beta-prior sampler.
type AutoresearchBayes struct {
	Classes    []string `yaml:"classes,omitempty"`
	PriorAlpha float64  `yaml:"prior_alpha,omitempty"`
	PriorBeta  float64  `yaml:"prior_beta,omitempty"`
}

// SLAConfig defines per-complexity story duration limits in minutes.
// Stories exceeding their limit emit STORY_SLA_BREACHED events.
//
// MaxMinutesPerComplexity accepts both bare integer keys (5: 60) and
// quoted string keys ("5": 60) in YAML — the latter is common when
// editing config files with YAML tooling that normalises all keys to strings.
type SLAConfig struct {
	MaxMinutesPerComplexity IntKeyMap `yaml:"max_minutes_per_complexity"`
	AutoEscalate            bool      `yaml:"auto_escalate"`
}

// IntKeyMap is a map[int]int that accepts both bare-integer and
// quoted-string keys when unmarshalled from YAML.
type IntKeyMap map[int]int

// UnmarshalYAML implements yaml.Unmarshaler so that both
//
//	5: 60          (bare integer key)
//	"5": 60        (quoted string key)
//
// are accepted without a cryptic "cannot unmarshal !!str" error.
func (m *IntKeyMap) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("sla.max_minutes_per_complexity: expected a mapping, got %v", value.Tag)
	}
	out := make(IntKeyMap, len(value.Content)/2)
	for i := 0; i+1 < len(value.Content); i += 2 {
		keyNode := value.Content[i]
		valNode := value.Content[i+1]

		k, err := strconv.Atoi(keyNode.Value)
		if err != nil {
			return fmt.Errorf("sla.max_minutes_per_complexity: key %q is not an integer: %w", keyNode.Value, err)
		}
		v, err := strconv.Atoi(valNode.Value)
		if err != nil {
			return fmt.Errorf("sla.max_minutes_per_complexity[%d]: value %q is not an integer: %w", k, valNode.Value, err)
		}
		out[k] = v
	}
	*m = out
	return nil
}

// BillingConfig controls cost estimation and client quoting.
type BillingConfig struct {
	DefaultRate   float64            `yaml:"default_rate"`
	Currency      string             `yaml:"currency"`
	HoursPerPoint map[int][2]float64 `yaml:"hours_per_point"`
	LLMCosts      LLMCostConfig      `yaml:"llm_costs"`
}

// LLMCostConfig tracks LLM API costs.
type LLMCostConfig struct {
	Mode  string               `yaml:"mode"`
	Rates map[string]TokenRate `yaml:"rates,omitempty"`
}

// TokenRate defines per-token pricing for a model.
type TokenRate struct {
	InputPer1K  float64 `yaml:"input_per_1k"`
	OutputPer1K float64 `yaml:"output_per_1k"`
}

// validBackends is the set of allowed workspace backends.
var validBackends = map[string]bool{
	"dolt":   true,
	"sqlite": true,
}

// validLogLevels is the set of allowed log levels.
var validLogLevels = map[string]bool{
	"debug": true,
	"info":  true,
	"warn":  true,
	"error": true,
}

// validWorktreePrune is the set of allowed worktree prune modes.
var validWorktreePrune = map[string]bool{
	"immediate": true,
	"deferred":  true,
}

// validLogArchive is the set of allowed log archive modes.
var validLogArchive = map[string]bool{
	"dolt": true,
	"file": true,
	"none": true,
}

var validDesignApproaches = map[string]bool{
	"":         true,
	"ddd-tdd":  true,
	"tdd":      true,
	"standard": true,
}

// DevDBConfig configures per-story ephemeral databases (planned 2026-05-21).
// Provider == "" or "null" disables the feature; agents do not get DBs.
type DevDBConfig struct {
	Provider  string             `yaml:"provider"`  // "ghost" | "docker" | "null"
	Template  string             `yaml:"template"`  // source DB name for forks
	OnFailure DevDBFailurePolicy `yaml:"on_failure"`
	Ghost     DevDBGhostConfig   `yaml:"ghost"`
	Docker    DevDBDockerConfig  `yaml:"docker"`
	// FunctionDenylistExtra appends operator-supplied function names to the
	// built-in side-effecting-function denylist enforced by `vxd db sql`
	// without --write (pg_terminate_backend, lo_import, pg_read_file, …).
	// Use it to block site-specific functions that write despite READ ONLY.
	FunctionDenylistExtra []string `yaml:"function_denylist_extra,omitempty"`
}

// DevDBFailurePolicy controls behaviour when a story finishes with an error.
type DevDBFailurePolicy struct {
	KeepDB      bool `yaml:"keep_db"`
	RetainHours int  `yaml:"retain_hours"` // default 24
}

// DevDBGhostConfig configures the Ghost (cloud) provider.
type DevDBGhostConfig struct {
	APIKeyEnv string `yaml:"api_key_env"` // default GHOST_API_KEY
	SpaceID   string `yaml:"space_id"`
}

// DevDBDockerConfig configures the Docker (local) provider.
type DevDBDockerConfig struct {
	Image          string `yaml:"image"`           // default postgres:16
	ContainerName  string `yaml:"container_name"`  // default vxd-devdb-pg16
	TemplateVolume string `yaml:"template_volume"` // default ~/.vxd/devdb-data
	Network        string `yaml:"network"`         // default vxd-devdb
	HostPortRange  string `yaml:"host_port_range"` // default 5500-5599
	Host           string `yaml:"host"`            // default "localhost"; override for Colima/VM setups (e.g. "192.168.64.3")
}

// Warnings returns non-fatal configuration advisories: settings that are
// accepted but do not do what an operator plausibly expects. Surfaced by
// `vxd config validate` and the qa_model preflight check.
func (c Config) Warnings() []string {
	var warnings []string

	// models.qa is inert: the QA stage runs lint/build/test commands
	// (engine/qa.go), not an LLM. An operator who explicitly binds the qa
	// role (any binding different from the shipped default) expects a
	// review pass that never happens — and cost estimates look wrong.
	def := DefaultConfig().Models.QA
	qa := c.Models.QA
	if qa.Provider != "" && (qa.Provider != def.Provider || qa.Model != def.Model) {
		warnings = append(warnings, fmt.Sprintf(
			"models.qa is bound to %s/%s, but the QA stage is command-based (lint/build/test) and never calls an LLM — this binding is inert. For an LLM review pass, bind models.reviewer instead (see README: Configuration).",
			qa.Provider, qa.Model))
	}

	return warnings
}

// Validate checks that all configuration values are within allowed ranges.
// It returns an error describing the first invalid value found.
func (c Config) Validate() error {
	if !validBackends[c.Workspace.Backend] {
		return fmt.Errorf("workspace.backend must be \"dolt\" or \"sqlite\", got %q", c.Workspace.Backend)
	}

	if !validLogLevels[c.Workspace.LogLevel] {
		return fmt.Errorf("workspace.log_level must be one of debug, info, warn, error; got %q", c.Workspace.LogLevel)
	}

	if !validWorktreePrune[c.Cleanup.WorktreePrune] {
		return fmt.Errorf("cleanup.worktree_prune must be \"immediate\" or \"deferred\", got %q", c.Cleanup.WorktreePrune)
	}

	if !validLogArchive[c.Cleanup.LogArchive] {
		return fmt.Errorf("cleanup.log_archive must be \"dolt\", \"file\", or \"none\"; got %q", c.Cleanup.LogArchive)
	}

	if c.Routing.MaxConcurrentAgents < 1 || c.Routing.MaxConcurrentAgents > 50 {
		return fmt.Errorf("routing.max_concurrent_agents must be between 1 and 50, got %d", c.Routing.MaxConcurrentAgents)
	}

	switch c.Secrets.Provider {
	case "", "env", "vault":
		// valid
	default:
		return fmt.Errorf("secrets.provider must be \"env\" or \"vault\", got %q", c.Secrets.Provider)
	}
	if c.Secrets.Provider == "vault" && c.Secrets.VaultAddr == "" {
		return fmt.Errorf("secrets.vault_addr is required when secrets.provider is \"vault\"")
	}

	for name, rc := range c.Runtimes {
		switch rc.Runner {
		case "", "tmux", "docker", "ssh":
			// valid
		default:
			return fmt.Errorf("runtimes.%s.runner must be \"tmux\", \"docker\", or \"ssh\", got %q", name, rc.Runner)
		}
		if rc.Runner == "docker" && rc.Docker.Image == "" {
			return fmt.Errorf("runtimes.%s.docker.image is required when runner is \"docker\"", name)
		}
		if rc.Runner == "ssh" && rc.SSH.Host == "" {
			return fmt.Errorf("runtimes.%s.ssh.host is required when runner is \"ssh\"", name)
		}
	}

	if c.Routing.JuniorMaxComplexity < 1 || c.Routing.JuniorMaxComplexity > 13 {
		return fmt.Errorf("routing.junior_max_complexity must be 1-13, got %d", c.Routing.JuniorMaxComplexity)
	}

	if c.Routing.IntermediateMaxComplexity < c.Routing.JuniorMaxComplexity {
		return fmt.Errorf(
			"routing.intermediate_max_complexity (%d) must be >= junior_max_complexity (%d)",
			c.Routing.IntermediateMaxComplexity, c.Routing.JuniorMaxComplexity,
		)
	}

	if c.Routing.IntermediateMaxComplexity > 13 {
		return fmt.Errorf("routing.intermediate_max_complexity must be <= 13, got %d", c.Routing.IntermediateMaxComplexity)
	}

	if !validDesignApproaches[c.Planning.DesignApproach] {
		return fmt.Errorf("planning.design_approach must be \"ddd-tdd\", \"tdd\", or \"standard\"; got %q", c.Planning.DesignApproach)
	}

	// Billing validation
	if c.Billing.DefaultRate < 0 {
		return fmt.Errorf("billing.default_rate must be >= 0, got %f", c.Billing.DefaultRate)
	}
	if c.Billing.Currency == "" {
		return fmt.Errorf("billing.currency must not be empty")
	}
	validLLMModes := map[string]bool{"subscription": true, "per_token": true}
	if !validLLMModes[c.Billing.LLMCosts.Mode] {
		return fmt.Errorf("billing.llm_costs.mode must be \"subscription\" or \"per_token\", got %q", c.Billing.LLMCosts.Mode)
	}
	for pts, hrs := range c.Billing.HoursPerPoint {
		if hrs[0] < 0 || hrs[1] < 0 {
			return fmt.Errorf("billing.hours_per_point[%d] values must be >= 0", pts)
		}
		if hrs[0] > hrs[1] {
			return fmt.Errorf("billing.hours_per_point[%d] low (%f) must be <= high (%f)", pts, hrs[0], hrs[1])
		}
	}

	validReviewModes := map[string]bool{"": true, "auto": true, "manual": true, "plan_only": true}
	if !validReviewModes[c.Merge.ReviewMode] {
		return fmt.Errorf("merge.review_mode must be \"auto\", \"manual\", or \"plan_only\"; got %q", c.Merge.ReviewMode)
	}

	// QA validation
	validCriterionKinds := map[string]bool{
		"output_contains": true, "output_not_contains": true,
		"file_exists": true, "file_contains": true,
		"file_not_empty": true, "exit_code_zero": true,
		"migration_succeeds": true, "schema_changed": true, "sql_query_returns": true,
	}
	for i, sc := range c.QA.SuccessCriteria {
		if !validCriterionKinds[sc.Kind] {
			return fmt.Errorf("qa.success_criteria[%d].kind must be one of output_contains, output_not_contains, file_exists, file_contains, file_not_empty, exit_code_zero, migration_succeeds, schema_changed, sql_query_returns; got %q", i, sc.Kind)
		}
		if sc.Command != "" && len(sc.CommandList) > 0 {
			return fmt.Errorf("qa.success_criteria[%d]: command and command_list are mutually exclusive", i)
		}
		// Fail fast at load time on commands the runtime would reject —
		// a copy-pasted hostile vxd.yaml should not survive `vxd config
		// validate`, let alone reach dispatch.
		if err := ValidateShellCommand(sc.Command, c.Security.StrictShellCommands); err != nil {
			return fmt.Errorf("qa.success_criteria[%d].command: %w", i, err)
		}
		for j, entry := range sc.CommandList {
			if err := ValidateShellCommand(entry, c.Security.StrictShellCommands); err != nil {
				return fmt.Errorf("qa.success_criteria[%d].command_list[%d]: %w", i, j, err)
			}
		}
	}
	if err := ValidateShellCommand(c.Autoresearch.Metric.Command, c.Security.StrictShellCommands); err != nil {
		return fmt.Errorf("autoresearch.metric.command: %w", err)
	}

	if err := c.Autoresearch.validate(); err != nil {
		return err
	}

	if err := validateDevDB(c.DevDB); err != nil {
		return err
	}

	return nil
}

// validateAutoresearch is exported via Config.Validate; only checks fields
// when autoresearch is enabled, keeping the feature fully opt-in.
func (a AutoresearchConfig) validate() error {
	if !a.Enabled {
		return nil
	}
	if a.Metric.Command == "" {
		return fmt.Errorf("autoresearch.metric.command is required when autoresearch.enabled is true")
	}
	validParserKinds := map[string]bool{
		"regex": true, "json_path": true, "last_float": true, "exit_code_inverse": true,
	}
	if !validParserKinds[a.Metric.Parser.Kind] {
		return fmt.Errorf("autoresearch.metric.parser.kind must be one of regex, json_path, last_float, exit_code_inverse; got %q", a.Metric.Parser.Kind)
	}
	if a.Metric.Parser.Kind == "regex" && a.Metric.Parser.Pattern == "" {
		return fmt.Errorf("autoresearch.metric.parser.pattern is required for regex parser")
	}
	if a.Metric.Parser.Kind == "json_path" && a.Metric.Parser.Pattern == "" {
		return fmt.Errorf("autoresearch.metric.parser.pattern is required for json_path parser")
	}
	if a.Metric.TieEpsilon < 0 {
		return fmt.Errorf("autoresearch.metric.tie_epsilon must be >= 0, got %v", a.Metric.TieEpsilon)
	}
	if len(a.EditablePaths) == 0 {
		return fmt.Errorf("autoresearch.editable_paths must contain at least one allowlisted glob")
	}
	validGates := map[string]bool{"auto": true, "winning": true, "pr": true}
	if !validGates[a.Gate] {
		return fmt.Errorf("autoresearch.gate must be \"auto\", \"winning\", or \"pr\"; got %q", a.Gate)
	}
	if a.Budget == "" {
		return fmt.Errorf("autoresearch.budget is required (e.g. \"5m\")")
	}
	if a.Parallel < 1 {
		return fmt.Errorf("autoresearch.parallel must be >= 1, got %d", a.Parallel)
	}
	if a.Bayes.PriorAlpha < 0 || a.Bayes.PriorBeta < 0 {
		return fmt.Errorf("autoresearch.bayes.prior_alpha and prior_beta must be >= 0")
	}
	return nil
}

func validateDevDB(c DevDBConfig) error {
	switch c.Provider {
	case "", "null":
		return nil
	case "ghost":
		if c.Template == "" {
			return fmt.Errorf("devdb.template required for ghost provider")
		}
		return nil
	case "docker":
		if c.Template == "" {
			return fmt.Errorf("devdb.template required for docker provider")
		}
		return nil
	default:
		return fmt.Errorf("devdb.provider must be ghost|docker|null, got %q", c.Provider)
	}
}
