package engine

import (
	"encoding/json"
	"testing"
)

// TestExtractJSON_NestedFenceInStringValue pins finding #6: a code-fenced JSON
// payload whose string values themselves contain a markdown code fence (very
// common in reviewer output that quotes code) must not be truncated at the first
// inner ```. The old fence branch took the first closing fence and returned
// without validating, yielding invalid JSON and a failed review.
func TestExtractJSON_NestedFenceInStringValue(t *testing.T) {
	// summary contains a ```go ... ``` fence inside the JSON string value.
	obj := `{"approved":false,"summary":"replace ` + "```go\\nx:=1\\n```" + ` with a constant"}`
	raw := "```json\n" + obj + "\n```"

	got := extractJSON(raw)
	if !json.Valid([]byte(got)) {
		t.Fatalf("extractJSON returned invalid JSON (truncated at nested fence):\n%s", got)
	}
	var parsed struct {
		Approved bool   `json:"approved"`
		Summary  string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("unmarshal: %v\ngot: %s", err, got)
	}
	if parsed.Approved {
		t.Errorf("approved = true, want false (payload corrupted)")
	}
	if parsed.Summary == "" {
		t.Errorf("summary lost; got: %s", got)
	}
}
