package engine

import "testing"

func TestExtractJSON_NestedBracketsInStrings(t *testing.T) {
	// Inner string contains brackets that look like JSON but aren't
	input := `Here: {"key": "value [not an array]", "list": [1,2,3]}`
	want := `{"key": "value [not an array]", "list": [1,2,3]}`
	got := extractJSON(input)
	if got != want {
		t.Errorf("extractJSON nested = %q, want %q", got, want)
	}
}

func TestExtractJSON_NestedObjectsInPreamble(t *testing.T) {
	input := `Result: [{"id":"s-001","meta":{"nested":"obj"}},{"id":"s-002"}]`
	want := `[{"id":"s-001","meta":{"nested":"obj"}},{"id":"s-002"}]`
	got := extractJSON(input)
	if got != want {
		t.Errorf("extractJSON objects = %q, want %q", got, want)
	}
}

func TestExtractJSON_BracketsInsideJsonStrings(t *testing.T) {
	input := `[{"data": "[1,2]"}, {"data": "{\"x\":1}"}]`
	want := input // pure JSON, should return as-is
	got := extractJSON(input)
	if got != want {
		t.Errorf("extractJSON pure = %q, want %q", got, want)
	}
}

// TestExtractJSON_StrayBraceAfterJSON tests that a stray closing brace after the
// JSON payload does not get included via LastIndexByte.
func TestExtractJSON_StrayBraceAfterJSON(t *testing.T) {
	input := `Result: {"key": "val"} and then text with } stray brace`
	want := `{"key": "val"}`
	got := extractJSON(input)
	if got != want {
		t.Errorf("extractJSON stray = %q, want %q", got, want)
	}
}

// TestExtractJSON_PreambleContainsBrackets tests that a preamble containing
// brackets (e.g. a score annotation) does not confuse the JSON start detection.
func TestExtractJSON_PreambleContainsBrackets(t *testing.T) {
	input := `Score [10/10]: [{"id":"s-001"},{"id":"s-002"}]`
	want := `[{"id":"s-001"},{"id":"s-002"}]`
	got := extractJSON(input)
	if got != want {
		t.Errorf("extractJSON preamble brackets = %q, want %q", got, want)
	}
}
