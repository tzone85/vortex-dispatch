package runtime

import (
	"strings"
	"testing"
)

// TestValidateEnvKey_AcceptsPOSIXNames pins the happy path.
func TestValidateEnvKey_AcceptsPOSIXNames(t *testing.T) {
	for _, k := range []string{"PATH", "HOME", "_FOO", "FOO_BAR", "X1", "_1"} {
		if err := ValidateEnvKey(k); err != nil {
			t.Errorf("expected %q valid, got %v", k, err)
		}
	}
}

// TestValidateEnvKey_RejectsInjectionAttempts pins the security boundary:
// any key that could shell-inject when concatenated into `export %s=...`
// must be rejected.
func TestValidateEnvKey_RejectsInjectionAttempts(t *testing.T) {
	bad := []string{
		"",                  // empty
		"1FOO",              // leading digit
		"FOO BAR",           // space
		"FOO;rm",            // semicolon
		"FOO=BAR",           // equals
		"FOO$BAR",           // dollar
		"FOO`",              // backtick
		"FOO(",              // paren
		"FOO|cat",           // pipe
		"FOO&",              // background
		"FOO\nBAR",          // newline
		"FOO\\",             // backslash
		"FOO'BAR",           // single quote
		"$(rm -rf /)",       // command substitution
	}
	for _, k := range bad {
		if err := ValidateEnvKey(k); err == nil {
			t.Errorf("expected %q invalid, got nil error", k)
		}
	}
}

// TestBuildEnvExports_NeutralizesValueInjection is the core regression test.
// A value containing $(cmd), backticks, or ! MUST be emitted single-quoted
// so that sh -c sees it literally — never executes it.
func TestBuildEnvExports_NeutralizesValueInjection(t *testing.T) {
	payloads := []string{
		`$(touch /tmp/pwned)`,
		"`id`",
		`!history`,
		`$IFS$9cat$IFS/etc/passwd`,
		`x;rm -rf /`,
		`x"y"$z`,
	}
	for _, val := range payloads {
		got := BuildEnvExports(map[string]string{"FOO": val})

		// Must contain the variable name and be a single-export statement.
		if !strings.HasPrefix(got, "export FOO=") {
			t.Errorf("payload %q: expected leading `export FOO=`, got %q", val, got)
		}

		// The value MUST be single-quoted. Single quotes in POSIX shell
		// disable ALL expansion. Verify by checking the substring after
		// `export FOO=` opens with a single quote.
		body := strings.TrimPrefix(got, "export FOO=")
		if !strings.HasPrefix(body, "'") {
			t.Errorf("payload %q: expected single-quoted value, got %q", val, body)
		}
	}
}

// TestBuildEnvExports_DropsInvalidKey verifies the helper silently skips
// (with a log line — verified by absence in output) keys that would inject.
func TestBuildEnvExports_DropsInvalidKey(t *testing.T) {
	got := BuildEnvExports(map[string]string{
		"GOOD":    "value",
		"BAD;rm":  "anything",
		"":        "empty-key",
		"1NUMBER": "leading-digit",
	})
	if !strings.Contains(got, "export GOOD=") {
		t.Errorf("expected GOOD to survive, got %q", got)
	}
	for _, banned := range []string{"BAD;rm", "1NUMBER"} {
		if strings.Contains(got, banned) {
			t.Errorf("expected %q dropped, found in %q", banned, got)
		}
	}
}

// TestBuildEnvExports_DeterministicOrder pins testability: the output is
// stable across map iteration randomness so callers can assert on it.
func TestBuildEnvExports_DeterministicOrder(t *testing.T) {
	in := map[string]string{"B": "1", "A": "1", "C": "1"}
	first := BuildEnvExports(in)
	for i := 0; i < 5; i++ {
		if got := BuildEnvExports(in); got != first {
			t.Errorf("non-deterministic output: %q vs %q", first, got)
		}
	}
	if !strings.Contains(first, "export A=") {
		t.Errorf("expected A first, got %q", first)
	}
	if idxA, idxB, idxC := strings.Index(first, "A="), strings.Index(first, "B="), strings.Index(first, "C="); !(idxA < idxB && idxB < idxC) {
		t.Errorf("expected sorted A<B<C, got order in %q", first)
	}
}

// TestBuildEnvExports_EmptyMap returns an empty prefix.
func TestBuildEnvExports_EmptyMap(t *testing.T) {
	if got := BuildEnvExports(nil); got != "" {
		t.Errorf("expected empty string for nil map, got %q", got)
	}
	if got := BuildEnvExports(map[string]string{}); got != "" {
		t.Errorf("expected empty string for empty map, got %q", got)
	}
}
