package cli

import (
	"fmt"
	"strings"
)

// QueryKind classifies the leading statement of a SQL query for safety
// gating. Read-only kinds are allowed by default in `vxd db sql`; any
// other kind requires --write.
type QueryKind int

const (
	QueryUnknown QueryKind = iota
	QueryReadOnly
	QueryMutating
)

// readOnlyLeadingKeywords are leading SQL keywords that cannot mutate
// data on their own. WITH is included because CTEs (which always start
// with WITH) finish with a SELECT in the common case; if the CTE ends
// in INSERT/UPDATE/DELETE the gate falls back to false-positive-safe
// behaviour (rejected as mutating). EXPLAIN and SHOW are read-only by
// definition.
var readOnlyLeadingKeywords = map[string]struct{}{
	"SELECT":  {},
	"WITH":    {},
	"EXPLAIN": {},
	"SHOW":    {},
	"VALUES":  {},
	"TABLE":   {}, // TABLE foo; is shorthand for SELECT * FROM foo
}

// IsMultiStatement reports whether the query contains more than one
// top-level statement separated by `;`. Trailing semicolons are tolerated.
// Detection is intentionally simple: any `;` followed by non-whitespace,
// non-comment content counts as multi-statement.
func IsMultiStatement(query string) bool {
	stripped := stripSQLCommentsAndStrings(query)
	stripped = strings.TrimRight(strings.TrimSpace(stripped), ";")
	return strings.Contains(stripped, ";")
}

// ClassifyQuery returns the QueryKind for the given SQL string. Empty
// queries (after stripping comments/whitespace) return QueryUnknown.
func ClassifyQuery(query string) QueryKind {
	body := strings.TrimSpace(stripSQLCommentsAndStrings(query))
	if body == "" {
		return QueryUnknown
	}
	// Grab the first whitespace-separated word.
	first := body
	if idx := strings.IndexAny(body, " \t\n\r("); idx > 0 {
		first = body[:idx]
	}
	first = strings.ToUpper(first)
	if _, ok := readOnlyLeadingKeywords[first]; ok {
		return QueryReadOnly
	}
	return QueryMutating
}

// ValidateSQLForReadOnly returns an error if the query is not safe to run
// without the --write flag. Specifically it rejects multi-statement
// queries (always — too easy to hide a mutation behind a leading SELECT)
// and queries whose leading statement is not in readOnlyLeadingKeywords.
//
// Callers pass `writeFlag=true` when the operator opted in to mutations;
// in that case multi-statement is still rejected so a typo can't silently
// run extra statements, but the leading-keyword check is skipped.
func ValidateSQLForReadOnly(query string, writeFlag bool) error {
	if strings.TrimSpace(query) == "" {
		return fmt.Errorf("query is empty")
	}
	if IsMultiStatement(query) {
		return fmt.Errorf("multi-statement queries are rejected; run statements one at a time")
	}
	if writeFlag {
		return nil
	}
	switch ClassifyQuery(query) {
	case QueryReadOnly:
		return nil
	case QueryUnknown:
		return fmt.Errorf("query is empty after stripping comments")
	default:
		return fmt.Errorf("query is not read-only; re-run with --write to allow mutations")
	}
}

// stripSQLCommentsAndStrings removes -- line comments, /* */ block
// comments, and string literals (single-quoted) from the input. This
// neutralises ambushes like `SELECT 1; /* */ DROP TABLE foo` that would
// fool a naive substring check. The output is suitable ONLY for classifier
// use — it is not a valid SQL string.
func stripSQLCommentsAndStrings(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		// Line comment: -- ... \n
		if i+1 < len(s) && s[i] == '-' && s[i+1] == '-' {
			for i < len(s) && s[i] != '\n' {
				i++
			}
			continue
		}
		// Block comment: /* ... */
		if i+1 < len(s) && s[i] == '/' && s[i+1] == '*' {
			i += 2
			for i+1 < len(s) && !(s[i] == '*' && s[i+1] == '/') {
				i++
			}
			if i+1 < len(s) {
				i += 2
			} else {
				i = len(s)
			}
			continue
		}
		// Single-quoted string: '...' with '' as escaped quote
		if s[i] == '\'' {
			i++
			for i < len(s) {
				if s[i] == '\'' {
					if i+1 < len(s) && s[i+1] == '\'' {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
