//go:build !windows

package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestCompletionGate_CancelledContext_ReturnsErrorNotVerdict: the monitor
// must be told there is no verdict, so it neither completes nor blocks the
// requirement. The cancel lands while the shimmed suite is running.
func TestCompletionGate_CancelledContext_ReturnsErrorNotVerdict(t *testing.T) {
	marker := hangingRunner(t)
	dir := goModule(t)
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# m\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	g := NewCompletionGate(nil, "test-model", 1000, 1, "main", nil, nil)
	g.pull = func(_, _ string) {}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type result struct {
		passed bool
		err    error
	}
	done := make(chan result, 1)
	go func() {
		p, err := g.Run(ctx, "REQ-CANCEL", dir)
		done <- result{p, err}
	}()
	awaitMarker(t, marker)
	cancelledAt := time.Now()
	cancel()
	r := <-done
	// The group kill itself is timed strictly in TestCheckTests_CancelledContext_NoVerdict;
	// here the assertion is that the cancel propagates through the gate and
	// beats the WaitDelay fallback with room for a slow runner.
	if elapsed := time.Since(cancelledAt); elapsed >= 2*verifyWaitDelay {
		t.Fatalf("Run took %s after cancel — the cancel did not propagate", elapsed)
	}
	if r.err == nil {
		t.Fatal("Run must report the aborted run as an error (no verdict)")
	}
	if r.passed {
		t.Fatal("an aborted run must never certify completion")
	}
}
