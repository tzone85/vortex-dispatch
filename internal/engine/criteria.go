package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CriterionKind represents a type of success criterion.
type CriterionKind string

const (
	CriterionOutputContains    CriterionKind = "output_contains"
	CriterionOutputNotContains CriterionKind = "output_not_contains"
	CriterionFileExists        CriterionKind = "file_exists"
	CriterionFileContains      CriterionKind = "file_contains"
	CriterionFileNotEmpty      CriterionKind = "file_not_empty"
	CriterionExitCodeZero      CriterionKind = "exit_code_zero"
)

// Criterion is a single declarative success check.
type Criterion struct {
	Kind    CriterionKind `yaml:"kind" json:"kind"`
	Value   string        `yaml:"value,omitempty" json:"value,omitempty"`     // for contains checks
	Path    string        `yaml:"path,omitempty" json:"path,omitempty"`       // for file checks
	Message string        `yaml:"message,omitempty" json:"message,omitempty"` // custom failure message
}

// CriterionResult holds the evaluation outcome of a single criterion.
type CriterionResult struct {
	Criterion Criterion
	Passed    bool
	Detail    string
}

// EvaluateCriteria checks all criteria against the given context.
// workDir is the worktree path, output is the combined agent output.
func EvaluateCriteria(criteria []Criterion, workDir, output string) []CriterionResult {
	results := make([]CriterionResult, 0, len(criteria))
	for _, c := range criteria {
		results = append(results, evaluateOne(c, workDir, output))
	}
	return results
}

// AllPassed returns true if every criterion in the results passed.
func AllPassed(results []CriterionResult) bool {
	for _, r := range results {
		if !r.Passed {
			return false
		}
	}
	return true
}

// CriteriaFailureSummary returns a formatted string of all failed criteria.
func CriteriaFailureSummary(results []CriterionResult) string {
	var parts []string
	for _, r := range results {
		if !r.Passed {
			msg := r.Detail
			if r.Criterion.Message != "" {
				msg = r.Criterion.Message
			}
			parts = append(parts, fmt.Sprintf("[CRITERION %s FAILED] %s", r.Criterion.Kind, msg))
		}
	}
	return strings.Join(parts, "\n")
}

func evaluateOne(c Criterion, workDir, output string) CriterionResult {
	switch c.Kind {
	case CriterionOutputContains:
		passed := strings.Contains(output, c.Value)
		detail := fmt.Sprintf("output contains %q", c.Value)
		if !passed {
			detail = fmt.Sprintf("output does not contain %q", c.Value)
		}
		return CriterionResult{Criterion: c, Passed: passed, Detail: detail}

	case CriterionOutputNotContains:
		passed := !strings.Contains(output, c.Value)
		detail := fmt.Sprintf("output does not contain %q", c.Value)
		if !passed {
			detail = fmt.Sprintf("output contains forbidden %q", c.Value)
		}
		return CriterionResult{Criterion: c, Passed: passed, Detail: detail}

	case CriterionFileExists:
		path := resolvePath(workDir, c.Path)
		_, err := os.Stat(path)
		passed := err == nil
		detail := fmt.Sprintf("file %s exists", c.Path)
		if !passed {
			detail = fmt.Sprintf("file %s not found", c.Path)
		}
		return CriterionResult{Criterion: c, Passed: passed, Detail: detail}

	case CriterionFileContains:
		path := resolvePath(workDir, c.Path)
		data, err := os.ReadFile(path)
		if err != nil {
			return CriterionResult{
				Criterion: c,
				Passed:    false,
				Detail:    fmt.Sprintf("cannot read %s: %v", c.Path, err),
			}
		}
		passed := strings.Contains(string(data), c.Value)
		detail := fmt.Sprintf("file %s contains %q", c.Path, c.Value)
		if !passed {
			detail = fmt.Sprintf("file %s does not contain %q", c.Path, c.Value)
		}
		return CriterionResult{Criterion: c, Passed: passed, Detail: detail}

	case CriterionFileNotEmpty:
		path := resolvePath(workDir, c.Path)
		fi, err := os.Stat(path)
		if err != nil {
			return CriterionResult{
				Criterion: c,
				Passed:    false,
				Detail:    fmt.Sprintf("file %s not found", c.Path),
			}
		}
		passed := fi.Size() > 0
		detail := fmt.Sprintf("file %s is not empty (%d bytes)", c.Path, fi.Size())
		if !passed {
			detail = fmt.Sprintf("file %s is empty", c.Path)
		}
		return CriterionResult{Criterion: c, Passed: passed, Detail: detail}

	case CriterionExitCodeZero:
		// This is evaluated by the caller based on command exit code.
		// The criterion is a marker; actual evaluation happens in QA.
		return CriterionResult{
			Criterion: c,
			Passed:    true,
			Detail:    "exit code check delegated to QA runner",
		}

	default:
		return CriterionResult{
			Criterion: c,
			Passed:    false,
			Detail:    fmt.Sprintf("unknown criterion kind: %s", c.Kind),
		}
	}
}

func resolvePath(workDir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(workDir, path)
}
