package llm

import (
	"sort"
	"testing"
)

func TestIsKnownModelAlias(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"claude-opus-4-8", true},
		{"claude-opus-4-7", true},
		{"claude-sonnet-4-6", true},
		{"claude-haiku-4-5", true},
		{"gpt-5.5", true},
		{"gemma-4-27b-it", true},
		// dated snapshots are NOT aliases even though they once worked
		{"claude-opus-4-20250514", false},
		{"claude-sonnet-4-6-20250620", false},
		// typos / unknown
		{"claude-opus-4-9", false},
		{"", false},
		{"gpt-4o", false},
	}
	for _, tc := range tests {
		if got := IsKnownModelAlias(tc.id); got != tc.want {
			t.Errorf("IsKnownModelAlias(%q) = %v, want %v", tc.id, got, tc.want)
		}
	}
}

func TestLooksLikeDatedSnapshot(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		// retired snapshots -- the whole reason this check exists
		{"claude-opus-4-20250514", true},
		{"claude-sonnet-4-20250514", true},
		{"claude-sonnet-4-6-20250620", true},
		// undated aliases must NOT be flagged
		{"claude-opus-4-8", false},
		{"claude-sonnet-4-6", false},
		{"claude-haiku-4-5", false},
		{"gpt-5.5", false},
		{"gemma-4-27b-it", false},
		// near-misses: not year-prefixed, or wrong digit count
		{"model-19991231", false},
		{"model-2025051", false},
		{"model-202505141", false},
		{"", false},
	}
	for _, tc := range tests {
		if got := LooksLikeDatedSnapshot(tc.id); got != tc.want {
			t.Errorf("LooksLikeDatedSnapshot(%q) = %v, want %v", tc.id, got, tc.want)
		}
	}
}

func TestKnownModelAliases_SortedAndComplete(t *testing.T) {
	ids := KnownModelAliases()
	if len(ids) == 0 {
		t.Fatal("known alias list is empty")
	}
	if !sort.StringsAreSorted(ids) {
		t.Errorf("KnownModelAliases not sorted: %v", ids)
	}
	// Every shipped default binding must be covered by the registry,
	// otherwise doctor would nag about healthy installs.
	required := []string{
		"claude-opus-4-8", "claude-opus-4-7",
		"claude-sonnet-4-6", "claude-haiku-4-5",
		"gpt-5.5", "gemma-4-27b-it",
	}
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	for _, r := range required {
		if !set[r] {
			t.Errorf("required default model %q missing from KnownModelAliases", r)
		}
	}
}
