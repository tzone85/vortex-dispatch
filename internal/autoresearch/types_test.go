package autoresearch

import "testing"

func TestScoreImproves_LowerIsBetter(t *testing.T) {
	s := Score{Final: 100, LowerIsBetter: true}
	if !s.Improves(110) {
		t.Error("100 vs baseline 110 (lower-better) must be improvement")
	}
	if s.Improves(100) {
		t.Error("equal must not improve")
	}
	if s.Improves(90) {
		t.Error("100 vs 90 (lower-better) must not improve")
	}
}

func TestScoreImproves_HigherIsBetter(t *testing.T) {
	s := Score{Final: 90, LowerIsBetter: false}
	if !s.Improves(80) {
		t.Error("90 vs 80 (higher-better) must improve")
	}
	if s.Improves(95) {
		t.Error("90 vs 95 (higher-better) must not improve")
	}
}

func TestIsAgentCausedFailure(t *testing.T) {
	cases := []struct {
		reason string
		infra  bool
		want   bool
	}{
		{"timeout", false, true},
		{"no_diff", false, true},
		{"scope", false, true},
		{"infra", false, false},
		{"worktree_create", false, false},
		{"tmux_start", false, false},
		{"provider_outage", false, false},
		{"timeout", true, false},  // explicit infra flag overrides reason
		{"anything", true, false}, // explicit infra flag wins
	}
	for _, c := range cases {
		got := IsAgentCausedFailure(c.reason, c.infra)
		if got != c.want {
			t.Errorf("IsAgentCausedFailure(%q, %v) = %v, want %v", c.reason, c.infra, got, c.want)
		}
	}
}
