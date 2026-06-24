package engine

import "testing"

// The pre-merge gate must keep a green base green WITHOUT deadlocking when the
// base is already red (a pre-existing failure must not be blamed on every
// subsequent story).
func TestPreMergeDecision(t *testing.T) {
	pass := QAResult{Passed: true}
	failLint := QAResult{Passed: false, Checks: []QACheckResult{{Name: "lint", Passed: false, Output: "F401"}}}

	cases := []struct {
		name        string
		rebased     QAResult
		base        QAResult
		wantBlock   bool
	}{
		{"rebased passes → allow", pass, pass, false},
		{"rebased passes even if base red → allow", pass, failLint, false},
		{"story turns green base red → BLOCK", failLint, pass, true},
		{"base already red → allow (no deadlock)", failLint, failLint, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			block, reason := preMergeDecision(tc.rebased, tc.base)
			if block != tc.wantBlock {
				t.Errorf("block=%v want %v (reason=%q)", block, tc.wantBlock, reason)
			}
			if block && reason == "" {
				t.Error("a block must carry a reason")
			}
		})
	}
}
