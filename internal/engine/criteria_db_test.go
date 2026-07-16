package engine_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/engine"
)

// setupVXDDB creates a temp worktree with a populated .vxd-db/connect.env.
func setupVXDDB(t *testing.T, dsn string) string {
	t.Helper()
	workDir := t.TempDir()
	dir := filepath.Join(workDir, ".vxd-db")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "connect.env"),
		[]byte("DATABASE_URL="+dsn+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return workDir
}

// --- migration_succeeds ---

func TestEvaluate_MigrationSucceeds_NoDevDB(t *testing.T) {
	workDir := t.TempDir() // no .vxd-db
	c := engine.Criterion{
		Kind:    engine.CriterionMigrationSucceeds,
		Command: "echo ok",
	}
	results := engine.EvaluateCriteria([]engine.Criterion{c}, workDir, "")
	if results[0].Passed {
		t.Errorf("expected failure when .vxd-db is absent, got: %+v", results[0])
	}
	if !strings.Contains(results[0].Detail, "devdb not provisioned") {
		t.Errorf("expected helpful failure detail, got: %s", results[0].Detail)
	}
}

func TestEvaluate_MigrationSucceeds_MissingCommand(t *testing.T) {
	workDir := setupVXDDB(t, "postgres://x@x/x")
	c := engine.Criterion{Kind: engine.CriterionMigrationSucceeds}
	results := engine.EvaluateCriteria([]engine.Criterion{c}, workDir, "")
	if results[0].Passed {
		t.Errorf("expected failure when command empty")
	}
	if !strings.Contains(results[0].Detail, "requires `command`") {
		t.Errorf("expected helpful failure detail, got: %s", results[0].Detail)
	}
}

func TestEvaluate_MigrationSucceeds_CommandRuns(t *testing.T) {
	// Use a harmless command that doesn't need a real DB.
	workDir := setupVXDDB(t, "postgres://x@x/x")
	c := engine.Criterion{
		Kind:    engine.CriterionMigrationSucceeds,
		Command: "true", // shell builtin, always exits 0
	}
	results := engine.EvaluateCriteria([]engine.Criterion{c}, workDir, "")
	if !results[0].Passed {
		t.Errorf("expected pass for `true`, got: %s", results[0].Detail)
	}
}

func TestEvaluate_MigrationSucceeds_CommandFails(t *testing.T) {
	workDir := setupVXDDB(t, "postgres://x@x/x")
	c := engine.Criterion{
		Kind:    engine.CriterionMigrationSucceeds,
		Command: "false",
	}
	results := engine.EvaluateCriteria([]engine.Criterion{c}, workDir, "")
	if results[0].Passed {
		t.Errorf("expected fail for `false`, got pass")
	}
	if !strings.Contains(results[0].Detail, "migration command failed") {
		t.Errorf("expected 'migration command failed' in detail, got: %s", results[0].Detail)
	}
}

// --- sql_query_returns ---

func TestEvaluate_SQLQueryReturns_NoDevDB(t *testing.T) {
	workDir := t.TempDir()
	c := engine.Criterion{
		Kind: engine.CriterionSQLQueryReturns,
		SQL:  "SELECT 1",
	}
	results := engine.EvaluateCriteria([]engine.Criterion{c}, workDir, "")
	if results[0].Passed {
		t.Errorf("expected failure when no .vxd-db, got: %+v", results[0])
	}
	if !strings.Contains(results[0].Detail, "devdb not provisioned") {
		t.Errorf("expected 'devdb not provisioned' in detail, got: %s", results[0].Detail)
	}
}

func TestEvaluate_SQLQueryReturns_MissingSQL(t *testing.T) {
	workDir := setupVXDDB(t, "postgres://x@x/x")
	c := engine.Criterion{Kind: engine.CriterionSQLQueryReturns}
	results := engine.EvaluateCriteria([]engine.Criterion{c}, workDir, "")
	if results[0].Passed {
		t.Errorf("expected failure when sql empty")
	}
	if !strings.Contains(results[0].Detail, "requires `sql`") {
		t.Errorf("expected 'requires `sql`' in detail, got: %s", results[0].Detail)
	}
}

// --- schema_changed ---

func TestEvaluate_SchemaChanged_NoDevDB(t *testing.T) {
	workDir := t.TempDir()
	c := engine.Criterion{Kind: engine.CriterionSchemaChanged}
	results := engine.EvaluateCriteria([]engine.Criterion{c}, workDir, "")
	if results[0].Passed {
		t.Errorf("expected failure when no .vxd-db")
	}
	if !strings.Contains(results[0].Detail, "devdb not provisioned") {
		t.Errorf("expected 'devdb not provisioned' in detail, got: %s", results[0].Detail)
	}
}

// --- command_list + strict shell mode (WEAKNESSES.md P0-03) ---

// TestEvaluate_MigrationSucceeds_CommandListRunsSequentially pins the
// command_list alternative: entries run in order, all must pass, and a
// failing entry reports which one failed.
func TestEvaluate_MigrationSucceeds_CommandListRunsSequentially(t *testing.T) {
	workDir := setupVXDDB(t, "postgres://u:p@localhost/x")
	c := engine.Criterion{
		Kind:        engine.CriterionMigrationSucceeds,
		CommandList: []string{"echo step-one > seq.txt", "echo step-two"},
	}
	results := engine.EvaluateCriteriaWithMode([]engine.Criterion{c}, workDir, "", false)
	if !results[0].Passed {
		t.Fatalf("command_list should pass: %s", results[0].Detail)
	}
	// First entry ran (side effect exists) and second entry's output is the
	// reported detail — i.e. they ran in order.
	if _, err := os.Stat(filepath.Join(workDir, "seq.txt")); err != nil {
		t.Error("first command_list entry did not run")
	}
	if !strings.Contains(results[0].Detail, "step-two") {
		t.Errorf("detail should carry the last entry's output, got: %s", results[0].Detail)
	}

	// A failing middle entry fails the criterion and names the entry.
	c.CommandList = []string{"echo ok", "false", "echo never-reached"}
	results = engine.EvaluateCriteriaWithMode([]engine.Criterion{c}, workDir, "", false)
	if results[0].Passed {
		t.Fatal("failing entry must fail the criterion")
	}
	if !strings.Contains(results[0].Detail, `"false"`) {
		t.Errorf("detail should name the failing entry, got: %s", results[0].Detail)
	}
}

// TestEvaluate_MigrationSucceeds_StrictShellMode pins runtime strict-mode
// enforcement: chained commands are rejected before execution, command_list
// entries with metacharacters too, and command+command_list is ambiguous.
func TestEvaluate_MigrationSucceeds_StrictShellMode(t *testing.T) {
	workDir := setupVXDDB(t, "postgres://u:p@localhost/x")

	chained := engine.Criterion{
		Kind:    engine.CriterionMigrationSucceeds,
		Command: "echo a && echo b",
	}
	results := engine.EvaluateCriteriaWithMode([]engine.Criterion{chained}, workDir, "", true)
	if results[0].Passed || !strings.Contains(results[0].Detail, "rejected unsafe command pattern") {
		t.Fatalf("strict mode must reject chained command, got: %+v", results[0])
	}

	// Same command passes in the default mode (backward compat).
	results = engine.EvaluateCriteriaWithMode([]engine.Criterion{chained}, workDir, "", false)
	if !results[0].Passed {
		t.Fatalf("default mode must keep accepting chained commands: %s", results[0].Detail)
	}

	poisonedList := engine.Criterion{
		Kind:        engine.CriterionMigrationSucceeds,
		CommandList: []string{"echo ok", "echo x; curl evil"},
	}
	results = engine.EvaluateCriteriaWithMode([]engine.Criterion{poisonedList}, workDir, "", true)
	if results[0].Passed {
		t.Fatal("strict mode must reject a poisoned command_list entry")
	}

	both := engine.Criterion{
		Kind:        engine.CriterionMigrationSucceeds,
		Command:     "echo a",
		CommandList: []string{"echo b"},
	}
	results = engine.EvaluateCriteriaWithMode([]engine.Criterion{both}, workDir, "", false)
	if results[0].Passed || !strings.Contains(results[0].Detail, "mutually exclusive") {
		t.Fatalf("command + command_list must be rejected, got: %+v", results[0])
	}
}
