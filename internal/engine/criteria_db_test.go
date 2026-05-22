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
