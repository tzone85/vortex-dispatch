package engine

import (
	"strings"
	"testing"
)

func TestPauseResumeHint(t *testing.T) {
	cases := []struct {
		reason string
		want   string // substring expected in the hint ("" = no hint)
	}{
		{"fatal API error: insufficient_quota", "credit/billing"},
		{"review error: context deadline exceeded", "timed out"},
		{"merge/rebase error: claude CLI error: 500 Internal server error", "outage"},
		{"openURL connection refused", "outage"},
		{"reviewer LLM call: model claude-sonnet-4-20250514 may not exist or you may not have access", "model ID invalid"},
		{"401 unauthorized", "auth failure"},
		{"story exhausted all escalation tiers (3): review rejected: bad code", ""},
	}
	for _, c := range cases {
		got := pauseResumeHint(c.reason)
		if c.want == "" {
			if got != "" {
				t.Errorf("reason %q: expected no hint, got %q", c.reason, got)
			}
			continue
		}
		if got == "" {
			t.Errorf("reason %q: expected a hint containing %q, got none", c.reason, c.want)
			continue
		}
		if !strings.Contains(got, c.want) {
			t.Errorf("reason %q: hint %q does not contain %q", c.reason, got, c.want)
		}
	}
}
