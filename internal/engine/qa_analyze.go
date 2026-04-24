package engine

import (
	"strings"
)

// AnalyzeFailure uses simple string-matching heuristics to detect common failure
// patterns in QA output and return actionable hints. This function analyzes the
// failure summary without using LLM services, relying on pattern matching to
// provide quick diagnostic guidance.
func AnalyzeFailure(qaOutput string) string {
	if qaOutput == "" {
		return "Check the QA output above for details"
	}

	// Convert to lowercase for case-insensitive matching
	output := strings.ToLower(qaOutput)

	// Compilation errors (check first as they are root causes)
	// Undefined symbols (highest priority - these cause many runtime issues)
	if strings.Contains(output, "undefined:") ||
		strings.Contains(output, "not declared") ||
		strings.Contains(output, "undeclared name") ||
		strings.Contains(output, "undefined variable") {
		return "Compilation error: Undefined symbol. Check for typos in variable/function names, missing imports, or incorrect struct field names."
	}

	// Missing packages/files
	if strings.Contains(output, "cannot find package") ||
		strings.Contains(output, "package not found") ||
		strings.Contains(output, "no such file or directory") && strings.Contains(output, ".go") {
		return "Compilation error: Missing package or file. Check import paths and ensure all dependencies are properly installed with 'go mod tidy'."
	}

	// Syntax errors
	if strings.Contains(output, "syntax error") ||
		(strings.Contains(output, "expected") && (strings.Contains(output, "found") || strings.Contains(output, "got")) && !strings.Contains(output, "test")) {
		return "Syntax error: Invalid Go syntax. Check for missing semicolons, unmatched brackets, or incorrect language constructs."
	}

	// Fatal runtime errors (check before general panics)
	if strings.Contains(output, "fatal error") ||
		strings.Contains(output, "goroutine") && strings.Contains(output, "panic") {
		return "Fatal runtime error: Severe runtime failure occurred. Investigate goroutine panics, memory issues, or stack overflow conditions."
	}

	// Runtime panics (after compilation issues and fatal errors)
	if strings.Contains(output, "panic:") ||
		strings.Contains(output, "runtime error") ||
		strings.Contains(output, "index out of range") ||
		strings.Contains(output, "nil pointer dereference") {
		return "Runtime panic: Code crashed during execution. Check for nil pointer access, array bounds violations, or unhandled error conditions."
	}

	// Test failures (more specific patterns)
	if (strings.Contains(output, "fail") && strings.Contains(output, "test")) ||
		strings.Contains(output, "test failed") ||
		(strings.Contains(output, "fail\t") || strings.Contains(output, "fail ")) && !strings.Contains(output, "build failed") && !strings.Contains(output, "compilation") {
		return "Test failure: One or more tests failed. Review the test output to identify assertion failures, incorrect expected values, or logic errors."
	}

	// Lint issues
	if strings.Contains(output, "golint") ||
		strings.Contains(output, "should not use dot imports") ||
		strings.Contains(output, "exported") && strings.Contains(output, "should have comment") {
		return "Linting issue: Code style violations detected. Add missing comments for exported functions, fix naming conventions, or address other style issues."
	}

	if strings.Contains(output, "staticcheck") ||
		strings.Contains(output, "unused") ||
		strings.Contains(output, "ineffective assignment") {
		return "Static analysis issue: Code quality problems detected. Remove unused variables/functions, fix ineffective assignments, or address other static analysis warnings."
	}

	// Missing dependencies
	if strings.Contains(output, "missing go.sum entry") ||
		strings.Contains(output, "go.sum") && strings.Contains(output, "missing") {
		return "Dependency issue: Missing go.sum entries. Run 'go mod tidy' to update dependencies and regenerate the go.sum file."
	}

	if strings.Contains(output, "module not found") ||
		strings.Contains(output, "could not import") && strings.Contains(output, "module") {
		return "Module dependency issue: Required module not found. Verify module paths in go.mod, run 'go mod tidy', or check if the module exists and is accessible."
	}

	// Permission errors
	if strings.Contains(output, "permission denied") ||
		strings.Contains(output, "access denied") ||
		strings.Contains(output, "operation not permitted") {
		return "Permission error: Insufficient permissions to access files or execute operations. Check file permissions or run with appropriate privileges."
	}

	// Build/compilation issues
	if strings.Contains(output, "build failed") ||
		strings.Contains(output, "compilation error") ||
		strings.Contains(output, "build constraints") {
		return "Build failure: Project failed to compile. Check for missing files, incorrect build tags, or platform-specific build constraints."
	}

	// Network/connectivity issues
	if strings.Contains(output, "connection refused") ||
		strings.Contains(output, "no such host") ||
		strings.Contains(output, "timeout") && strings.Contains(output, "connect") {
		return "Network connectivity issue: Unable to connect to external resources. Check network connectivity, proxy settings, or service availability."
	}

	// Generic fallback
	return "Check the QA output above for details"
}