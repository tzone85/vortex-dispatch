package autoresearch

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/state"
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

func TestNewBayesSamplerFromStore_RestoresKeptAndDiscardedAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	store, err := state.NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []state.Event{
		state.NewEvent(state.EventExperimentKept, "autoresearch", "", map[string]any{
			"repo": "repo-a", "class": string(ClassPerf),
		}),
		state.NewEvent(state.EventExperimentDiscarded, "autoresearch", "", map[string]any{
			"repo": "repo-a", "class": string(ClassPerf),
		}),
		state.NewEvent(state.EventExperimentKept, "autoresearch", "", map[string]any{
			"repo": "repo-b", "class": string(ClassRefactor),
		}),
	} {
		if err := store.Append(event); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := state.NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	sampler, err := NewBayesSamplerFromStore(reopened)
	if err != nil {
		t.Fatal(err)
	}
	if alpha, beta := sampler.Posterior("repo-a", ClassPerf); alpha != 2 || beta != 2 {
		t.Fatalf("repo-a/perf posterior = (%v, %v), want (2, 2)", alpha, beta)
	}
	if alpha, beta := sampler.Posterior("repo-b", ClassRefactor); alpha != 2 || beta != 1 {
		t.Fatalf("repo-b/refactor posterior = (%v, %v), want (2, 1)", alpha, beta)
	}
}

func TestNewBayesSamplerFromStore_RestoresAgentFailuresAndTripwires(t *testing.T) {
	store := newMemStore(t)
	for _, event := range []state.Event{
		state.NewEvent(state.EventExperimentFailed, "autoresearch", "", map[string]any{
			"repo": "repo", "class": string(ClassTest), "infra_caused": false,
		}),
		state.NewEvent(state.EventExperimentTripwired, "autoresearch", "", map[string]any{
			"repo": "repo", "class": string(ClassTest), "reason": "scope",
		}),
		// The runner also emits a non-terminal tripwire observation without a
		// class before a kept/discarded terminal event. It must not be counted.
		state.NewEvent(state.EventExperimentTripwired, "autoresearch", "", map[string]any{
			"repo": "repo", "verdict": string(VerdictOK),
		}),
	} {
		if err := store.Append(event); err != nil {
			t.Fatal(err)
		}
	}

	sampler, err := NewBayesSamplerFromStore(store)
	if err != nil {
		t.Fatal(err)
	}
	if alpha, beta := sampler.Posterior("repo", ClassTest); alpha != 1 || beta != 3 {
		t.Fatalf("repo/test posterior = (%v, %v), want (1, 3)", alpha, beta)
	}
}

func TestNewBayesSamplerFromStore_SkipsInfraMalformedAndIrrelevantEvents(t *testing.T) {
	store := newMemStore(t)
	for _, event := range []state.Event{
		state.NewEvent(state.EventExperimentFailed, "autoresearch", "", map[string]any{
			"repo": "repo", "class": string(ClassPerf), "infra_caused": true,
		}),
		state.NewEvent(state.EventExperimentFailed, "autoresearch", "", map[string]any{
			"repo": "repo", "class": string(ClassPerf), "infra_caused": "true",
		}),
		state.NewEvent(state.EventExperimentKept, "other-subsystem", "", map[string]any{
			"repo": "repo", "class": string(ClassPerf),
		}),
		state.NewEvent(state.EventExperimentRunning, "autoresearch", "", map[string]any{
			"repo": "repo", "class": string(ClassPerf),
		}),
		{Type: state.EventExperimentDiscarded, Payload: []byte("not-json")},
		state.NewEvent(state.EventExperimentKept, "autoresearch", "", map[string]any{
			"repo": "", "class": string(ClassPerf),
		}),
		state.NewEvent(state.EventExperimentDiscarded, "autoresearch", "", map[string]any{
			"repo": "repo", "class": "not-a-class",
		}),
	} {
		if err := store.Append(event); err != nil {
			t.Fatal(err)
		}
	}

	sampler, err := NewBayesSamplerFromStore(store)
	if err != nil {
		t.Fatal(err)
	}
	if alpha, beta := sampler.Posterior("repo", ClassPerf); alpha != 1 || beta != 1 {
		t.Fatalf("repo/perf posterior = (%v, %v), want unchanged (1, 1)", alpha, beta)
	}
	if alpha, beta := sampler.Posterior("repo", ExperimentClass("not-a-class")); alpha != 1 || beta != 1 {
		t.Fatalf("unknown class posterior = (%v, %v), want unchanged (1, 1)", alpha, beta)
	}
}

func TestNewBayesSamplerFromStore_ReturnsListError(t *testing.T) {
	want := errors.New("list failed")
	sampler, err := NewBayesSamplerFromStore(errorEventStore{err: want})
	if sampler != nil {
		t.Fatal("sampler should be nil when replay fails")
	}
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}

func TestNewBayesSamplerFromStore_RejectsNilStore(t *testing.T) {
	sampler, err := NewBayesSamplerFromStore(nil)
	if sampler != nil || err == nil {
		t.Fatalf("got sampler=%v error=%v, want nil sampler and error", sampler, err)
	}
}

type errorEventStore struct{ err error }

func (s errorEventStore) Append(state.Event) error                      { return nil }
func (s errorEventStore) List(state.EventFilter) ([]state.Event, error) { return nil, s.err }
func (s errorEventStore) Count(state.EventFilter) (int, error)          { return 0, nil }
func (s errorEventStore) Close() error                                  { return nil }
