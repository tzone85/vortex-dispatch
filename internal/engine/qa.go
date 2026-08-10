package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

// QACheckResult holds the outcome of a single QA check (lint, build, or test).
type QACheckResult struct {
	Name    string
	Passed  bool
	Output  string
	Elapsed time.Duration
}

// QAResult holds the aggregate outcome of all QA checks for a story.
type QAResult struct {
	Passed bool
	Checks []QACheckResult
}

// FailureSummary returns a structured description of what failed and why,
// suitable for feeding back to an agent on retry. Returns empty string if
// all checks passed.
func (r QAResult) FailureSummary() string {
	if r.Passed {
		return ""
	}
	var parts []string
	for _, c := range r.Checks {
		if !c.Passed {
			output := c.Output
			// Truncate long output but keep enough for the agent to understand
			if len(output) > 500 {
				output = output[:500] + "\n... (truncated)"
			}
			parts = append(parts, fmt.Sprintf("[%s FAILED]\n%s", strings.ToUpper(c.Name), strings.TrimSpace(output)))
		}
	}
	if len(parts) == 0 {
		return "QA checks failed (no details available)"
	}
	return strings.Join(parts, "\n\n")
}

// CommandRunner abstracts command execution for testability.
type CommandRunner interface {
	Run(ctx context.Context, workDir, name string, args ...string) (string, error)
}

// ExecRunner executes commands via os/exec.
type ExecRunner struct{}

// Run executes the given command in the specified working directory and
// returns the combined stdout/stderr output.
func (e *ExecRunner) Run(ctx context.Context, workDir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...) // nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command -- QA commands come from operator vxd.yaml, gated by ValidateConfigShellCommand
	cmd.Dir = workDir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// QAConfig describes the commands to run for each QA check.
type QAConfig struct {
	LintCommand     string
	BuildCommand    string
	TestCommand     string
	SuccessCriteria []Criterion
	// StrictShellCommands mirrors security.strict_shell_commands: command-
	// bearing success criteria reject shell chaining/redirection
	// metacharacters and must use command_list for multi-step work.
	StrictShellCommands bool
}

// QA runs lint, build, and test commands against a worktree directory and
// emits pass/fail events.
type QA struct {
	config     QAConfig
	runner     CommandRunner
	eventStore state.EventStore
	projStore  state.ProjectionStore
}

// NewQA creates a QA instance with the given configuration, command runner,
// event store, and projection store.
func NewQA(cfg QAConfig, runner CommandRunner, es state.EventStore, ps state.ProjectionStore) *QA {
	return &QA{
		config:     cfg,
		runner:     runner,
		eventStore: es,
		projStore:  ps,
	}
}

// Run executes all QA checks (lint, build, test) in the given worktree
// directory for the specified story. It emits STORY_QA_STARTED, then either
// STORY_QA_PASSED or STORY_QA_FAILED.
func (q *QA) Run(ctx context.Context, storyID, worktreePath string) (QAResult, error) {
	// Emit QA started
	startEvt := state.NewEvent(state.EventStoryQAStarted, "qa", storyID, map[string]any{
		"worktree_path": worktreePath,
	})
	if err := q.eventStore.Append(startEvt); err != nil {
		return QAResult{}, fmt.Errorf("emit qa started: %w", err)
	}
	if err := q.projStore.Project(startEvt); err != nil {
		return QAResult{}, fmt.Errorf("project qa started: %w", err)
	}

	result := q.RunCommandChecks(ctx, worktreePath)

	// Evaluate declarative success criteria (if configured).
	if len(q.config.SuccessCriteria) > 0 {
		// Read agent output from log file if available.
		agentOutput := ""
		logPath := filepath.Join(worktreePath, "..", "logs", storyID+".log")
		if data, err := os.ReadFile(logPath); err == nil {
			agentOutput = string(data)
		}

		criteriaResults := EvaluateCriteriaWithMode(q.config.SuccessCriteria, worktreePath, agentOutput, q.config.StrictShellCommands)
		for _, cr := range criteriaResults {
			result.Checks = append(result.Checks, QACheckResult{
				Name:   fmt.Sprintf("criterion:%s", cr.Criterion.Kind),
				Passed: cr.Passed,
				Output: cr.Detail,
			})
			if !cr.Passed {
				result.Passed = false
			}
		}
	}

	// Emit result event
	eventType := state.EventStoryQAPassed
	if !result.Passed {
		eventType = state.EventStoryQAFailed
	}

	failedChecks := make([]string, 0)
	for _, c := range result.Checks {
		if !c.Passed {
			failedChecks = append(failedChecks, c.Name)
		}
	}

	resultEvt := state.NewEvent(eventType, "qa", storyID, map[string]any{
		"passed":        result.Passed,
		"total_checks":  len(result.Checks),
		"failed_checks": failedChecks,
		"quality_score": computeQualityScore(result),
		"checks_passed": len(result.Checks) - len(failedChecks),
		"duration_s":    totalDuration(result),
	})
	if err := q.eventStore.Append(resultEvt); err != nil {
		return result, fmt.Errorf("emit qa result: %w", err)
	}
	if err := q.projStore.Project(resultEvt); err != nil {
		return result, fmt.Errorf("project qa result: %w", err)
	}

	return result, nil
}

// RunCommandChecks runs the configured lint/build/test commands against a
// worktree and returns the aggregate result WITHOUT emitting any events. It is
// the pure command-execution core shared by Run (story QA) and the pre-merge
// integration gate (verifyRebasedQA), which re-runs these checks on the
// rebased worktree to keep the base branch green.
func (q *QA) RunCommandChecks(ctx context.Context, worktreePath string) QAResult {
	checks := []struct {
		name    string
		command string
	}{
		{"lint", q.config.LintCommand},
		{"build", q.config.BuildCommand},
		{"test", q.config.TestCommand},
	}

	result := QAResult{Passed: true}
	for _, check := range checks {
		if check.command == "" {
			continue
		}
		checkResult := q.runCheck(ctx, worktreePath, check.name, check.command)
		result.Checks = append(result.Checks, checkResult)
		if !checkResult.Passed {
			result.Passed = false
		}
	}
	return result
}

// preMergeDecision decides whether a story's merge must be blocked because its
// rebased worktree fails the repo-wide checks. The rule is deadlock-free: only
// block when the story turns a GREEN base RED. If the base branch is already
// failing THOSE checks, the breakage is not attributable to this story, so it
// is allowed through (blocking would deadlock every story behind a pre-existing
// main failure).
//
// Attribution is PER CHECK, not on the aggregate QAResult.Passed. A base that
// is red on one check (e.g. perpetual lint debt) must not mask a regression the
// story introduces in a DIFFERENT check: comparing only base.Passed would let a
// build/test regression merge whenever the base was red on anything at all,
// turning the gate into a silent no-op for the whole requirement.
func preMergeDecision(rebased, base QAResult) (block bool, reason string) {
	if rebased.Passed {
		return false, ""
	}
	// A fully-green base ⇒ every red check on the rebased worktree is this
	// story's regression. (Handles the common case and base results that carry
	// no per-check breakdown.)
	if base.Passed {
		return true, "pre-merge verify: story turns a green base red: " + rebased.FailureSummary()
	}
	// Partially-red base: block only checks that are GREEN on the base but RED
	// on the rebased worktree. base and rebased are produced by the same
	// RunCommandChecks config, so check names line up.
	baseRed := make(map[string]bool, len(base.Checks))
	for _, c := range base.Checks {
		if !c.Passed {
			baseRed[c.Name] = true
		}
	}
	var regressed []string
	for _, c := range rebased.Checks {
		if !c.Passed && !baseRed[c.Name] {
			regressed = append(regressed, c.Name)
		}
	}
	if len(regressed) == 0 {
		return false, "base branch already failing these checks — not attributable to this story"
	}
	return true, "pre-merge verify: story turns a green base red (" + strings.Join(regressed, ", ") + "): " + rebased.FailureSummary()
}

// computeQualityScore derives a 1-5 quality rating from QA results.
// A score of 5 means all checks passed; 1 means the overall QA failed.
func computeQualityScore(result QAResult) int {
	if !result.Passed {
		return 1
	}
	total := len(result.Checks)
	if total == 0 {
		return 3
	}
	passed := 0
	for _, c := range result.Checks {
		if c.Passed {
			passed++
		}
	}
	ratio := float64(passed) / float64(total)
	switch {
	case ratio >= 1.0:
		return 5
	case ratio >= 0.8:
		return 4
	case ratio >= 0.6:
		return 3
	case ratio >= 0.4:
		return 2
	default:
		return 1
	}
}

// totalDuration sums the elapsed time of all QA checks and returns seconds.
func totalDuration(result QAResult) int {
	var total time.Duration
	for _, c := range result.Checks {
		total += c.Elapsed
	}
	return int(total.Seconds())
}

// runCheck executes a single QA command and returns the result.
func (q *QA) runCheck(ctx context.Context, workDir, name, command string) QACheckResult {
	command = strings.TrimSpace(command)
	if command == "" {
		return QACheckResult{Name: name, Passed: false, Output: "empty command"}
	}

	start := time.Now()
	cmdName := ""
	var args []string
	if needsShell(command) {
		cmdName = "sh"
		args = []string{"-c", command}
	} else {
		parts := strings.Fields(command)
		cmdName = parts[0]
		args = parts[1:]
	}
	output, err := q.runner.Run(ctx, workDir, cmdName, args...)
	elapsed := time.Since(start)

	return QACheckResult{
		Name:    name,
		Passed:  err == nil,
		Output:  output,
		Elapsed: elapsed,
	}
}

func needsShell(command string) bool {
	return strings.ContainsAny(command, "|&;<>()$`*?[]{}~") ||
		strings.Contains(command, "&&") ||
		strings.Contains(command, "||") ||
		strings.Contains(command, "'") ||
		strings.Contains(command, "\"") ||
		strings.Contains(command, "=")
}
