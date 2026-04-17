package improve

import "testing"

func TestClampScore_WithinRange(t *testing.T) {
	for i := 0; i <= 10; i++ {
		if got := clampScore(i); got != i {
			t.Errorf("clampScore(%d) = %d, want %d", i, got, i)
		}
	}
}

func TestClampScore_Negative(t *testing.T) {
	if got := clampScore(-5); got != 0 {
		t.Errorf("clampScore(-5) = %d, want 0", got)
	}
}

func TestClampScore_TooHigh(t *testing.T) {
	if got := clampScore(1000); got != 10 {
		t.Errorf("clampScore(1000) = %d, want 10", got)
	}
}

func TestComputeRank_ClampedScores(t *testing.T) {
	opp := Opportunity{
		RelevanceScore: 10,
		BudgetScore:    10,
		WinProbability: 10,
	}
	want := (10 * 3) + (10 * 2) + 10 // 60 — max possible
	if got := ComputeRank(opp); got != want {
		t.Errorf("ComputeRank = %d, want %d", got, want)
	}
}
