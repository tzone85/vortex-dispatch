package memory

import (
	"testing"
)

func TestParseSearchResults_MultipleResults(t *testing.T) {
	raw := `[1] security / vulnerabilities
    Source: security-findings.md
    Match:  0.850

    Go Vulnerability Database is critical for dependency auditing.

[2] go_ecosystem / releases
    Source: go-releases.md
    Match:  0.420

    Go 1.26 introduces SIMD support and improved GC.
`

	results := ParseSearchResults(raw)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	r1 := results[0]
	if r1.Wing != "security" {
		t.Errorf("result[0].Wing = %q, want %q", r1.Wing, "security")
	}
	if r1.Room != "vulnerabilities" {
		t.Errorf("result[0].Room = %q, want %q", r1.Room, "vulnerabilities")
	}
	if r1.SourceFile != "security-findings.md" {
		t.Errorf("result[0].SourceFile = %q, want %q", r1.SourceFile, "security-findings.md")
	}
	if r1.Similarity != 0.850 {
		t.Errorf("result[0].Similarity = %f, want %f", r1.Similarity, 0.850)
	}
	if r1.Text != "Go Vulnerability Database is critical for dependency auditing." {
		t.Errorf("result[0].Text = %q", r1.Text)
	}

	r2 := results[1]
	if r2.Wing != "go_ecosystem" {
		t.Errorf("result[1].Wing = %q, want %q", r2.Wing, "go_ecosystem")
	}
	if r2.Room != "releases" {
		t.Errorf("result[1].Room = %q, want %q", r2.Room, "releases")
	}
	if r2.Similarity != 0.420 {
		t.Errorf("result[1].Similarity = %f, want %f", r2.Similarity, 0.420)
	}
}

func TestParseSearchResults_SingleResult(t *testing.T) {
	raw := `[1] competitors / swe_agent
    Source: competitor-analysis.md
    Match:  0.670

    SWE-agent released training trajectories for scaling agents.
`

	results := ParseSearchResults(raw)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Wing != "competitors" {
		t.Errorf("Wing = %q, want %q", results[0].Wing, "competitors")
	}
}

func TestParseSearchResults_EmptyInput(t *testing.T) {
	results := ParseSearchResults("")
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty input, got %d", len(results))
	}

	results = ParseSearchResults("   \n  \n  ")
	if len(results) != 0 {
		t.Errorf("expected 0 results for whitespace input, got %d", len(results))
	}
}

func TestParseSearchResults_NoSlashInHeader(t *testing.T) {
	raw := `[1] general_wing
    Source: notes.md
    Match:  0.300

    Some content without wing/room separator.
`

	results := ParseSearchResults(raw)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Wing != "general_wing" {
		t.Errorf("Wing = %q, want %q", results[0].Wing, "general_wing")
	}
	if results[0].Room != "" {
		t.Errorf("Room = %q, want empty", results[0].Room)
	}
}

func TestParseSearchResults_MultilineContent(t *testing.T) {
	raw := `[1] docs / architecture
    Source: arch.md
    Match:  0.500

    Line one of content.
    Line two of content.
    Line three of content.
`

	results := ParseSearchResults(raw)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Text != "Line one of content.\nLine two of content.\nLine three of content." {
		t.Errorf("Text = %q", results[0].Text)
	}
}
