package engine

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tzone85/vortex-dispatch/internal/shellexec"
	"github.com/tzone85/vortex-dispatch/internal/sqlsafety"
)

// readDatabaseURL returns the DATABASE_URL value from .vxd-db/connect.env in workDir,
// or empty string if the file is missing or the var isn't present.
func readDatabaseURL(workDir string) string {
	f, err := os.Open(filepath.Join(workDir, ".vxd-db", "connect.env"))
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "DATABASE_URL=") {
			return strings.TrimPrefix(line, "DATABASE_URL=")
		}
	}
	return ""
}

// evaluateMigrationSucceeds runs the configured command in workDir with
// DATABASE_URL set from .vxd-db/connect.env. Passes if exit code is zero.
func evaluateMigrationSucceeds(c Criterion, workDir string) CriterionResult {
	if c.Command == "" {
		return CriterionResult{Criterion: c, Passed: false,
			Detail: "migration_succeeds requires `command` field"}
	}
	if err := ValidateConfigShellCommand(c.Command); err != nil {
		return CriterionResult{Criterion: c, Passed: false,
			Detail: fmt.Sprintf("rejected unsafe command pattern: %v", err)}
	}
	dsn := readDatabaseURL(workDir)
	if dsn == "" {
		return CriterionResult{Criterion: c, Passed: false,
			Detail: "no .vxd-db/connect.env in worktree — devdb not provisioned for this story"}
	}
	cmd := shellexec.Command(c.Command)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), "DATABASE_URL="+dsn)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return CriterionResult{Criterion: c, Passed: false,
			Detail: fmt.Sprintf("migration command failed: %v: %s", err, strings.TrimSpace(string(out)))}
	}
	return CriterionResult{Criterion: c, Passed: true,
		Detail: fmt.Sprintf("migration command succeeded: %s", strings.TrimSpace(string(out)))}
}

// evaluateSchemaChanged dumps the current schema and compares against either
// the baseline file path (if SchemaBaseline is set) or .vxd-db/baseline-schema.txt
// in the worktree. Passes if the dump differs from the baseline (non-empty diff).
//
// Resolution order is important: the operator-supplied SchemaBaseline
// is validated BEFORE the DSN check / pgx connect / schema dump. A
// malicious or misconfigured `vxd.yaml` with an absolute or traversal
// baseline must be rejected even when no devdb is provisioned for the
// story — otherwise the path-escape attack surface stays alive whenever
// devdb is disabled.
func evaluateSchemaChanged(c Criterion, workDir string) CriterionResult {
	// SchemaBaseline is operator-supplied via vxd.yaml — the same threat
	// model as the other path-bearing criteria. resolvePath rejects
	// absolute paths and traversal that would escape the worktree; the
	// baseline contents flow back through criterion Detail to the
	// dashboard, so a `../../etc/shadow` baseline would otherwise leak
	// host files via the diff message.
	baselinePath := c.SchemaBaseline
	if baselinePath == "" {
		baselinePath = filepath.Join(workDir, ".vxd-db", "baseline-schema.txt")
	} else {
		resolved, perr := resolvePath(workDir, baselinePath)
		if perr != nil {
			return CriterionResult{Criterion: c, Passed: false,
				Detail: fmt.Sprintf("schema_changed: %v", perr)}
		}
		baselinePath = resolved
	}

	dsn := readDatabaseURL(workDir)
	if dsn == "" {
		return CriterionResult{Criterion: c, Passed: false,
			Detail: "no .vxd-db/connect.env in worktree — devdb not provisioned for this story"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return CriterionResult{Criterion: c, Passed: false,
			Detail: fmt.Sprintf("schema_changed: pgx connect failed: %v", err)}
	}
	defer func() { _ = conn.Close(ctx) }() // best-effort cleanup

	current, err := dumpSchemaText(ctx, conn)
	if err != nil {
		return CriterionResult{Criterion: c, Passed: false,
			Detail: fmt.Sprintf("schema_changed: dump failed: %v", err)}
	}
	baseline, err := os.ReadFile(baselinePath)
	if err != nil {
		return CriterionResult{Criterion: c, Passed: false,
			Detail: fmt.Sprintf("schema_changed: cannot read baseline %s: %v", baselinePath, err)}
	}
	if string(baseline) == current {
		return CriterionResult{Criterion: c, Passed: false,
			Detail: "schema_changed: no diff between current and baseline schema"}
	}
	return CriterionResult{Criterion: c, Passed: true,
		Detail: "schema_changed: schema differs from baseline (as expected)"}
}

// evaluateSQLQueryReturns runs the configured SQL against the story DB.
// Passes if the query returns at least one row, OR exactly ExpectedRows rows when set.
//
// The YAML-supplied SQL flows through the same read-only gate that
// protects `vxd db sql`: only SELECT/SHOW/VALUES/TABLE are accepted
// (no --write override here — this is a QA criterion, not a CLI
// command), multi-statement is rejected, and execution wraps in a
// pgx ReadOnly transaction so Postgres itself blocks DDL/DML that
// slipped past the classifier.
func evaluateSQLQueryReturns(c Criterion, workDir string) CriterionResult {
	if c.SQL == "" {
		return CriterionResult{Criterion: c, Passed: false,
			Detail: "sql_query_returns requires `sql` field"}
	}
	if err := sqlsafety.ValidateSQLForReadOnly(c.SQL, false); err != nil {
		return CriterionResult{Criterion: c, Passed: false,
			Detail: fmt.Sprintf("sql_query_returns: rejected unsafe SQL: %v", err)}
	}
	dsn := readDatabaseURL(workDir)
	if dsn == "" {
		return CriterionResult{Criterion: c, Passed: false,
			Detail: "no .vxd-db/connect.env in worktree — devdb not provisioned for this story"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return CriterionResult{Criterion: c, Passed: false,
			Detail: fmt.Sprintf("sql_query_returns: pgx connect failed: %v", err)}
	}
	defer func() { _ = conn.Close(ctx) }() // best-effort cleanup
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return CriterionResult{Criterion: c, Passed: false,
			Detail: fmt.Sprintf("sql_query_returns: begin read-only tx failed: %v", err)}
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	rows, err := tx.Query(ctx, c.SQL)
	if err != nil {
		return CriterionResult{Criterion: c, Passed: false,
			Detail: fmt.Sprintf("sql_query_returns: query failed: %v", err)}
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
	}
	if err := rows.Err(); err != nil {
		return CriterionResult{Criterion: c, Passed: false,
			Detail: fmt.Sprintf("sql_query_returns: rows error: %v", err)}
	}
	if c.ExpectedRows != nil {
		if count != *c.ExpectedRows {
			return CriterionResult{Criterion: c, Passed: false,
				Detail: fmt.Sprintf("sql_query_returns: got %d rows, want %d", count, *c.ExpectedRows)}
		}
		return CriterionResult{Criterion: c, Passed: true,
			Detail: fmt.Sprintf("sql_query_returns: matched %d rows", count)}
	}
	if count == 0 {
		return CriterionResult{Criterion: c, Passed: false,
			Detail: "sql_query_returns: query returned zero rows"}
	}
	return CriterionResult{Criterion: c, Passed: true,
		Detail: fmt.Sprintf("sql_query_returns: returned %d rows", count)}
}

// dumpSchemaText returns a deterministic text representation of the connected
// DB's schema. Mirrors DumpSchema in internal/devdb but uses pgx directly to
// avoid an import cycle.
func dumpSchemaText(ctx context.Context, conn *pgx.Conn) (string, error) {
	rows, err := conn.Query(ctx, `
		SELECT table_schema, table_name, column_name, data_type, is_nullable
		FROM information_schema.columns
		WHERE table_schema NOT IN ('pg_catalog','information_schema')
		ORDER BY table_schema, table_name, ordinal_position
	`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var b strings.Builder
	curr := ""
	for rows.Next() {
		var schema, table, col, dtype, nullable string
		if err := rows.Scan(&schema, &table, &col, &dtype, &nullable); err != nil {
			return "", err
		}
		key := schema + "." + table
		if key != curr {
			b.WriteString("\nTABLE " + key + "\n")
			curr = key
		}
		fmt.Fprintf(&b, "  %s %s (nullable=%s)\n", col, dtype, nullable)
	}
	return b.String(), rows.Err()
}
