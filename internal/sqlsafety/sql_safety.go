package sqlsafety

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
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

// readOnlyLeadingKeywords are leading SQL keywords that the static
// classifier accepts without --write. The list is deliberately narrow:
//
//   - WITH is NOT here: a CTE can wrap DELETE/UPDATE/INSERT … RETURNING
//     and surface as a read-only-looking query. Operators wanting a CTE
//     pass --write, which routes execution through a transaction whose
//     READ ONLY mode lets Postgres itself reject any mutation.
//   - EXPLAIN is NOT here: `EXPLAIN ANALYZE INSERT INTO ...` actually
//     runs the underlying statement. Same routing as WITH.
//
// Even for the keywords below, the read-only path executes inside a
// `BEGIN READ ONLY ... ROLLBACK` transaction so Postgres enforces the
// promise at the protocol level. Side-effecting *function calls* (e.g.
// `SELECT pg_terminate_backend(...)`) are NOT blocked by READ ONLY —
// the known dangerous ones are rejected statically instead (see
// defaultFunctionDenylist); anything not on the denylist (user-defined
// functions that write) remains the operator's responsibility, and the
// command's Long help calls this out explicitly.
var readOnlyLeadingKeywords = map[string]struct{}{
	"SELECT": {},
	"SHOW":   {},
	"VALUES": {},
	"TABLE":  {}, // TABLE foo; is shorthand for SELECT * FROM foo
}

// defaultFunctionDenylist names Postgres functions with side effects that a
// READ ONLY transaction does NOT block: session termination, large-object
// and server-filesystem access, and config reload. Calling any of these from
// `vxd db sql` without --write is rejected by the static classifier — on a
// shared devdb host they let one local user disrupt or read another user's
// session even though the transaction never "writes".
var defaultFunctionDenylist = []string{
	"pg_terminate_backend",
	"pg_cancel_backend",
	"lo_import",
	"lo_export",
	"pg_read_file",
	"pg_ls_dir",
	"pg_reload_conf",
	"pg_stat_file",
}

// denyPatternCache holds compiled per-function regexes. Patterns are tiny and
// the set is small; the cache just avoids recompiling on every CLI query.
var (
	denyPatternMu    sync.Mutex
	denyPatternCache = map[string]*regexp.Regexp{}
)

// denyPattern returns a case-insensitive pattern matching a *call* of fn:
// the function name as a whole identifier (so `my_pg_terminate_backend(` does
// not match, while `pg_catalog.pg_terminate_backend (` does) followed by an
// opening parenthesis with optional whitespace.
func denyPattern(fn string) *regexp.Regexp {
	denyPatternMu.Lock()
	defer denyPatternMu.Unlock()
	if re, ok := denyPatternCache[fn]; ok {
		return re
	}
	re := regexp.MustCompile(`(?i)(^|[^a-zA-Z0-9_])` + regexp.QuoteMeta(strings.ToLower(fn)) + `[\s]*\(`)
	denyPatternCache[fn] = re
	return re
}

// ContainsDeniedFunction reports the first denied side-effecting function
// called in the query, checking the built-in denylist plus any extra names
// supplied by the operator (devdb.function_denylist_extra). Comments and
// string literals are stripped first, so a `pg_terminate/**/_backend(...)`
// comment ambush reassembles into the plain call and is caught, while a
// quoted string mention ('pg_terminate_backend') does not false-positive.
// Double-quoted identifiers are unwrapped by the stripper (their delimiters
// removed but the name kept), so `"pg_terminate_backend"(...)` — a valid
// Postgres call of the same built-in — is caught too and cannot slip past the
// denylist.
func ContainsDeniedFunction(query string, extra []string) (string, bool) {
	stripped := stripSQLCommentsAndStrings(query)
	for _, fn := range defaultFunctionDenylist {
		if denyPattern(fn).MatchString(stripped) {
			return fn, true
		}
	}
	for _, fn := range extra {
		fn = strings.TrimSpace(fn)
		if fn == "" {
			continue
		}
		if denyPattern(fn).MatchString(stripped) {
			return fn, true
		}
	}
	return "", false
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
	return ValidateSQL(query, writeFlag, nil)
}

// ValidateSQL is ValidateSQLForReadOnly plus the side-effecting-function
// denylist: in read-only mode (writeFlag=false) a call to any function in
// defaultFunctionDenylist or extraDeny is rejected, because BEGIN READ ONLY
// does not stop pg_terminate_backend / lo_import / pg_read_file-class calls.
// Operators explicitly opting into mutations with --write skip the denylist,
// same as they skip the leading-keyword check.
func ValidateSQL(query string, writeFlag bool, extraDeny []string) error {
	if strings.TrimSpace(query) == "" {
		return fmt.Errorf("query is empty")
	}
	if IsMultiStatement(query) {
		return fmt.Errorf("multi-statement queries are rejected; run statements one at a time")
	}
	if writeFlag {
		return nil
	}
	if fn, hit := ContainsDeniedFunction(query, extraDeny); hit {
		return fmt.Errorf("query calls side-effecting function %s(), which READ ONLY transactions do not block; re-run with --write if this is intentional", fn)
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
// comments, and string literals (single-quoted) from the input, and unwraps
// double-quoted identifiers (removing the `"` delimiters but KEEPING the
// identifier name). This neutralises ambushes like
// `SELECT 1; /* */ DROP TABLE foo` that would fool a naive substring check,
// and — because the identifier name is preserved — a denylisted function
// wrapped in double quotes (`"pg_terminate_backend"(...)`, a valid Postgres
// call of the same built-in) reassembles into the bare call so the denylist
// pattern still matches. The output is suitable ONLY for classifier use — it
// is not a valid SQL string.
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
			for i+1 < len(s) && (s[i] != '*' || s[i+1] != '/') {
				i++
			}
			if i+1 < len(s) {
				i += 2
			} else {
				i = len(s)
			}
			continue
		}
		// Single-quoted string: '...' with '' as escaped quote. Dropped
		// entirely — a string literal's content is data, never a call.
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
		// Double-quoted identifier: "..." with "" as an escaped quote. Unlike a
		// string literal, the identifier NAME is significant to the classifier —
		// "pg_terminate_backend"(…) invokes the same built-in as the bare form —
		// so emit the inner name WITHOUT the surrounding quotes rather than
		// dropping it. Consuming the whole quoted token here also prevents a
		// stray apostrophe inside an identifier ("it's") from being mistaken for
		// the start of a string literal.
		if s[i] == '"' {
			i++
			for i < len(s) {
				if s[i] == '"' {
					if i+1 < len(s) && s[i+1] == '"' {
						i += 2 // "" escaped quote inside the identifier
						continue
					}
					i++
					break
				}
				b.WriteByte(s[i])
				i++
			}
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
