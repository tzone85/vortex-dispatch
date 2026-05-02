// Package autoresearch implements a generic, sustainable, self-improving
// experiment harness inspired by karpathy/autoresearch. It runs the loop
//
//	hypothesize → edit → measure → keep/discard → iterate
//
// against any tracked repo, with pluggable metrics. See
// docs/superpowers/specs/2026-05-02-autoresearch-harness-design.md.
package autoresearch

import "time"

// ExperimentClass categorizes a proposed change. The BayesSampler maintains
// per-class Beta priors to learn which classes work in a given repo.
type ExperimentClass string

const (
	ClassRefactor ExperimentClass = "refactor"
	ClassPerf     ExperimentClass = "perf"
	ClassTest     ExperimentClass = "test"
	ClassSimplify ExperimentClass = "simplify"
	ClassFeature  ExperimentClass = "feature"
	ClassOther    ExperimentClass = "other"
)

// DefaultClasses is the canonical list used when none is configured.
var DefaultClasses = []ExperimentClass{
	ClassRefactor, ClassPerf, ClassTest, ClassSimplify, ClassFeature, ClassOther,
}

// Verdict is the tripwire judge's output.
type Verdict string

const (
	VerdictOK         Verdict = "OK"
	VerdictSuspicious Verdict = "SUSPICIOUS"
	VerdictRejected   Verdict = "REJECTED"
)

// Proposal describes the experiment a coordinator decided to run, before
// the agent has touched any code.
type Proposal struct {
	ID                string          `json:"id"`
	Repo              string          `json:"repo"`
	Class             ExperimentClass `json:"class"`
	Prompt            string          `json:"-"` // not stored in events; full prompt is large
	PromptHash        string          `json:"prompt_hash"`
	ParentWinHashes   []string        `json:"parent_win_hashes,omitempty"`
	ParentLossHashes  []string        `json:"parent_loss_hashes,omitempty"`
}

// Score is the parsed-and-tiebroken output of MetricHarness.
type Score struct {
	Primary       float64 `json:"primary"`        // raw parsed value
	TiebreakNudge float64 `json:"tiebreak_nudge"` // bounded in [-tie_epsilon, +tie_epsilon]
	Final         float64 `json:"final"`          // primary + nudge
	LowerIsBetter bool    `json:"lower_is_better"`
	RawOutput     string  `json:"-"`
}

// Improves reports whether s improves on baseline given the metric direction.
func (s Score) Improves(baseline float64) bool {
	if s.LowerIsBetter {
		return s.Final < baseline
	}
	return s.Final > baseline
}

// Outcome is the full record of an experiment after the runner has finished.
type Outcome struct {
	Proposal     Proposal      `json:"proposal"`
	DiffHash     string        `json:"diff_hash"`
	Diff         string        `json:"-"` // never stored in events; only for tripwire/gate
	Score        Score         `json:"score"`
	Baseline     float64       `json:"baseline"`
	Delta        float64       `json:"delta"` // signed; sign convention: positive = improvement
	Verdict      Verdict       `json:"verdict"`
	VerdictNote  string        `json:"verdict_note,omitempty"`
	Kept         bool          `json:"kept"`
	GateAction   string        `json:"gate_action,omitempty"`   // "auto" | "winning" | "pr"
	BranchOrPR   string        `json:"branch_or_pr,omitempty"`  // branch name or PR url
	Worktree     string        `json:"worktree"`
	Started      time.Time     `json:"started"`
	Finished     time.Time     `json:"finished"`
	Duration     time.Duration `json:"duration"`
	InfraCaused  bool          `json:"infra_caused,omitempty"`  // for failed outcomes; skip Bayes if true
	FailReason   string        `json:"fail_reason,omitempty"`
}

// Experiment is the projection-shaped record returned by HypothesisBank.
// It is the smaller, read-side counterpart of Outcome — what we keep around
// to seed future prompts and compute statistics.
type Experiment struct {
	ID         string          `json:"id"`
	Repo       string          `json:"repo"`
	Class      ExperimentClass `json:"class"`
	DiffHash   string          `json:"diff_hash"`
	Score      float64         `json:"score"`
	Baseline   float64         `json:"baseline"`
	Delta      float64         `json:"delta"` // signed; positive = improvement
	Verdict    Verdict         `json:"verdict"`
	Kept       bool            `json:"kept"`
	FailReason string          `json:"fail_reason,omitempty"`
	Timestamp  time.Time       `json:"timestamp"`
}

// IsAgentCausedFailure decides whether a failure should update Bayes priors.
// Infra failures (worktree create, tmux start, etc.) are excluded so we don't
// poison the per-class posterior with environmental noise.
//
// Centralized so the rule lives in exactly one place — every failure path
// (runner, coordinator, recovery) consults this function.
func IsAgentCausedFailure(reason string, infraCaused bool) bool {
	if infraCaused {
		return false
	}
	switch reason {
	case "infra", "worktree_create", "tmux_start", "provider_outage":
		return false
	}
	return true
}
