package engine

import "testing"

func TestSanitizeReviewResult_ClampsNegativeLine(t *testing.T) {
	r := &ReviewResult{
		Summary: "ok",
		Comments: []ReviewComment{
			{File: "a.go", Line: -5, Severity: "major", Comment: "bad line"},
		},
	}
	sanitizeReviewResult(r)
	if r.Comments[0].Line != 0 {
		t.Errorf("Line = %d, want 0", r.Comments[0].Line)
	}
}

func TestSanitizeReviewResult_NormalizesUnknownSeverity(t *testing.T) {
	r := &ReviewResult{
		Comments: []ReviewComment{
			{Severity: "APOCALYPTIC"},
			{Severity: "critical"},
			{Severity: ""},
		},
	}
	sanitizeReviewResult(r)
	if r.Comments[0].Severity != "info" {
		t.Errorf("unknown severity should become info, got %q", r.Comments[0].Severity)
	}
	if r.Comments[1].Severity != "critical" {
		t.Errorf("valid severity should stay, got %q", r.Comments[1].Severity)
	}
	if r.Comments[2].Severity != "info" {
		t.Errorf("empty severity should become info, got %q", r.Comments[2].Severity)
	}
}

func TestSanitizeReviewResult_TruncatesSummary(t *testing.T) {
	long := make([]byte, 5000)
	for i := range long {
		long[i] = 'x'
	}
	r := &ReviewResult{Summary: string(long)}
	sanitizeReviewResult(r)
	if len(r.Summary) > 2100 {
		t.Errorf("summary too long: %d", len(r.Summary))
	}
}

func TestSanitizeReviewResult_PreservesValidResult(t *testing.T) {
	r := &ReviewResult{
		Passed:  true,
		Summary: "Looks good",
		Comments: []ReviewComment{
			{File: "a.go", Line: 42, Severity: "minor", Comment: "nit"},
		},
	}
	sanitizeReviewResult(r)
	if r.Comments[0].Line != 42 {
		t.Errorf("valid line should stay, got %d", r.Comments[0].Line)
	}
	if r.Comments[0].Severity != "minor" {
		t.Errorf("valid severity should stay, got %q", r.Comments[0].Severity)
	}
	if r.Summary != "Looks good" {
		t.Errorf("short summary should stay, got %q", r.Summary)
	}
}
