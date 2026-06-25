package engine_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/engine"
	"github.com/tzone85/vortex-dispatch/internal/llm"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

func TestPlanner_RejectsDanglingDependsOn(t *testing.T) {
	dir := t.TempDir()
	es, err := state.NewFileStore(dir + "/events.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer es.Close()
	ps, err := state.NewSQLiteStore(dir + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer ps.Close()

	// s-002 depends on s-999, which doesn't exist
	resp := llm.CompletionResponse{
		Content: `[
			{"id":"s-001","title":"Setup","description":"x","acceptance_criteria":"x","complexity":1,"depends_on":[],"owned_files":["a.go"],"wave_hint":"parallel"},
			{"id":"s-002","title":"Feature","description":"x","acceptance_criteria":"x","complexity":2,"depends_on":["s-999"],"owned_files":["b.go"],"wave_hint":"parallel"}
		]`,
	}
	p := engine.NewPlanner(llm.NewReplayClient(resp), config.DefaultConfig(), es, ps)

	_, err = p.Plan(context.Background(), "req-dang", "Build feature", t.TempDir())
	if err == nil {
		t.Fatal("expected error for dangling depends_on reference")
	}
	if !strings.Contains(err.Error(), "depends_on") {
		t.Errorf("error should mention depends_on: %v", err)
	}
}

func TestPlanner_RejectsDuplicateStoryIDs(t *testing.T) {
	dir := t.TempDir()
	es, err := state.NewFileStore(dir + "/events.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer es.Close()
	ps, err := state.NewSQLiteStore(dir + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer ps.Close()

	// Two stories with the same ID
	resp := llm.CompletionResponse{
		Content: `[
			{"id":"s-001","title":"First","description":"x","acceptance_criteria":"x","complexity":1,"depends_on":[],"owned_files":["a.go"],"wave_hint":"parallel"},
			{"id":"s-001","title":"Duplicate","description":"x","acceptance_criteria":"x","complexity":2,"depends_on":[],"owned_files":["b.go"],"wave_hint":"parallel"}
		]`,
	}
	p := engine.NewPlanner(llm.NewReplayClient(resp), config.DefaultConfig(), es, ps)

	_, err = p.Plan(context.Background(), "req-dup", "Build feature", t.TempDir())
	if err == nil {
		t.Fatal("expected error for duplicate story IDs")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error should mention duplicate: %v", err)
	}
}

func TestPlanner_AcceptsValidDependencies(t *testing.T) {
	dir := t.TempDir()
	es, err := state.NewFileStore(dir + "/events.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer es.Close()
	ps, err := state.NewSQLiteStore(dir + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer ps.Close()

	// s-002 depends on s-001 — valid
	resp := llm.CompletionResponse{
		Content: `[
			{"id":"s-001","title":"Setup","description":"x","acceptance_criteria":"x","complexity":1,"depends_on":[],"owned_files":["a.go"],"wave_hint":"parallel"},
			{"id":"s-002","title":"Feature","description":"x","acceptance_criteria":"x","complexity":2,"depends_on":["s-001"],"owned_files":["b.go"],"wave_hint":"parallel"}
		]`,
	}
	cfg := config.DefaultConfig()
	cfg.Planning.EmitScribeStory = false
	cfg.Planning.EmitIntegrationStory = false
	p := engine.NewPlanner(llm.NewReplayClient(resp), cfg, es, ps)

	result, err := p.Plan(context.Background(), "req-valid", "Build feature", t.TempDir())
	if err != nil {
		t.Fatalf("valid dependencies should succeed: %v", err)
	}
	if len(result.Stories) != 2 {
		t.Errorf("stories = %d, want 2", len(result.Stories))
	}
}
