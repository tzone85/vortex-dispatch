package engine

import (
	"os"
	"strings"
	"testing"
)

// TestSanitizedEnv_StripsCredentials verifies the QA migration subprocess does
// not inherit the parent's API keys / tokens (which migration tools commonly
// echo into error output that surfaces on the dashboard), while preserving
// benign vars like PATH that migrations legitimately need.
func TestSanitizedEnv_StripsCredentials(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-secret")
	t.Setenv("VAULT_TOKEN", "s.vaulttoken")
	t.Setenv("GOOGLE_AI_API_KEY", "g-secret")
	t.Setenv("VXD_TEST_BENIGN", "keep-me")

	env := sanitizedEnv()
	joined := strings.Join(env, "\n")

	for _, leaked := range []string{"sk-ant-secret", "s.vaulttoken", "g-secret"} {
		if strings.Contains(joined, leaked) {
			t.Errorf("sanitizedEnv leaked a credential value: %q", leaked)
		}
	}
	for _, key := range []string{"ANTHROPIC_API_KEY=", "VAULT_TOKEN=", "GOOGLE_AI_API_KEY="} {
		if strings.Contains(joined, key) {
			t.Errorf("sanitizedEnv kept sensitive key %q", key)
		}
	}
	if !strings.Contains(joined, "VXD_TEST_BENIGN=keep-me") {
		t.Error("sanitizedEnv dropped a benign env var it should preserve")
	}
	if os.Getenv("PATH") != "" && !strings.Contains(joined, "PATH=") {
		t.Error("sanitizedEnv dropped PATH, which migrations need")
	}
}

// TestEvaluateSQLQueryReturns_RejectsMutatingSQL pins the
// sqlsafety.ValidateSQLForReadOnly gate on the YAML criterion path.
// Without it, a malicious vxd.yaml entry like:
//
//	qa.success_criteria:
//	  - kind: sql_query_returns
//	    sql: "DROP TABLE users; SELECT 1"
//
// would execute against the story devdb. With the gate the criterion
// fails fast with a rejection message and the DB is untouched. We
// don't need a real Postgres for this test — the gate runs before any
// connection is opened.
func TestEvaluateSQLQueryReturns_RejectsMutatingSQL(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want string
	}{
		{"plain DROP", "DROP TABLE users", "not read-only"},
		{"INSERT", "INSERT INTO t VALUES (1)", "not read-only"},
		{"multi-stmt with SELECT prefix", "SELECT 1; DROP TABLE t", "multi-statement"},
		{"WITH cte that deletes", "WITH x AS (DELETE FROM t RETURNING *) SELECT * FROM x", "not read-only"},
		{"EXPLAIN ANALYZE INSERT", "EXPLAIN ANALYZE INSERT INTO t VALUES (1)", "not read-only"},
		{"empty after stripping comments", "-- only comment\n", "empty after stripping"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := evaluateSQLQueryReturns(
				Criterion{Kind: CriterionSQLQueryReturns, SQL: c.sql},
				t.TempDir(),
			)
			if res.Passed {
				t.Fatalf("expected rejection, got pass with detail %q", res.Detail)
			}
			if !strings.Contains(res.Detail, c.want) {
				t.Errorf("detail = %q, want substring %q", res.Detail, c.want)
			}
		})
	}
}

// TestEvaluateSchemaChanged_RejectsBaselineEscape pins the resolvePath
// gate on SchemaBaseline. Previously evaluateSchemaChanged accepted any
// absolute path or `../` traversal; a vxd.yaml entry like:
//
//	qa.success_criteria:
//	  - kind: schema_changed
//	    schema_baseline: "/etc/passwd"
//
// would read host files and surface their content (or absence) in the
// criterion Detail string. The gate now refuses absolute paths and
// traversal that escapes the worktree. The check fires BEFORE pgx
// connect, so no DB is needed.
func TestEvaluateSchemaChanged_RejectsBaselineEscape(t *testing.T) {
	cases := []struct {
		name     string
		baseline string
		want     string // substring expected in Detail
	}{
		{"absolute path", "/etc/passwd", "absolute"},
		{"traversal", "../../etc/shadow", "escapes the story worktree"},
		{"interior traversal", "subdir/../../../oops", "escapes the story worktree"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := evaluateSchemaChanged(
				Criterion{Kind: CriterionSchemaChanged, SchemaBaseline: c.baseline},
				t.TempDir(),
			)
			if res.Passed {
				t.Fatalf("expected rejection, got pass with detail %q", res.Detail)
			}
			if !strings.Contains(res.Detail, c.want) {
				t.Errorf("detail = %q, want substring %q", res.Detail, c.want)
			}
		})
	}
}

func TestEvaluateSQLQueryReturns_AcceptsBareSelectShapes(t *testing.T) {
	// These survive the safety gate (they pass classifier + multi-stmt
	// checks). The follow-up `readDatabaseURL` step then fails because
	// no devdb is provisioned in t.TempDir(), so the criterion returns
	// "no .vxd-db/connect.env" — not the rejection message. That's the
	// signal the SQL gate accepted the input.
	for _, sql := range []string{
		"SELECT 1",
		"SHOW search_path",
		"VALUES (1), (2)",
		"TABLE foo",
	} {
		res := evaluateSQLQueryReturns(
			Criterion{Kind: CriterionSQLQueryReturns, SQL: sql},
			t.TempDir(),
		)
		if strings.Contains(res.Detail, "rejected unsafe SQL") {
			t.Errorf("SQL %q wrongly rejected: %s", sql, res.Detail)
		}
	}
}
