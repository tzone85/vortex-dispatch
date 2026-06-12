package cli

import (
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/improve"
)

func TestDispositionIcon(t *testing.T) {
	cases := map[string]string{
		"implemented": "[OK]",
		"proposed":    "[--]",
		"aborted":     "[!!]",
		"unknown":     "[??]",
		"":            "[??]",
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			if got := dispositionIcon(in); got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

func TestExtractDate_ValidRFC3339(t *testing.T) {
	got := extractDate("2026-06-12T10:00:00Z")
	if got != "2026-06-12" {
		t.Errorf("got %q, want 2026-06-12", got)
	}
}

func TestExtractDate_PreserveFirstTenOnError(t *testing.T) {
	// A plain date isn't valid RFC3339 — should fall back to first 10
	// characters of the string.
	got := extractDate("2026-06-12-something")
	if got != "2026-06-12" {
		t.Errorf("got %q, want 2026-06-12 (first 10 chars)", got)
	}
}

func TestComputeStats_TallyAllDispositions(t *testing.T) {
	entries := []improve.AuditEntry{
		{Disposition: "implemented"},
		{Disposition: "implemented", Error: "minor"},
		{Disposition: "proposed"},
		{Disposition: "aborted"},
		{Disposition: "aborted", Error: "fatal"},
		{Disposition: "something-else"}, // counts in total but not in named buckets
	}
	s := computeStats(entries)
	if s.total != 6 {
		t.Errorf("total = %d, want 6", s.total)
	}
	if s.implemented != 2 {
		t.Errorf("implemented = %d, want 2", s.implemented)
	}
	if s.proposed != 1 {
		t.Errorf("proposed = %d, want 1", s.proposed)
	}
	if s.aborted != 2 {
		t.Errorf("aborted = %d, want 2", s.aborted)
	}
	if s.withErrors != 2 {
		t.Errorf("withErrors = %d, want 2", s.withErrors)
	}
}

func TestComputeStats_Empty(t *testing.T) {
	s := computeStats(nil)
	if s.total != 0 || s.implemented != 0 || s.proposed != 0 || s.aborted != 0 || s.withErrors != 0 {
		t.Errorf("empty entries should yield zero stats; got %+v", s)
	}
}

func TestReverseEntries(t *testing.T) {
	in := []improve.AuditEntry{
		{FindingID: "a"},
		{FindingID: "b"},
		{FindingID: "c"},
	}
	got := reverseEntries(in)
	if len(got) != 3 {
		t.Fatalf("len got %d", len(got))
	}
	if got[0].FindingID != "c" || got[1].FindingID != "b" || got[2].FindingID != "a" {
		t.Errorf("reverse order wrong: %v", got)
	}
	// Confirm input slice unchanged (no in-place mutation).
	if in[0].FindingID != "a" {
		t.Errorf("input mutated: %v", in)
	}
}

func TestReverseEntries_Empty(t *testing.T) {
	got := reverseEntries(nil)
	if got == nil {
		t.Error("reverseEntries(nil) should return non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("len got %d, want 0", len(got))
	}
}

func TestMustJSON_RoundTrip(t *testing.T) {
	type point struct {
		X int `json:"x"`
		Y int `json:"y"`
	}
	got := mustJSON(point{X: 1, Y: 2})
	if !strings.Contains(got, `"x":1`) || !strings.Contains(got, `"y":2`) {
		t.Errorf("got %q, want JSON with x:1, y:2", got)
	}
}

func TestMustJSON_Unmarshalable(t *testing.T) {
	// A channel can't be JSON-marshalled — must return "{}" instead of
	// panicking.
	ch := make(chan int)
	if got := mustJSON(ch); got != "{}" {
		t.Errorf("unmarshalable value should yield {}, got %q", got)
	}
}

func TestAuditDir_ContainsExpectedSuffix(t *testing.T) {
	got := auditDir()
	if !strings.HasSuffix(got, "docs/self-improvement") {
		t.Errorf("got %q, want suffix docs/self-improvement", got)
	}
}
