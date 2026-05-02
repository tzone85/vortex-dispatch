package autoresearch

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

func newMemStore(t *testing.T) *state.FileStore {
	t.Helper()
	dir := t.TempDir()
	store, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("filestore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func appendOutcome(t *testing.T, store state.EventStore, eventType state.EventType, repo, id, class, diffHash string, score, baseline, delta float64, ts time.Time) {
	t.Helper()
	evt := state.Event{
		ID:        id,
		Type:      eventType,
		Timestamp: ts,
		Payload: MarshalForEvent(map[string]any{
			"id":        id,
			"repo":      repo,
			"class":     class,
			"diff_hash": diffHash,
			"score":     score,
			"baseline":  baseline,
			"delta":     delta,
		}),
	}
	if err := store.Append(evt); err != nil {
		t.Fatalf("append: %v", err)
	}
}

func TestHypothesisBank_TopWins_OrderedByAbsoluteDelta(t *testing.T) {
	store := newMemStore(t)
	bank := NewHypothesisBank(store)

	now := time.Now()
	appendOutcome(t, store, state.EventExperimentKept, "r1", "e1", "perf", "h1", 80, 100, +20, now)
	appendOutcome(t, store, state.EventExperimentKept, "r1", "e2", "perf", "h2", 95, 100, +5, now.Add(time.Second))
	appendOutcome(t, store, state.EventExperimentKept, "r1", "e3", "refactor", "h3", 50, 100, +50, now.Add(2*time.Second))

	wins, err := bank.TopWins("r1", 2)
	if err != nil {
		t.Fatalf("topwins: %v", err)
	}
	if len(wins) != 2 {
		t.Fatalf("got %d wins, want 2", len(wins))
	}
	if wins[0].DiffHash != "h3" || wins[1].DiffHash != "h1" {
		t.Errorf("expected ordering h3,h1; got %s,%s", wins[0].DiffHash, wins[1].DiffHash)
	}
}

func TestHypothesisBank_TopLosses_OrderedByRecency(t *testing.T) {
	store := newMemStore(t)
	bank := NewHypothesisBank(store)

	now := time.Now()
	appendOutcome(t, store, state.EventExperimentDiscarded, "r1", "e1", "perf", "h1", 110, 100, -10, now)
	appendOutcome(t, store, state.EventExperimentTripwired, "r1", "e2", "test", "h2", 105, 100, -5, now.Add(time.Hour))

	losses, err := bank.TopLosses("r1", 5)
	if err != nil {
		t.Fatalf("toplosses: %v", err)
	}
	if len(losses) != 2 {
		t.Fatalf("got %d losses, want 2", len(losses))
	}
	if losses[0].DiffHash != "h2" {
		t.Errorf("most recent loss should be first; got %s", losses[0].DiffHash)
	}
}

func TestHypothesisBank_DedupesByDiffHash(t *testing.T) {
	store := newMemStore(t)
	bank := NewHypothesisBank(store)

	now := time.Now()
	// Same diff hash, different IDs — should collapse to one record.
	appendOutcome(t, store, state.EventExperimentDiscarded, "r1", "e1", "perf", "shared", 110, 100, -10, now)
	appendOutcome(t, store, state.EventExperimentKept, "r1", "e2", "perf", "shared", 90, 100, +10, now.Add(time.Second))

	wins, _ := bank.TopWins("r1", 5)
	losses, _ := bank.TopLosses("r1", 5)
	// The kept event came later → final state should be a win, not a loss.
	if len(wins) != 1 || len(losses) != 0 {
		t.Fatalf("dedupe: expected 1 win 0 losses, got %d wins %d losses", len(wins), len(losses))
	}
}

func TestHypothesisBank_FiltersByRepo(t *testing.T) {
	store := newMemStore(t)
	bank := NewHypothesisBank(store)

	now := time.Now()
	appendOutcome(t, store, state.EventExperimentKept, "r1", "e1", "perf", "h1", 90, 100, +10, now)
	appendOutcome(t, store, state.EventExperimentKept, "r2", "e2", "perf", "h2", 80, 100, +20, now)

	r1Wins, _ := bank.TopWins("r1", 5)
	r2Wins, _ := bank.TopWins("r2", 5)
	if len(r1Wins) != 1 || r1Wins[0].DiffHash != "h1" {
		t.Errorf("r1 should only see its own win, got %+v", r1Wins)
	}
	if len(r2Wins) != 1 || r2Wins[0].DiffHash != "h2" {
		t.Errorf("r2 should only see its own win, got %+v", r2Wins)
	}
}

func TestHypothesisBank_SeenDiff(t *testing.T) {
	store := newMemStore(t)
	bank := NewHypothesisBank(store)

	now := time.Now()
	appendOutcome(t, store, state.EventExperimentKept, "r1", "e1", "perf", "abc123", 90, 100, +10, now)

	seen, err := bank.SeenDiff("abc123")
	if err != nil {
		t.Fatalf("seendiff: %v", err)
	}
	if !seen {
		t.Error("abc123 was just appended, must be seen")
	}
	notSeen, _ := bank.SeenDiff("def456")
	if notSeen {
		t.Error("def456 was never appended, must not be seen")
	}
	empty, _ := bank.SeenDiff("")
	if empty {
		t.Error("empty diff hash must not be seen")
	}
}

func TestHashDiff_Stable(t *testing.T) {
	a := HashDiff("hello world")
	b := HashDiff("hello world")
	if a != b {
		t.Errorf("HashDiff must be deterministic, got %s vs %s", a, b)
	}
	c := HashDiff("hello world ")
	if a == c {
		t.Error("HashDiff must differentiate by content")
	}
}
