package engine

import (
	"strings"
	"testing"
)

func TestAnalyzeError_MissingSymbol(t *testing.T) {
	cat, suggestion := AnalyzeError("./store/store_test.go:42:15: undefined: NewStore")
	if cat != ErrCatMissingSymbol {
		t.Errorf("expected missing_symbol, got %s", cat)
	}
	if suggestion == "" {
		t.Error("expected non-empty suggestion")
	}
}

func TestAnalyzeError_SyntaxError(t *testing.T) {
	cat, _ := AnalyzeError("main.go:10:5: syntax error: unexpected newline")
	if cat != ErrCatSyntax {
		t.Errorf("expected syntax, got %s", cat)
	}
}

func TestAnalyzeError_TypeMismatch(t *testing.T) {
	cat, _ := AnalyzeError("cannot use myVar (type string) as type int")
	if cat != ErrCatTypeError {
		t.Errorf("expected type_error, got %s", cat)
	}
}

func TestAnalyzeError_ImportIssue(t *testing.T) {
	cat, _ := AnalyzeError(`"fmt" imported and not used`)
	if cat != ErrCatImport {
		t.Errorf("expected import, got %s", cat)
	}
}

func TestAnalyzeError_TestFailure(t *testing.T) {
	cat, _ := AnalyzeError("--- FAIL: TestUserCreate (0.01s)\n    user_test.go:25: expected 'admin', got 'user'")
	if cat != ErrCatTestFailure {
		t.Errorf("expected test_failure, got %s", cat)
	}
}

func TestAnalyzeError_Environment(t *testing.T) {
	cat, _ := AnalyzeError("dial tcp 127.0.0.1:5432: connection refused")
	if cat != ErrCatEnvironment {
		t.Errorf("expected environment, got %s", cat)
	}
}

func TestAnalyzeError_Timeout(t *testing.T) {
	cat, _ := AnalyzeError("context deadline exceeded while awaiting response")
	if cat != ErrCatTimeout {
		t.Errorf("expected timeout, got %s", cat)
	}
}

func TestAnalyzeError_Unknown(t *testing.T) {
	cat, suggestion := AnalyzeError("some completely novel error nobody has seen before")
	if cat != ErrCatUnknown {
		t.Errorf("expected unknown, got %s", cat)
	}
	if suggestion == "" {
		t.Error("even unknown errors should have a suggestion")
	}
}

func TestBuildSmartRetryContext_HasStructuredOutput(t *testing.T) {
	qaOutput := "[BUILD FAILED]\n./store/store_test.go:42:15: undefined: NewStore"
	ctx := BuildSmartRetryContext(qaOutput)

	if !strings.Contains(ctx, "Error Category:") {
		t.Error("expected 'Error Category:' in smart retry context")
	}
	if !strings.Contains(ctx, "Fix Guidance:") {
		t.Error("expected 'Fix Guidance:' in smart retry context")
	}
	if !strings.Contains(ctx, "undefined: NewStore") {
		t.Error("expected original error output in smart retry context")
	}
	if !strings.Contains(ctx, "MUST FIX") {
		t.Error("expected MUST FIX header in smart retry context")
	}
}

func TestBuildSmartRetryContext_EmptyInput(t *testing.T) {
	ctx := BuildSmartRetryContext("")
	if ctx != "" {
		t.Errorf("expected empty output for empty input, got %q", ctx)
	}
}
