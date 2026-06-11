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

	// SP5 — DB-touching criteria.
	CriterionMigrationSucceeds CriterionKind = "migration_succeeds"
	CriterionSchemaChanged     CriterionKind = "schema_changed"
	CriterionSQLQueryReturns   CriterionKind = "sql_query_returns"
)

// Criterion is a single declarative success check.
type Criterion struct {
	Kind    CriterionKind `yaml:"kind" json:"kind"`
	Value   string        `yaml:"value,omitempty" json:"value,omitempty"`     // for contains checks
	Path    string        `yaml:"path,omitempty" json:"path,omitempty"`       // for file checks
	Message string        `yaml:"message,omitempty" json:"message,omitempty"` // custom failure message

	// SP5 additions — DB-touching criteria.
	Command        string `yaml:"command,omitempty" json:"command,omitempty"`                     // migration_succeeds: shell command to run
	SQL            string `yaml:"sql,omitempty" json:"sql,omitempty"`                             // sql_query_returns: query to execute
	ExpectedRows   *int   `yaml:"expected_rows,omitempty" json:"expected_rows,omitempty"`          // sql_query_returns: optional exact row count
	SchemaBaseline string `yaml:"schema_baseline,omitempty" json:"schema_baseline,omitempty"`     // schema_changed: path to baseline file
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
		path, perr := resolvePath(workDir, c.Path)
		if perr != nil {
			return CriterionResult{Criterion: c, Passed: false, Detail: perr.Error()}
		}
		_, err := os.Stat(path)
		passed := err == nil
		detail := fmt.Sprintf("file %s exists", c.Path)
		if !passed {
			detail = fmt.Sprintf("file %s not found", c.Path)
		}
		return CriterionResult{Criterion: c, Passed: passed, Detail: detail}

	case CriterionFileContains:
		path, perr := resolvePath(workDir, c.Path)
		if perr != nil {
			return CriterionResult{Criterion: c, Passed: false, Detail: perr.Error()}
		}
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
		path, perr := resolvePath(workDir, c.Path)
		if perr != nil {
			return CriterionResult{Criterion: c, Passed: false, Detail: perr.Error()}
		}
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

	case CriterionMigrationSucceeds:
		return evaluateMigrationSucceeds(c, workDir)

	case CriterionSchemaChanged:
		return evaluateSchemaChanged(c, workDir)

	case CriterionSQLQueryReturns:
		return evaluateSQLQueryReturns(c, workDir)

	default:
		return CriterionResult{
			Criterion: c,
			Passed:    false,
			Detail:    fmt.Sprintf("unknown criterion kind: %s", c.Kind),
		}
	}
}

// resolvePath joins a criterion path against the story worktree and
// REFUSES to escape that worktree. The threat model is a malicious or
// misconfigured `vxd.yaml` whose `path:` reads `/etc/shadow` or
// `../../../home/op/.ssh/id_ed25519` — the criterion's Detail field
// flows back through the WebSocket `command_result` response and would
// expose host file contents to anyone with dashboard read access.
//
// Rules:
//   - Absolute paths are rejected outright.
//   - After joining with workDir, the cleaned result must remain a
//     descendant of the cleaned workDir.
//   - Both checks operate on cleaned forms so `foo/../../etc/shadow`
//     collapses to `/etc/shadow` (rejected) instead of being silently
//     normalised into the worktree by the OS at open time.
func resolvePath(workDir, p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("criterion path is empty")
	}
	if filepath.IsAbs(p) {
		return "", fmt.Errorf("criterion path %q is absolute; must be relative to the story worktree", p)
	}
	cleanWork, err := filepath.Abs(workDir)
	if err != nil {
		return "", fmt.Errorf("resolve workdir %q: %w", workDir, err)
	}
	joined := filepath.Join(cleanWork, p)
	clean := filepath.Clean(joined)
	// `cleanWork` is already cleaned and absolute. clean must equal it
	// (the workdir itself) or sit underneath as <workdir>/something.
	if clean != cleanWork && !strings.HasPrefix(clean, cleanWork+string(os.PathSeparator)) {
		return "", fmt.Errorf("criterion path %q escapes the story worktree", p)
	}
	return clean, nil
}
