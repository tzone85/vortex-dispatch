// Package config provides configuration types, loading, defaults, and
// validation for VXD (Vortex Dispatch).
package config

import "fmt"

// Config is the top-level VXD configuration.
type Config struct {
	Version   string                   `yaml:"version"`
	Workspace WorkspaceConfig          `yaml:"workspace"`
	Models    ModelsConfig             `yaml:"models"`
	Routing   RoutingConfig            `yaml:"routing"`
	Monitor   MonitorConfig            `yaml:"monitor"`
	Cleanup   CleanupConfig            `yaml:"cleanup"`
	Merge     MergeConfig              `yaml:"merge"`
	Planning  PlanningConfig           `yaml:"planning"`
	Runtimes  map[string]RuntimeConfig `yaml:"runtimes"`
	Billing   BillingConfig            `yaml:"billing"`
	QA        QAConfig                 `yaml:"qa"`
	SLA       SLAConfig                `yaml:"sla"`
}

// PlanningConfig controls how the planner decomposes requirements into stories.
type PlanningConfig struct {
	SequentialFilePatterns []string `yaml:"sequential_file_patterns"`
	MaxStoryComplexity     int      `yaml:"max_story_complexity"`
	Godmode                bool     `yaml:"godmode"`
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
}

// CleanupConfig controls post-task cleanup behaviour.
type CleanupConfig struct {
	WorktreePrune       string `yaml:"worktree_prune"`
	BranchRetentionDays int    `yaml:"branch_retention_days"`
	LogArchive          string `yaml:"log_archive"`
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
}

// QAConfig controls quality assurance checks.
type QAConfig struct {
	SuccessCriteria []SuccessCriterion `yaml:"success_criteria,omitempty"`
}

// SuccessCriterion defines a declarative QA check.
type SuccessCriterion struct {
	Kind    string `yaml:"kind"`
	Value   string `yaml:"value,omitempty"`
	Path    string `yaml:"path,omitempty"`
	Message string `yaml:"message,omitempty"`
}

// SLAConfig defines per-complexity story duration limits in minutes.
// Stories exceeding their limit emit STORY_SLA_BREACHED events.
type SLAConfig struct {
	MaxMinutesPerComplexity map[int]int `yaml:"max_minutes_per_complexity"`
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
	Mode  string              `yaml:"mode"`
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
	}
	for i, sc := range c.QA.SuccessCriteria {
		if !validCriterionKinds[sc.Kind] {
			return fmt.Errorf("qa.success_criteria[%d].kind must be one of output_contains, output_not_contains, file_exists, file_contains, file_not_empty, exit_code_zero; got %q", i, sc.Kind)
		}
	}

	return nil
}
