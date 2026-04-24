package engine

import (
	"testing"
)

func TestAnalyzeFailure(t *testing.T) {
	tests := []struct {
		name     string
		qaOutput string
		expected string
	}{
		// Empty/default cases
		{
			name:     "empty output",
			qaOutput: "",
			expected: "Check the QA output above for details",
		},
		{
			name:     "no matching patterns",
			qaOutput: "Some random output with no recognizable failure patterns",
			expected: "Check the QA output above for details",
		},

		// Compilation errors - package not found
		{
			name:     "cannot find package",
			qaOutput: "[BUILD FAILED]\nbuild cannot find package 'github.com/missing/pkg' in any of:\n\t/usr/local/go/src/github.com/missing/pkg",
			expected: "Compilation error: Missing package or file. Check import paths and ensure all dependencies are properly installed with 'go mod tidy'.",
		},
		{
			name:     "package not found",
			qaOutput: "package not found: github.com/example/missing",
			expected: "Compilation error: Missing package or file. Check import paths and ensure all dependencies are properly installed with 'go mod tidy'.",
		},
		{
			name:     "no such file with .go extension",
			qaOutput: "open /path/to/missing.go: no such file or directory",
			expected: "Compilation error: Missing package or file. Check import paths and ensure all dependencies are properly installed with 'go mod tidy'.",
		},

		// Compilation errors - undefined symbols
		{
			name:     "undefined variable",
			qaOutput: "main.go:15:2: undefined: someVariable",
			expected: "Compilation error: Undefined symbol. Check for typos in variable/function names, missing imports, or incorrect struct field names.",
		},
		{
			name:     "not declared",
			qaOutput: "error: 'someFunc' not declared in this scope",
			expected: "Compilation error: Undefined symbol. Check for typos in variable/function names, missing imports, or incorrect struct field names.",
		},
		{
			name:     "undeclared name",
			qaOutput: "undeclared name: unknownFunction",
			expected: "Compilation error: Undefined symbol. Check for typos in variable/function names, missing imports, or incorrect struct field names.",
		},

		// Syntax errors
		{
			name:     "syntax error",
			qaOutput: "syntax error: unexpected '}' at line 42",
			expected: "Syntax error: Invalid Go syntax. Check for missing semicolons, unmatched brackets, or incorrect language constructs.",
		},
		{
			name:     "expected found pattern",
			qaOutput: "expected ';', found 'EOF'",
			expected: "Syntax error: Invalid Go syntax. Check for missing semicolons, unmatched brackets, or incorrect language constructs.",
		},
		{
			name:     "expected got pattern",
			qaOutput: "expected identifier, got '{'",
			expected: "Syntax error: Invalid Go syntax. Check for missing semicolons, unmatched brackets, or incorrect language constructs.",
		},

		// Test failures
		{
			name:     "test failure with expected",
			qaOutput: "[TEST FAILED]\nTest failed: expected 42, got 24",
			expected: "Test failure: One or more tests failed. Review the test output to identify assertion failures, incorrect expected values, or logic errors.",
		},
		{
			name:     "simple fail in test",
			qaOutput: "FAIL\tgithub.com/example/pkg\t0.023s",
			expected: "Test failure: One or more tests failed. Review the test output to identify assertion failures, incorrect expected values, or logic errors.",
		},

		// Runtime panics
		{
			name:     "panic with colon",
			qaOutput: "panic: runtime error: invalid memory address or nil pointer dereference",
			expected: "Runtime panic: Code crashed during execution. Check for nil pointer access, array bounds violations, or unhandled error conditions.",
		},
		{
			name:     "runtime error",
			qaOutput: "runtime error: slice bounds out of range",
			expected: "Runtime panic: Code crashed during execution. Check for nil pointer access, array bounds violations, or unhandled error conditions.",
		},
		{
			name:     "index out of range",
			qaOutput: "index out of range [5] with length 3",
			expected: "Runtime panic: Code crashed during execution. Check for nil pointer access, array bounds violations, or unhandled error conditions.",
		},
		{
			name:     "nil pointer dereference",
			qaOutput: "runtime error: invalid memory address or nil pointer dereference",
			expected: "Runtime panic: Code crashed during execution. Check for nil pointer access, array bounds violations, or unhandled error conditions.",
		},

		// Fatal errors
		{
			name:     "fatal error",
			qaOutput: "fatal error: concurrent map writes",
			expected: "Fatal runtime error: Severe runtime failure occurred. Investigate goroutine panics, memory issues, or stack overflow conditions.",
		},
		{
			name:     "goroutine panic",
			qaOutput: "goroutine 1 [running]:\npanic: something went wrong",
			expected: "Fatal runtime error: Severe runtime failure occurred. Investigate goroutine panics, memory issues, or stack overflow conditions.",
		},

		// Lint issues
		{
			name:     "golint issue",
			qaOutput: "golint: exported function should have comment or be unexported",
			expected: "Linting issue: Code style violations detected. Add missing comments for exported functions, fix naming conventions, or address other style issues.",
		},
		{
			name:     "dot imports warning",
			qaOutput: "should not use dot imports",
			expected: "Linting issue: Code style violations detected. Add missing comments for exported functions, fix naming conventions, or address other style issues.",
		},
		{
			name:     "exported comment missing",
			qaOutput: "exported function Foo should have comment",
			expected: "Linting issue: Code style violations detected. Add missing comments for exported functions, fix naming conventions, or address other style issues.",
		},

		// Static analysis
		{
			name:     "staticcheck issue",
			qaOutput: "staticcheck: unused variable 'x'",
			expected: "Static analysis issue: Code quality problems detected. Remove unused variables/functions, fix ineffective assignments, or address other static analysis warnings.",
		},
		{
			name:     "unused variable",
			qaOutput: "unused variable or function detected",
			expected: "Static analysis issue: Code quality problems detected. Remove unused variables/functions, fix ineffective assignments, or address other static analysis warnings.",
		},
		{
			name:     "ineffective assignment",
			qaOutput: "ineffective assignment to variable",
			expected: "Static analysis issue: Code quality problems detected. Remove unused variables/functions, fix ineffective assignments, or address other static analysis warnings.",
		},

		// Dependencies
		{
			name:     "missing go.sum entry",
			qaOutput: "missing go.sum entry for module github.com/example/pkg",
			expected: "Dependency issue: Missing go.sum entries. Run 'go mod tidy' to update dependencies and regenerate the go.sum file.",
		},
		{
			name:     "go.sum missing pattern",
			qaOutput: "go.sum is missing entries for required modules",
			expected: "Dependency issue: Missing go.sum entries. Run 'go mod tidy' to update dependencies and regenerate the go.sum file.",
		},
		{
			name:     "module not found",
			qaOutput: "go: module github.com/missing/module not found",
			expected: "Module dependency issue: Required module not found. Verify module paths in go.mod, run 'go mod tidy', or check if the module exists and is accessible.",
		},
		{
			name:     "could not import module",
			qaOutput: "could not import module github.com/example/pkg",
			expected: "Module dependency issue: Required module not found. Verify module paths in go.mod, run 'go mod tidy', or check if the module exists and is accessible.",
		},

		// Permission errors
		{
			name:     "permission denied",
			qaOutput: "permission denied: cannot write to file",
			expected: "Permission error: Insufficient permissions to access files or execute operations. Check file permissions or run with appropriate privileges.",
		},
		{
			name:     "access denied",
			qaOutput: "access denied: unable to read configuration",
			expected: "Permission error: Insufficient permissions to access files or execute operations. Check file permissions or run with appropriate privileges.",
		},
		{
			name:     "operation not permitted",
			qaOutput: "operation not permitted: cannot execute binary",
			expected: "Permission error: Insufficient permissions to access files or execute operations. Check file permissions or run with appropriate privileges.",
		},

		// Build failures
		{
			name:     "build failed",
			qaOutput: "[BUILD FAILED]\nCompilation terminated due to errors",
			expected: "Build failure: Project failed to compile. Check for missing files, incorrect build tags, or platform-specific build constraints.",
		},
		{
			name:     "compilation error",
			qaOutput: "compilation error: multiple issues detected",
			expected: "Build failure: Project failed to compile. Check for missing files, incorrect build tags, or platform-specific build constraints.",
		},
		{
			name:     "build constraints",
			qaOutput: "build constraints exclude all Go files",
			expected: "Build failure: Project failed to compile. Check for missing files, incorrect build tags, or platform-specific build constraints.",
		},

		// Network issues
		{
			name:     "connection refused",
			qaOutput: "connection refused: unable to connect to database",
			expected: "Network connectivity issue: Unable to connect to external resources. Check network connectivity, proxy settings, or service availability.",
		},
		{
			name:     "no such host",
			qaOutput: "no such host: api.example.com",
			expected: "Network connectivity issue: Unable to connect to external resources. Check network connectivity, proxy settings, or service availability.",
		},
		{
			name:     "connection timeout",
			qaOutput: "timeout: failed to connect within 30 seconds",
			expected: "Network connectivity issue: Unable to connect to external resources. Check network connectivity, proxy settings, or service availability.",
		},

		// Case insensitive matching
		{
			name:     "case insensitive panic",
			qaOutput: "PANIC: Something went wrong in UPPERCASE",
			expected: "Runtime panic: Code crashed during execution. Check for nil pointer access, array bounds violations, or unhandled error conditions.",
		},
		{
			name:     "case insensitive undefined",
			qaOutput: "UNDEFINED: MissingFunction not found",
			expected: "Compilation error: Undefined symbol. Check for typos in variable/function names, missing imports, or incorrect struct field names.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AnalyzeFailure(tt.qaOutput)
			if result != tt.expected {
				t.Errorf("AnalyzeFailure(%q) = %q, want %q", tt.qaOutput, result, tt.expected)
			}
		})
	}
}

func TestAnalyzeFailure_MultiplePatterns(t *testing.T) {
	// Test that the function returns the first matching pattern
	qaOutput := "panic: undefined variable someVar causes runtime error"

	result := AnalyzeFailure(qaOutput)

	// Should match the first pattern encountered (undefined variable in this case)
	expected := "Compilation error: Undefined symbol. Check for typos in variable/function names, missing imports, or incorrect struct field names."
	if result != expected {
		t.Errorf("AnalyzeFailure with multiple patterns = %q, want %q", result, expected)
	}
}

func TestAnalyzeFailure_RealWorldExample(t *testing.T) {
	// Test with a realistic QA failure output
	qaOutput := `[BUILD FAILED]
main.go:15:2: undefined: fmt
./src/handler.go:23:1: syntax error: unexpected '}' at end of statement
--- FAIL: TestCreateUser (0.01s)
    handler_test.go:45: Expected status code 201, got 500`

	result := AnalyzeFailure(qaOutput)

	// Should detect the undefined symbol first
	expected := "Compilation error: Undefined symbol. Check for typos in variable/function names, missing imports, or incorrect struct field names."
	if result != expected {
		t.Errorf("AnalyzeFailure with real-world example = %q, want %q", result, expected)
	}
}

// Test that covers at least 5 heuristic patterns as required by acceptance criteria
func TestAnalyzeFailure_MinimumPatterns(t *testing.T) {
	patterns := []struct {
		name   string
		output string
	}{
		{"compilation_error", "undefined: someFunction"},
		{"test_failure", "FAIL: test case failed with expected 1 got 2"},
		{"lint_issue", "golint: exported function should have comment"},
		{"dependency_issue", "missing go.sum entry for module"},
		{"permission_error", "permission denied: cannot access file"},
		{"syntax_error", "syntax error: unexpected token"},
		{"runtime_panic", "panic: nil pointer dereference"},
		{"build_failure", "build failed: compilation error"},
		{"network_issue", "connection refused to host"},
	}

	uniqueHints := make(map[string]bool)

	for _, p := range patterns {
		result := AnalyzeFailure(p.output)
		if result == "Check the QA output above for details" {
			t.Errorf("Pattern %q should have been detected, but got generic response", p.name)
		}
		uniqueHints[result] = true
	}

	// Verify we have at least 5 different hints (acceptance criteria requirement)
	if len(uniqueHints) < 5 {
		t.Errorf("Expected at least 5 different heuristic patterns, got %d unique hints", len(uniqueHints))
		t.Log("Unique hints found:")
		for hint := range uniqueHints {
			t.Log(" -", hint)
		}
	}
}