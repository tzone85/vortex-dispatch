package engine

import (
	"fmt"
	"strings"
)

// ErrorCategory classifies a failure to guide retry behavior.
type ErrorCategory string

const (
	ErrCatMissingSymbol  ErrorCategory = "missing_symbol"  // undefined: X, cannot find symbol
	ErrCatSyntax         ErrorCategory = "syntax"           // syntax error, unexpected token
	ErrCatTypeError      ErrorCategory = "type_error"       // type mismatch, cannot convert
	ErrCatImport         ErrorCategory = "import"           // missing import, unresolved module
	ErrCatTestFailure    ErrorCategory = "test_failure"     // test assertion failed
	ErrCatBuildConfig    ErrorCategory = "build_config"     // missing dependency, wrong version
	ErrCatEnvironment    ErrorCategory = "environment"      // connection refused, file not found, permission denied
	ErrCatTimeout        ErrorCategory = "timeout"          // context deadline, timeout exceeded
	ErrCatUnknown        ErrorCategory = "unknown"          // doesn't match any pattern
)

// errorPattern maps a substring to a category and fix suggestion.
type errorPattern struct {
	substring  string
	category   ErrorCategory
	suggestion string
}

var errorPatterns = []errorPattern{
	// Missing symbol / undefined reference
	{"undefined:", ErrCatMissingSymbol, "A function, variable, or type is used but not defined. Check for typos in the name, missing function definitions, or unexported identifiers (lowercase first letter in Go)."},
	{"cannot find symbol", ErrCatMissingSymbol, "A symbol is referenced but not defined. Check imports and ensure the function/class exists."},
	{"not defined", ErrCatMissingSymbol, "A name is used before it's defined. Add the missing definition or fix the import."},
	{"is not defined", ErrCatMissingSymbol, "A name is not in scope. Check imports or move the definition before its usage."},

	// Syntax errors
	{"syntax error", ErrCatSyntax, "There's a syntax error. Check for missing brackets, semicolons, commas, or mismatched delimiters near the reported line."},
	{"unexpected token", ErrCatSyntax, "Unexpected token found. Check the line above — often a missing comma, bracket, or semicolon."},
	{"expected ';'", ErrCatSyntax, "Missing semicolon. Add it at the reported location."},
	{"expected '}'", ErrCatSyntax, "Missing closing brace. Check for unclosed blocks."},
	{"expected declaration", ErrCatSyntax, "Go expects a declaration (func, var, type, const) at the top level, not a statement."},

	// Type errors
	{"cannot use", ErrCatTypeError, "Type mismatch. The value's type doesn't match what's expected. Check the function signature or variable declaration."},
	{"cannot convert", ErrCatTypeError, "Type conversion failed. Use explicit conversion or check if the types are compatible."},
	{"type mismatch", ErrCatTypeError, "Types don't match. Verify the expected vs actual types and add conversion if needed."},
	{"incompatible types", ErrCatTypeError, "Incompatible types in assignment or comparison. Check both sides of the expression."},

	// Import issues
	{"imported and not used", ErrCatImport, "An import exists but isn't used. Remove the unused import or use goimports to auto-fix."},
	{"could not import", ErrCatImport, "Import failed. Run 'go mod tidy' to resolve missing dependencies."},
	{"no required module provides", ErrCatImport, "Go module not found. Run 'go mod tidy' or 'go get <module>'."},
	{"module not found", ErrCatImport, "Module not found. Check the import path and run 'go mod tidy'."},
	{"unresolved import", ErrCatImport, "Import can't be resolved. Check the module path and run the package manager."},

	// Test failures
	{"FAIL", ErrCatTestFailure, "A test failed. Read the assertion error — it shows expected vs actual values. Fix the implementation to match the expected behavior."},
	{"assert", ErrCatTestFailure, "An assertion failed. Check the expected vs actual values in the test output."},
	{"expected", ErrCatTestFailure, "Test expectation not met. Read the 'expected X, got Y' message and fix the implementation."},

	// Build/config issues
	{"go.sum", ErrCatBuildConfig, "go.sum mismatch. Run 'go mod tidy' to update checksums."},
	{"replace directive", ErrCatBuildConfig, "Go module replace directive issue. Check go.mod for conflicting replacements."},

	// Environment issues
	{"connection refused", ErrCatEnvironment, "A service isn't running or the port is wrong. Check if the database/API/service is started and accessible."},
	{"permission denied", ErrCatEnvironment, "File permission issue. Check file ownership and permissions (chmod/chown)."},
	{"no such file", ErrCatEnvironment, "File not found. Check the path — it may be relative vs absolute, or the file hasn't been created yet."},
	{"ENOENT", ErrCatEnvironment, "File not found (Node.js). Check if the file path is correct."},
	{"address already in use", ErrCatEnvironment, "Port is occupied. Another process is using this port. Kill it or use a different port."},

	// Timeout
	{"context deadline", ErrCatTimeout, "Operation timed out. The test or command took too long. Check for infinite loops, slow network calls, or increase the timeout."},
	{"timeout", ErrCatTimeout, "Timeout occurred. Check for slow operations or increase the timeout threshold."},
}

// AnalyzeError categorizes an error string and returns a fix suggestion.
// This is a lightweight, zero-cost alternative to LLM-based analysis —
// catches ~80% of common failures via pattern matching.
func AnalyzeError(errorOutput string) (ErrorCategory, string) {
	lower := strings.ToLower(errorOutput)

	for _, p := range errorPatterns {
		if strings.Contains(lower, strings.ToLower(p.substring)) {
			return p.category, p.suggestion
		}
	}

	return ErrCatUnknown, "The error doesn't match known patterns. Read the full error output carefully and fix the root cause."
}

// BuildSmartRetryContext creates enhanced retry instructions from error analysis.
// This replaces blind retries with targeted fix instructions.
func BuildSmartRetryContext(qaFailureSummary string) string {
	if qaFailureSummary == "" {
		return ""
	}

	category, suggestion := AnalyzeError(qaFailureSummary)

	return fmt.Sprintf(`## QA Failure Analysis (MUST FIX)

**Error Category:** %s
**Fix Guidance:** %s

**Actual Error Output:**
%s

INSTRUCTIONS:
1. Read the error output above carefully
2. Identify the exact file and line number
3. Fix ONLY the reported issue — do not refactor surrounding code
4. Run the failing command locally to verify your fix
5. Commit and let QA re-run`, category, suggestion, qaFailureSummary)
}
