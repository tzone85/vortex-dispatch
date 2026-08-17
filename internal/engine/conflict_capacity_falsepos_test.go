package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/llm"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// TestRebaseWithResolution_LegitFileWithCapacityPhraseIsNotMisread pins the fix
// for the capacity-guard false positive: a correctly resolved conflict whose
// merged content legitimately contains generic capacity phrases ("rate limit",
// "connection refused", "service unavailable", "overloaded") must NOT be
// misclassified as a leaked session-limit notice. Before the fix, the resolver
// scanned the WHOLE resolved file with ContainsCapacitySignature, returned a
// synthetic 429, aborted the rebase, and paused the requirement with a false
// "wait for the limit to reset" message — which recurs deterministically on
// resume, wedging the pipeline.
func TestRebaseWithResolution_LegitFileWithCapacityPhraseIsNotMisread(t *testing.T) {
	_, worktreeDir := setupDivergentRepos(t, true)

	// A realistic merged feature.go: valid Go, no conflict markers, no resolver
	// chatter, >512 bytes, and it legitimately mentions several capacity phrases
	// the broad detector would have tripped on.
	resolvedContent := "package main\n\nimport \"fmt\"\n\n" +
		"// Feature applies the rate limit policy and classifies transport errors.\n" +
		"// A client that exceeds the rate limit is rejected; retryable transport\n" +
		"// failures (\"connection refused\", \"connection reset\", \"service unavailable\",\n" +
		"// upstream \"overloaded\") are surfaced so callers can back off.\n" +
		strings.Repeat("// policy detail line kept from both sides of the merge.\n", 12) +
		"func Feature() {\n\tfmt.Println(\"merged from both sides — rate limit enforced\")\n}\n"

	if len(resolvedContent) <= 512 {
		t.Fatalf("test setup: resolved content must exceed the leaked-notice length bound, got %d bytes", len(resolvedContent))
	}

	replayClient := llm.NewReplayClient(llm.CompletionResponse{Content: resolvedContent})

	dir := t.TempDir()
	es, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("create event store: %v", err)
	}
	defer es.Close()

	cr := NewConflictResolver(replayClient, "test-model", nil, "", 4096, nil, es)

	if err := cr.RebaseWithResolution(context.Background(), "s-falsepos", worktreeDir, "origin/main"); err != nil {
		if llm.IsCapacityError(err) {
			t.Fatalf("legit resolved file was misclassified as a capacity error: %v", err)
		}
		t.Fatalf("expected conflict to resolve cleanly, got: %v", err)
	}

	// The resolved content must have been written to the file, not discarded.
	got, rErr := os.ReadFile(filepath.Join(worktreeDir, "feature.go"))
	if rErr != nil {
		t.Fatal(rErr)
	}
	if !strings.Contains(string(got), "rate limit enforced") {
		t.Errorf("resolved content was not written to feature.go:\n%s", got)
	}
}
