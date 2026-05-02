package autoresearch

import (
	"testing"
)

func TestBayesSampler_DefaultClasses(t *testing.T) {
	s := NewBayesSampler(nil, 0, 0)
	got := s.Classes()
	if len(got) != len(DefaultClasses) {
		t.Fatalf("expected %d default classes, got %d", len(DefaultClasses), len(got))
	}
}

func TestBayesSampler_PriorsDefaultToOne(t *testing.T) {
	s := NewBayesSampler(nil, 0, 0)
	a, b := s.Posterior("repo", ClassPerf)
	if a != 1.0 || b != 1.0 {
		t.Errorf("zero-input priors should default to 1.0, got α=%v β=%v", a, b)
	}
}

func TestBayesSampler_UpdateMutatesAlphaOnSuccess(t *testing.T) {
	s := NewBayesSampler(nil, 1, 1)
	s.Update("repo", ClassPerf, true)
	a, b := s.Posterior("repo", ClassPerf)
	if a != 2 || b != 1 {
		t.Errorf("kept=true should bump α only; got α=%v β=%v", a, b)
	}
}

func TestBayesSampler_UpdateMutatesBetaOnFailure(t *testing.T) {
	s := NewBayesSampler(nil, 1, 1)
	s.Update("repo", ClassPerf, false)
	a, b := s.Posterior("repo", ClassPerf)
	if a != 1 || b != 2 {
		t.Errorf("kept=false should bump β only; got α=%v β=%v", a, b)
	}
}

func TestBayesSampler_PriorsIsolatedPerClass(t *testing.T) {
	s := NewBayesSampler(nil, 1, 1)
	s.Update("repo", ClassPerf, true)
	s.Update("repo", ClassPerf, true)
	s.Update("repo", ClassRefactor, false)

	pa, pb := s.Posterior("repo", ClassPerf)
	ra, rb := s.Posterior("repo", ClassRefactor)
	if pa != 3 || pb != 1 {
		t.Errorf("perf prior wrong: α=%v β=%v", pa, pb)
	}
	if ra != 1 || rb != 2 {
		t.Errorf("refactor prior wrong: α=%v β=%v", ra, rb)
	}
}

func TestBayesSampler_PriorsIsolatedPerRepo(t *testing.T) {
	s := NewBayesSampler(nil, 1, 1)
	s.Update("r1", ClassPerf, true)
	s.Update("r2", ClassPerf, false)

	a1, b1 := s.Posterior("r1", ClassPerf)
	a2, b2 := s.Posterior("r2", ClassPerf)
	if a1 != 2 || b1 != 1 {
		t.Errorf("r1 perf prior wrong: α=%v β=%v", a1, b1)
	}
	if a2 != 1 || b2 != 2 {
		t.Errorf("r2 perf prior wrong: α=%v β=%v", a2, b2)
	}
}

func TestBayesSampler_MeanReflectsKeptRate(t *testing.T) {
	s := NewBayesSampler(nil, 1, 1)
	for i := 0; i < 8; i++ {
		s.Update("r", ClassPerf, true)
	}
	for i := 0; i < 2; i++ {
		s.Update("r", ClassPerf, false)
	}
	mean := s.Mean("r", ClassPerf)
	// α=9, β=3 → mean = 9/12 = 0.75
	if mean < 0.7 || mean > 0.8 {
		t.Errorf("posterior mean ~0.75 expected, got %v", mean)
	}
}

func TestBayesSampler_NextPrefersHighSuccessClass(t *testing.T) {
	// Stack the deck heavily for ClassPerf and run many draws.
	// Expectation: ClassPerf wins the majority of Thompson samples.
	s := NewBayesSampler([]ExperimentClass{ClassPerf, ClassRefactor}, 1, 1)
	s.SetSeed(42)
	for i := 0; i < 50; i++ {
		s.Update("r", ClassPerf, true)
	}
	for i := 0; i < 50; i++ {
		s.Update("r", ClassRefactor, false)
	}
	counts := map[ExperimentClass]int{}
	for i := 0; i < 1000; i++ {
		counts[s.Next("r")]++
	}
	if counts[ClassPerf] <= counts[ClassRefactor] {
		t.Errorf("after 50 wins for perf and 50 losses for refactor, perf should dominate Thompson sampling; counts=%v", counts)
	}
	if counts[ClassPerf] < 950 {
		t.Errorf("perf should overwhelmingly dominate (>950/1000), got %d", counts[ClassPerf])
	}
}

func TestBayesSampler_NextDeterministicWithSeed(t *testing.T) {
	s1 := NewBayesSampler(nil, 1, 1)
	s1.SetSeed(7)
	s2 := NewBayesSampler(nil, 1, 1)
	s2.SetSeed(7)

	for i := 0; i < 20; i++ {
		if s1.Next("r") != s2.Next("r") {
			t.Errorf("same seed should produce same Next sequence; diverged at step %d", i)
			break
		}
	}
}
