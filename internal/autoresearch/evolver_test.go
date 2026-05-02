package autoresearch

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

func TestEvolver_OpensPRNeverMerges(t *testing.T) {
	dir := t.TempDir()
	store, _ := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	defer store.Close()

	// Seed bank with one win + one loss.
	now := time.Now()
	appendOutcome(t, store, state.EventExperimentKept, "r1", "e1", "perf", "h1", 90, 100, +10, now)
	appendOutcome(t, store, state.EventExperimentDiscarded, "r1", "e2", "refactor", "h2", 105, 100, -5, now)

	bank := NewHypothesisBank(store)
	gateOps := newFakeGateOps()

	e := &ProgramMDEvolver{
		Client:     scriptedClient{reply: "# new program.md\n\nbe better"},
		Model:      "test",
		Bank:       bank,
		GateOps:    gateOps,
		BaseBranch: "main",
		Events:     store,
		Now:        func() time.Time { return time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC) },
	}

	url, err := e.Evolve(context.Background(), "/repo", "r1", "old program.md content")
	if err != nil {
		t.Fatalf("evolve: %v", err)
	}
	if url == "" {
		t.Fatal("expected non-empty PR url")
	}
	if len(gateOps.prsCreated) != 1 {
		t.Errorf("evolver must open a PR, got %d", len(gateOps.prsCreated))
	}
	if len(gateOps.prsMerged) != 0 {
		t.Errorf("FAIL-CLOSED VIOLATION: evolver must NEVER auto-merge, got %d merges", len(gateOps.prsMerged))
	}
	// Branch convention check.
	if gateOps.createdBranches[0] != "autoresearch/evolve-20260503" {
		t.Errorf("branch name unexpected: %s", gateOps.createdBranches[0])
	}
}

func TestEvolver_NoChange_ReturnsEmptyURL(t *testing.T) {
	dir := t.TempDir()
	store, _ := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	defer store.Close()

	bank := NewHypothesisBank(store)
	gateOps := newFakeGateOps()
	e := &ProgramMDEvolver{
		Client:     scriptedClient{reply: "same content"},
		Model:      "test",
		Bank:       bank,
		GateOps:    gateOps,
		BaseBranch: "main",
		Events:     store,
	}
	url, err := e.Evolve(context.Background(), "/repo", "r1", "same content")
	if err != nil {
		t.Fatalf("evolve: %v", err)
	}
	if url != "" {
		t.Errorf("no-change cycle must return empty URL, got %s", url)
	}
	if len(gateOps.prsCreated) != 0 {
		t.Error("no-change cycle must not open a PR")
	}
}

func TestEvolver_NilClient_Errors(t *testing.T) {
	e := &ProgramMDEvolver{Bank: NewHypothesisBank(nil)}
	if _, err := e.Evolve(context.Background(), "/repo", "r1", "x"); err == nil {
		t.Error("nil LLM client must error")
	}
}
