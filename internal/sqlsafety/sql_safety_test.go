package sqlsafety

import (
	"strings"
	"testing"
)

func TestClassifyQuery(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  QueryKind
	}{
		{"select", "SELECT 1", QueryReadOnly},
		{"select lowercase", "select * from foo", QueryReadOnly},
		{"select with leading whitespace", "   \n  SELECT 1", QueryReadOnly},
		{"show", "SHOW search_path", QueryReadOnly},
		{"values", "VALUES (1), (2)", QueryReadOnly},
		{"table shorthand", "TABLE foo", QueryReadOnly},

		// CTEs and EXPLAIN are flagged as mutating so they can only run
		// through --write. WITH can wrap DELETE … RETURNING; EXPLAIN
		// ANALYZE actually runs the wrapped statement. The --write
		// SELECT path still runs under BEGIN READ ONLY so Postgres
		// rejects mutations at the protocol level.
		{"with cte rejected (could be DELETE RETURNING)", "WITH a AS (DELETE FROM t RETURNING *) SELECT * FROM a", QueryMutating},
		{"with cte select-only also rejected statically", "WITH a AS (SELECT 1) SELECT * FROM a", QueryMutating},
		{"explain bare rejected", "EXPLAIN SELECT 1", QueryMutating},
		{"explain analyze rejected", "EXPLAIN ANALYZE INSERT INTO t VALUES (1)", QueryMutating},

		{"insert", "INSERT INTO t VALUES (1)", QueryMutating},
		{"update", "UPDATE t SET x=1", QueryMutating},
		{"delete", "DELETE FROM t", QueryMutating},
		{"drop", "DROP TABLE t", QueryMutating},
		{"truncate", "TRUNCATE t", QueryMutating},
		{"create", "CREATE TABLE x (i int)", QueryMutating},
		{"alter", "ALTER TABLE t ADD COLUMN x int", QueryMutating},
		{"grant", "GRANT ALL ON foo TO bar", QueryMutating},

		{"empty", "", QueryUnknown},
		{"only comment", "-- nothing here\n", QueryUnknown},
		{"only block comment", "/* still nothing */", QueryUnknown},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyQuery(tt.query); got != tt.want {
				t.Errorf("ClassifyQuery(%q) = %v, want %v", tt.query, got, tt.want)
			}
		})
	}
}

func TestIsMultiStatement(t *testing.T) {
	cases := []struct {
		query string
		want  bool
	}{
		{"SELECT 1", false},
		{"SELECT 1;", false},
		{"SELECT 1;   \n  ", false},
		{"SELECT 1; SELECT 2", true},
		{"SELECT 1; DROP TABLE foo", true},
		// Trailing comment after a single statement is fine.
		{"DROP TABLE foo; -- harmless comment", false},
		// But trailing actual statement after the comment IS multi.
		{"DROP TABLE foo; -- harmless\nSELECT 1", true},
		// String literal containing semicolon must not trigger:
		{"SELECT ';'", false},
		{"SELECT 'foo;bar;baz'", false},
		// Block comment containing semicolon must not trigger:
		{"SELECT 1 /* ; ; ; */", false},
		// Line comment containing semicolon must not trigger:
		{"SELECT 1 -- ; harmless\n", false},
	}
	for _, tt := range cases {
		if got := IsMultiStatement(tt.query); got != tt.want {
			t.Errorf("IsMultiStatement(%q) = %v, want %v", tt.query, got, tt.want)
		}
	}
}

func TestValidateSQLForReadOnly(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		write   bool
		wantErr string // substring; empty = no error
	}{
		{"select default", "SELECT 1", false, ""},
		{"select with --write", "SELECT 1", true, ""},
		{"insert default rejected", "INSERT INTO t VALUES (1)", false, "not read-only"},
		{"insert with --write", "INSERT INTO t VALUES (1)", true, ""},
		{"drop default rejected", "DROP TABLE foo", false, "not read-only"},
		{"drop with --write allowed", "DROP TABLE foo", true, ""},

		// Multi-statement always rejected, even with --write — protects
		// against typo-introduced extras after a leading mutation.
		{"multi rejected without write", "SELECT 1; DROP TABLE foo", false, "multi-statement"},
		{"multi rejected with write", "SELECT 1; DROP TABLE foo", true, "multi-statement"},

		// Comment ambush: payload hidden after `;` inside a block comment
		// must be detected because the stripper removes the comment first.
		{"comment ambush", "SELECT 1 /* harmless */ ; DROP TABLE foo", false, "multi-statement"},

		// Empty / whitespace-only.
		{"empty query", "", false, "empty"},
		{"whitespace only", "   \n  ", false, "empty"},
		{"comment only", "-- nothing\n", false, "empty after stripping"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSQLForReadOnly(tt.query, tt.write)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("got error %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("got error %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestStripSQLCommentsAndStrings(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"SELECT 1", "SELECT 1"},
		{"SELECT 1 -- comment\n", "SELECT 1 \n"},
		{"SELECT 1 /* block */ FROM t", "SELECT 1  FROM t"},
		{"SELECT '; DROP'", "SELECT "},
		{"SELECT '''escaped'''", "SELECT "},
		// Unterminated comment — strip to EOF rather than panic.
		{"SELECT 1 /* unterminated", "SELECT 1 "},
		// Unterminated string — strip to EOF rather than panic.
		{"SELECT '''unterminated", "SELECT "},
	}
	for _, tt := range cases {
		if got := stripSQLCommentsAndStrings(tt.in); got != tt.want {
			t.Errorf("stripSQLCommentsAndStrings(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestSQLSafety_FunctionDenylist pins the side-effecting-function denylist:
// every built-in denied function is rejected in read-only mode, matching is
// case-insensitive and whitespace-invariant, comment ambushes reassemble and
// are caught, and near-miss identifiers do NOT false-positive.
func TestSQLSafety_FunctionDenylist(t *testing.T) {
	denied := []struct {
		name  string
		query string
	}{
		{"pg_terminate_backend", "SELECT pg_terminate_backend(1234)"},
		{"pg_cancel_backend", "SELECT pg_cancel_backend(1234)"},
		{"lo_import", "SELECT lo_import('/etc/passwd')"},
		{"lo_export", "SELECT lo_export(12345, '/tmp/out')"},
		{"pg_read_file", "SELECT pg_read_file('postgresql.conf')"},
		{"pg_ls_dir", "SELECT pg_ls_dir('.')"},
		{"pg_reload_conf", "SELECT pg_reload_conf()"},
		{"pg_stat_file", "SELECT pg_stat_file('postgresql.conf')"},
		{"case-insensitive", "SELECT PG_TERMINATE_BACKEND(1)"},
		{"mixed-case", "SELECT Pg_Terminate_Backend(1)"},
		{"whitespace before paren", "SELECT pg_terminate_backend   (1)"},
		{"newline before paren", "SELECT pg_terminate_backend\n(1)"},
		{"schema-qualified", "SELECT pg_catalog.pg_terminate_backend(1)"},
		{"comment ambush reassembles", "SELECT pg_terminate/**/_backend(1)"},
		{"nested in expression", "SELECT 1 WHERE pg_terminate_backend(pid) IS NOT NULL"},
	}
	for _, tt := range denied {
		t.Run("denied/"+tt.name, func(t *testing.T) {
			err := ValidateSQL(tt.query, false, nil)
			if err == nil {
				t.Fatalf("ValidateSQL(%q, false) = nil, want denylist rejection", tt.query)
			}
			if !strings.Contains(err.Error(), "side-effecting function") {
				t.Errorf("error %v does not mention side-effecting function", err)
			}
		})
	}

	allowed := []struct {
		name  string
		query string
	}{
		{"prefix identifier is a different function", "SELECT my_pg_terminate_backend(1)"},
		{"column named like function, no call", "SELECT pg_terminate_backend FROM audit_log"},
		{"quoted string mention", "SELECT * FROM logs WHERE msg = 'pg_terminate_backend(1)'"},
		{"comment mention", "SELECT 1 -- pg_terminate_backend(1)"},
		{"plain select", "SELECT id, name FROM users"},
	}
	for _, tt := range allowed {
		t.Run("allowed/"+tt.name, func(t *testing.T) {
			if err := ValidateSQL(tt.query, false, nil); err != nil {
				t.Errorf("ValidateSQL(%q, false) = %v, want nil", tt.query, err)
			}
		})
	}

	// --write skips the denylist — the operator explicitly opted in.
	if err := ValidateSQL("SELECT pg_terminate_backend(1)", true, nil); err != nil {
		t.Errorf("ValidateSQL with --write = %v, want nil (denylist skipped)", err)
	}
}

// TestSQLSafety_DenylistExtraConfig pins the operator-extended denylist
// (devdb.function_denylist_extra): extra names are denied with the same
// call-shape matching, and blank entries are ignored.
func TestSQLSafety_DenylistExtraConfig(t *testing.T) {
	extra := []string{"dangerous_write_fn", "  audit_purge  ", ""}

	for _, q := range []string{
		"SELECT dangerous_write_fn(1)",
		"SELECT DANGEROUS_WRITE_FN (1)",
		"SELECT audit_purge()",
	} {
		if err := ValidateSQL(q, false, extra); err == nil {
			t.Errorf("ValidateSQL(%q) = nil, want extra-denylist rejection", q)
		}
	}

	// Non-listed functions still pass; extra list doesn't over-match.
	for _, q := range []string{
		"SELECT count(*) FROM t",
		"SELECT not_dangerous_write_fn2(1)",
		"SELECT dangerous_write_fn_v2(1)",
	} {
		if err := ValidateSQL(q, false, extra); err != nil {
			t.Errorf("ValidateSQL(%q) = %v, want nil", q, err)
		}
	}

	// ContainsDeniedFunction reports which function fired.
	fn, hit := ContainsDeniedFunction("SELECT audit_purge()", extra)
	if !hit || fn != "audit_purge" {
		t.Errorf("ContainsDeniedFunction = (%q, %v), want (audit_purge, true)", fn, hit)
	}
}

// TestSQLSafety_QuotedIdentifierBypass pins the fix for the double-quoted
// identifier denylist bypass: a side-effecting function wrapped in Postgres
// double quotes (`"pg_terminate_backend"(...)`) is a valid call of the same
// built-in and must be rejected in read-only mode, not slip past denyPattern
// (which required "(" immediately after the bare name).
func TestSQLSafety_QuotedIdentifierBypass(t *testing.T) {
	denied := []string{
		`SELECT "pg_terminate_backend"(123)`,
		`SELECT pg_catalog."pg_terminate_backend"(123)`,
		`SELECT "pg_read_file"('/etc/passwd')`,
		`SELECT  "pg_cancel_backend" (1)`,
		`SELECT "PG_TERMINATE_BACKEND"(1)`, // case-insensitive denylist over-blocks the (invalid) upper form — safe direction
	}
	for _, q := range denied {
		if _, hit := ContainsDeniedFunction(q, nil); !hit {
			t.Errorf("ContainsDeniedFunction(%q) = false, want denied (quoted-identifier bypass)", q)
		}
		if err := ValidateSQLForReadOnly(q, false); err == nil {
			t.Errorf("ValidateSQLForReadOnly(%q, false) = nil, want rejection", q)
		}
	}

	// A quoted identifier that merely CONTAINS a denylisted substring, or a
	// string-literal mention, must NOT false-positive.
	for _, q := range []string{
		`SELECT "my_pg_read_file_wrapper"(1)`,     // different identifier
		`SELECT count(*) FROM "pg_read_file_log"`, // quoted table name, no call
		`SELECT 'pg_terminate_backend'`,           // string literal, not a call
	} {
		if _, hit := ContainsDeniedFunction(q, nil); hit {
			t.Errorf("ContainsDeniedFunction(%q) = true, want no false positive", q)
		}
	}

	// An apostrophe inside a double-quoted identifier must not be mistaken for
	// a string-literal start (which would swallow a following denied call).
	if _, hit := ContainsDeniedFunction(`SELECT "it's", pg_terminate_backend(1)`, nil); !hit {
		t.Error("apostrophe inside a quoted identifier broke stripping; denied call was missed")
	}
}
