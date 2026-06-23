package engine

import "testing"

// TestStoryIDPrefix_UniquePerRequirement pins the collision boundary that
// caused `vxd estimate` to crash with "UNIQUE constraint failed: stories.id":
// every estimate reqID ("est-YYYYMMDD-...") was truncated to its first 8 chars
// ("est-2026"), so all estimates in a calendar year shared a story-ID prefix.
func TestStoryIDPrefix_UniquePerRequirement(t *testing.T) {
	// Short IDs (≤8 chars, e.g. test fixtures) are used verbatim for readability.
	if got := storyIDPrefix("r-001"); got != "r-001" {
		t.Fatalf("short reqID should be verbatim, got %q", got)
	}
	if got := storyIDPrefix("12345678"); got != "12345678" {
		t.Fatalf("8-char reqID should be verbatim, got %q", got)
	}

	// Two estimate reqIDs in the same year previously both truncated to
	// "est-2026" and collided. They must now differ.
	a := storyIDPrefix("est-20260623-150405")
	b := storyIDPrefix("est-20260101-000000")
	if a == b {
		t.Fatalf("distinct reqIDs collided on prefix: %q", a)
	}

	// Two ULID reqIDs created within the same ~256ms window share their leading
	// timestamp bits; the prefix must still distinguish them (entropy lives in
	// the trailing characters of a ULID).
	u1 := storyIDPrefix("01JZ8ABCDEFGHJKMNPQRSTVWX1")
	u2 := storyIDPrefix("01JZ8ABCDEFGHJKMNPQRSTVWX2")
	if u1 == u2 {
		t.Fatalf("near-simultaneous ULID reqIDs collided on prefix: %q", u1)
	}

	// Deterministic: same reqID -> same prefix (one plan must reuse it).
	if storyIDPrefix("est-20260623-150405") != a {
		t.Fatal("prefix not deterministic for identical reqID")
	}

	// Prefix stays short for long reqIDs.
	if len(a) > 8 {
		t.Fatalf("prefix should be ≤8 chars, got %d (%q)", len(a), a)
	}
}
