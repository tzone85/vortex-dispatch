package engine

import "testing"

// The pre-merge gate must keep a green base green WITHOUT deadlocking when the
// base is already red (a pre-existing failure must not be blamed on every
// subsequent story).
func TestPreMergeDecision(t *testing.T) {
	pass := QAResult{Passed: true}
	failLint := QAResult{Passed: false, Checks: []QACheckResult{{Name: "lint", Passed: false, Output: "F401"}}}

	// Base perpetually red on lint (green on build+test). A story that then
	// breaks the build must STILL be blocked — the lint debt on base is not
	// what turned build red. This is the per-check-attribution case that the
	// old aggregate `!base.Passed` comparison silently let through.
	baseRedLintOnly := QAResult{Passed: false, Checks: []QACheckResult{
		{Name: "lint", Passed: false},
		{Name: "build", Passed: true},
		{Name: "test", Passed: true},
	}}
	rebasedLintAndBuildRed := QAResult{Passed: false, Checks: []QACheckResult{
		{Name: "lint", Passed: false},
		{Name: "build", Passed: false, Output: "undefined: Foo"},
		{Name: "test", Passed: true},
	}}
	// Same base, but the story only carries the pre-existing lint failure —
	// nothing new went red, so it must be allowed (no deadlock).
	rebasedLintOnly := QAResult{Passed: false, Checks: []QACheckResult{
		{Name: "lint", Passed: false},
		{Name: "build", Passed: true},
		{Name: "test", Passed: true},
	}}

	cases := []struct {
		name      string
		rebased   QAResult
		base      QAResult
		wantBlock bool
	}{
		{"rebased passes → allow", pass, pass, false},
		{"rebased passes even if base red → allow", pass, failLint, false},
		{"story turns green base red → BLOCK", failLint, pass, true},
		{"base already red → allow (no deadlock)", failLint, failLint, false},
		{"base red on lint, story breaks build → BLOCK", rebasedLintAndBuildRed, baseRedLintOnly, true},
		{"base red on lint, story only carries lint → allow", rebasedLintOnly, baseRedLintOnly, false},
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
