package llm_test

import (
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/llm"
)

// TestLooksLikeLeakedCapacityNotice pins the boundary between a leaked
// session-limit / capacity NOTICE (which must be caught so it is never written
// into a resolved file) and a legitimately resolved source file that merely
// CONTAINS a generic capacity phrase (which must NOT be misclassified as a
// synthetic 429 — the false positive that wedged conflict resolution).
func TestLooksLikeLeakedCapacityNotice(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		// --- Leaked notices: MUST be caught ---
		{"session limit banner", "You've hit your session limit · resets 3pm. Please try again later.", true},
		{"usage limit banner", "You've hit your usage limit, resets at midnight.", true},
		{"api error envelope untyped", `{"type":"error","error":{"message":"x","api_error_status":429}}`, true},
		{"api error envelope spaced", `{"api_error_status": 529}`, true},
		{"short rate-limit blurb", "Error: rate limit exceeded, retry later", true},
		{"short overloaded blurb", "the service is overloaded", true},

		// --- Legitimate resolved files: MUST NOT be flagged ---
		{
			"long file containing 'rate limit'",
			"package ratelimit\n\n// Limiter enforces a rate limit per client.\n" +
				strings.Repeat("// token bucket refill and burst handling logic here.\n", 40) +
				"func (l *Limiter) Allow() bool { return l.tokens > 0 }\n",
			false,
		},
		{
			"long file containing 'connection refused'",
			"package net\n\n// retry wraps a dial and classifies errors.\n" +
				strings.Repeat("// e.g. \"connection refused\", \"connection reset\", \"i/o timeout\".\n", 40) +
				"func classify(err error) string { return err.Error() }\n",
			false,
		},
		{
			"long file containing 'overloaded' and 'service unavailable'",
			"package health\n" +
				strings.Repeat("// returns 503 service unavailable when the pool is overloaded.\n", 40),
			false,
		},

		// --- Neither ---
		{"empty", "", false},
		{"ordinary short code", "func Add(a, b int) int { return a + b }", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := llm.LooksLikeLeakedCapacityNotice(tc.content); got != tc.want {
				t.Errorf("LooksLikeLeakedCapacityNotice(%.60q...) = %v, want %v", tc.content, got, tc.want)
			}
		})
	}
}
