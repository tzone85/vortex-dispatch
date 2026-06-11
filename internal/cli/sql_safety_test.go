package cli

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
