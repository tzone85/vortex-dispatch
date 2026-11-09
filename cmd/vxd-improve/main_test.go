package main

import (
	"context"
	"testing"
	"time"
)

// TestBoundSelfAudit_HasDeadline is a regression guard for the unattended daily
// run: Phase-0 self-audit/self-heal must run under a bounded context so a
// stalled network call (govulncheck's vuln DB fetch, a hung self-heal
// subprocess) cannot hang the cron forever. Previously main() passed
// context.Background() straight through, which carries no deadline.
func TestBoundSelfAudit_HasDeadline(t *testing.T) {
	ctx, cancel := boundSelfAudit(context.Background())
	defer cancel()

	dl, ok := ctx.Deadline()
	if !ok {
		t.Fatal("self-audit context must carry a deadline; got an unbounded context")
	}
	remaining := time.Until(dl)
	if remaining <= 0 || remaining > selfAuditTimeout+time.Minute {
		t.Fatalf("deadline %v is outside the expected ~%v window", remaining, selfAuditTimeout)
	}
}

// TestBoundSelfAudit_PropagatesParentCancel confirms the derived context is a
// child of the parent (cancelling the parent cancels the audit context), so an
// outer shutdown still aborts the self-audit promptly.
func TestBoundSelfAudit_PropagatesParentCancel(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	ctx, cancel := boundSelfAudit(parent)
	defer cancel()

	cancelParent()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("cancelling parent must cancel the self-audit context")
	}
}
