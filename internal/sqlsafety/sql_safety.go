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
// quoted mention ('pg_terminate_backend') does not false-positive.
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
	if hasUnicodeEscapeIdentifier(query) {
		return fmt.Errorf("query uses a Unicode-escape identifier (U&\"…\"), whose decoded name cannot be safety-checked; re-run with --write if this is intentional")
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

// isIdentStart reports whether b may begin a SQL identifier (letter or
// underscore, not a digit) — used to reject positional parameters like $1 as
// dollar-quote tags.
func isIdentStart(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// isIdentChar reports whether b can appear in a SQL identifier. Used to
// decide whether an E/e immediately before a quote is a standalone
// escape-string introducer (E'...') rather than the tail of an identifier.
func isIdentChar(b byte) bool {
	return isIdentStart(b) || (b >= '0' && b <= '9')
}

// dollarQuoteTag reports whether a dollar-quote opening delimiter begins at
// s[start] (which must be '$'), and if so returns the full delimiter token
// (e.g. "$$" or "$tag$"). The tag between the dollar signs, if present,
// follows unquoted-identifier rules (first char a letter or underscore), so a
// positional parameter like $1 is correctly rejected as a non-delimiter.
func dollarQuoteTag(s string, start int) (string, bool) {
	j := start + 1
	for j < len(s) && s[j] != '$' {
		if j == start+1 {
			if !isIdentStart(s[j]) {
				return "", false
			}
		} else if !isIdentChar(s[j]) {
			return "", false
		}
		j++
	}
	if j >= len(s) || s[j] != '$' {
		return "", false
	}
	return s[start : j+1], true
}

// stripSQLCommentsAndStrings removes -- line comments, /* */ block
// comments, string literals (single-quoted, including E'...' escape
// strings), and dollar-quoted strings ($tag$...$tag$) from the input, and
// unwraps double-quoted identifiers to their bare inner text. This
// neutralises ambushes like `SELECT 1; /* */ DROP TABLE foo` that would
// fool a naive substring check. The output is suitable ONLY for classifier
// use — it is not a valid SQL string.
//
// Double-quoted identifiers are unwrapped (the surrounding quotes dropped,
// `""` collapsed to nothing) rather than left verbatim: Postgres folds an
// unquoted identifier to lowercase, so `"pg_read_file"` names the *same*
// built-in as `pg_read_file`, yet the interposed quote between the name and
// its `(` used to defeat the denylist regex — a call like
// `SELECT "pg_terminate_backend"(pid)` slipped past ContainsDeniedFunction
// and ran under a read-only transaction that does not block it. Collapsing
// `"pg_read_file"(` to `pg_read_file(` makes the classifier see the true
// call. Inner text is stripped of quotes only; no denied function name
// contains a literal quote, so dropping the `""` escape body is safe here.
func stripSQLCommentsAndStrings(s string) string {
	stripped, _ := lexSQL(s)
	return stripped
}

// hasUnicodeEscapeIdentifier reports whether the query contains a
// Postgres Unicode-escape *identifier* (U&"…") outside of comments and
// string literals. Such an identifier can name a function to call while its
// \XXXX escapes decode only server-side, so the denylist regex — which sees
// the raw escapes — cannot recognise the decoded name (e.g.
// `SELECT U&"\0070\0067_..."('/etc/passwd')` calls pg_read_file). The
// read-only gate rejects these rather than attempt to decode arbitrary
// escapes; an operator who truly needs one re-runs with --write. Only the
// identifier form is flagged: U&'…' is a string literal (its content never
// executes) and is already stripped correctly.
func hasUnicodeEscapeIdentifier(query string) bool {
	_, u := lexSQL(query)
	return u
}

// lexSQL walks the query once, returning it with -- / block comments,
// string literals (single-quoted incl. E'…' escape strings, and
// dollar-quoted) removed and double-quoted identifiers unwrapped, plus a
// flag reporting whether a Unicode-escape identifier (U&"…") was seen.
func lexSQL(s string) (string, bool) {
	var b strings.Builder
	b.Grow(len(s))
	unicodeEscapeIdent := false
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
		// Dollar-quoted string: $tag$ ... $tag$ (tag optional, so $$ ... $$).
		// Handled before the single-quote branch because an odd number of
		// apostrophes *inside* a dollar-quoted literal would otherwise desync
		// the single-quote tracker and swallow a denied call that follows the
		// literal — e.g. `SELECT $$'$$, pg_read_file('x')` classified read-only
		// despite the real pg_read_file call outside any string. The tag
		// follows identifier rules (letter/underscore first), so positional
		// params like $1 are NOT treated as a delimiter.
		if s[i] == '$' {
			if tag, ok := dollarQuoteTag(s, i); ok {
				closeIdx := strings.Index(s[i+len(tag):], tag)
				if closeIdx < 0 {
					i = len(s) // unterminated — consume to EOF
				} else {
					i = i + len(tag) + closeIdx + len(tag)
				}
				continue
			}
			// Not a dollar-quote delimiter ($1 param, lone $): ordinary char.
			b.WriteByte(s[i])
			i++
			continue
		}
		// Single-quoted string: '...'. A normal string uses '' as its only
		// quote escape — standard_conforming_strings=on (the modern Postgres
		// default) treats a backslash literally. A string introduced by E'/e'
		// at a token boundary is an *escape string*: there a backslash escapes
		// the next character, so `\'` does NOT close the string. Missing that
		// mis-tracks the string boundary and lets a denied call following the
		// literal be swallowed and hidden — e.g.
		// `SELECT E'\'' || pg_read_file('x')` classified read-only despite the
		// real pg_read_file call. Detect escape mode and consume backslash
		// escapes so the boundary is tracked correctly.
		if s[i] == '\'' {
			escapeString := i >= 1 && (s[i-1] == 'E' || s[i-1] == 'e') &&
				(i == 1 || !isIdentChar(s[i-2]))
			i++
			for i < len(s) {
				if escapeString && s[i] == '\\' {
					i += 2 // skip the backslash and the char it escapes
					continue
				}
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
		// Double-quoted identifier: "..." with "" as escaped quote. Unwrap
		// to the bare inner text so a quoted denied-function name is caught.
		// A U&"…" / u&"…" prefix marks a Unicode-escape identifier whose
		// \XXXX escapes decode only server-side — flag it so the read-only
		// gate can reject it (the raw escapes can't be matched by the regex).
		if s[i] == '"' {
			if i >= 2 && s[i-1] == '&' && (s[i-2] == 'U' || s[i-2] == 'u') &&
				(i == 2 || !isIdentChar(s[i-3])) {
				unicodeEscapeIdent = true
			}
			i++
			for i < len(s) {
				if s[i] == '"' {
					if i+1 < len(s) && s[i+1] == '"' {
						i += 2
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
	return b.String(), unicodeEscapeIdent
}
