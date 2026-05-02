package autoresearch

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

// HypothesisBank reads experiment events and surfaces top-K wins/losses for
// prompt-seeding. It is a pure projection — no state of its own. Each call
// re-reads the event log via the EventStore interface.
//
// Dedupe: experiments are keyed on diff content-hash so the same change
// proposed twice (re-derived prompts, agent retries) collapses to one entry.
type HypothesisBank struct {
	store state.EventStore
}

// NewHypothesisBank constructs a bank backed by the given event store.
func NewHypothesisBank(store state.EventStore) *HypothesisBank {
	return &HypothesisBank{store: store}
}

// HashDiff returns the canonical diff hash used as Experiment.DiffHash.
// Stable across processes and never includes timestamps or paths.
func HashDiff(diff string) string {
	sum := sha256.Sum256([]byte(diff))
	return hex.EncodeToString(sum[:])
}

// HashPrompt returns the prompt-seed hash recorded in EXPERIMENT_PROPOSED.
func HashPrompt(prompt string) string {
	sum := sha256.Sum256([]byte(prompt))
	return hex.EncodeToString(sum[:])
}

// experimentsByDiff returns a deduped map of completed experiments
// for the given repo, keyed on diff hash. Later events override earlier
// ones for the same diff (so a kept experiment supersedes its proposed
// record).
func (b *HypothesisBank) experimentsByDiff(repo string) (map[string]Experiment, error) {
	events, err := b.store.List(state.EventFilter{})
	if err != nil {
		return nil, err
	}
	out := make(map[string]Experiment)
	for _, evt := range events {
		exp, ok := experimentFromEvent(evt, repo)
		if !ok {
			continue
		}
		if exp.DiffHash == "" {
			continue
		}
		out[exp.DiffHash] = exp
	}
	return out, nil
}

// TopWins returns up to k kept experiments, sorted by absolute delta
// (largest improvement first).
func (b *HypothesisBank) TopWins(repo string, k int) ([]Experiment, error) {
	byDiff, err := b.experimentsByDiff(repo)
	if err != nil {
		return nil, err
	}
	var wins []Experiment
	for _, e := range byDiff {
		if e.Kept {
			wins = append(wins, e)
		}
	}
	sort.Slice(wins, func(i, j int) bool {
		return absFloat(wins[i].Delta) > absFloat(wins[j].Delta)
	})
	if k > 0 && len(wins) > k {
		wins = wins[:k]
	}
	return wins, nil
}

// TopLosses returns up to k discarded/tripwired experiments, sorted by
// most-recent first. Losses are most useful as "do not try this again"
// signals, where recency matters more than magnitude.
func (b *HypothesisBank) TopLosses(repo string, k int) ([]Experiment, error) {
	byDiff, err := b.experimentsByDiff(repo)
	if err != nil {
		return nil, err
	}
	var losses []Experiment
	for _, e := range byDiff {
		if !e.Kept && e.Verdict != "" {
			losses = append(losses, e)
		}
	}
	sort.Slice(losses, func(i, j int) bool {
		return losses[i].Timestamp.After(losses[j].Timestamp)
	})
	if k > 0 && len(losses) > k {
		losses = losses[:k]
	}
	return losses, nil
}

// SeenDiff reports whether a diff with this hash has already been
// recorded for any repo. Used to skip near-duplicate proposals.
func (b *HypothesisBank) SeenDiff(hash string) (bool, error) {
	if hash == "" {
		return false, nil
	}
	events, err := b.store.List(state.EventFilter{})
	if err != nil {
		return false, err
	}
	for _, evt := range events {
		if !isAutoresearchEvent(evt.Type) {
			continue
		}
		payload := state.DecodePayload(evt.Payload)
		if h, ok := payload["diff_hash"].(string); ok && h == hash {
			return true, nil
		}
	}
	return false, nil
}

// experimentFromEvent decodes a single event into the Experiment shape.
// Returns ok=false if the event is irrelevant (wrong repo, wrong kind, etc.).
func experimentFromEvent(evt state.Event, repo string) (Experiment, bool) {
	if !isOutcomeEvent(evt.Type) {
		return Experiment{}, false
	}
	payload := state.DecodePayload(evt.Payload)
	r, _ := payload["repo"].(string)
	if repo != "" && r != repo {
		return Experiment{}, false
	}
	exp := Experiment{
		Repo:      r,
		Timestamp: evt.Timestamp,
	}
	if id, ok := payload["id"].(string); ok {
		exp.ID = id
	}
	if c, ok := payload["class"].(string); ok {
		exp.Class = ExperimentClass(c)
	}
	if h, ok := payload["diff_hash"].(string); ok {
		exp.DiffHash = h
	}
	if v, ok := payload["score"].(float64); ok {
		exp.Score = v
	}
	if v, ok := payload["baseline"].(float64); ok {
		exp.Baseline = v
	}
	if v, ok := payload["delta"].(float64); ok {
		exp.Delta = v
	}
	if v, ok := payload["verdict"].(string); ok {
		exp.Verdict = Verdict(v)
	}
	if v, ok := payload["fail_reason"].(string); ok {
		exp.FailReason = v
	}
	switch evt.Type {
	case state.EventExperimentKept:
		exp.Kept = true
		if exp.Verdict == "" {
			exp.Verdict = VerdictOK
		}
	case state.EventExperimentDiscarded:
		exp.Kept = false
		if exp.Verdict == "" {
			exp.Verdict = VerdictRejected
		}
	case state.EventExperimentTripwired:
		exp.Kept = false
		if exp.Verdict == "" {
			exp.Verdict = VerdictSuspicious
		}
	case state.EventExperimentFailed:
		exp.Kept = false
		if exp.Verdict == "" {
			exp.Verdict = VerdictRejected
		}
	}
	return exp, true
}

// isOutcomeEvent identifies the events that contribute to win/loss tallies.
func isOutcomeEvent(t state.EventType) bool {
	switch t {
	case state.EventExperimentKept,
		state.EventExperimentDiscarded,
		state.EventExperimentTripwired,
		state.EventExperimentFailed:
		return true
	}
	return false
}

// isAutoresearchEvent is the broader filter (any autoresearch-namespace event).
func isAutoresearchEvent(t state.EventType) bool {
	switch t {
	case state.EventBaselineMeasured,
		state.EventExperimentProposed,
		state.EventExperimentRunning,
		state.EventExperimentMeasured,
		state.EventExperimentTiebroken,
		state.EventExperimentTripwired,
		state.EventExperimentKept,
		state.EventExperimentDiscarded,
		state.EventExperimentFailed,
		state.EventCoordinatorPanic,
		state.EventProgrammdEvolved:
		return true
	}
	return false
}

// MarshalForEvent serializes a payload map for an event in a stable form.
// Helper used by emitters; centralized so the on-the-wire format is consistent.
func MarshalForEvent(m map[string]any) []byte {
	b, _ := json.Marshal(m)
	return b
}

func absFloat(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
