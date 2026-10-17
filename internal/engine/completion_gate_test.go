package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/llm"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// fakeFixClient records how many fix-agent invocations the gate made and
// returns a canned response so applyFix succeeds without spawning a real agent.
type fakeFixClient struct {
	mu    sync.Mutex
	calls int
	last  llm.CompletionRequest
}

func (c *fakeFixClient) Complete(_ context.Context, req llm.CompletionRequest) (llm.CompletionResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	c.last = req
	return llm.CompletionResponse{Content: "applied the fix"}, nil
}

func (c *fakeFixClient) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func green() VerificationResult {
	return VerificationResult{BuildPasses: true, TestsPassing: 3, TestsTotal: 3}
}

func red() VerificationResult {
	return VerificationResult{BuildPasses: false, Gaps: []VerificationGap{
		{Category: "build", Severity: "critical", File: "main.go", Detail: "does not compile"},
	}}
}

// scriptedVerify returns each result in sequence, repeating the last forever.
func scriptedVerify(results ...VerificationResult) (verifyFunc, *int) {
	calls := 0
	fn := func(_ context.Context, _ string, _ int) VerificationResult {
		r := results[min(calls, len(results)-1)]
		calls++
		return r
	}
	return fn, &calls
}

func newTestGate(t *testing.T, client llm.Client, maxCycles int, verify verifyFunc) (*CompletionGate, string) {
	t.Helper()
	repoDir := t.TempDir()
	g := NewCompletionGate(client, "test-model", 1000, maxCycles, "main", nil, nil)
	g.verify = verify
	g.pull = func(_, _ string) {} // no-op pull in tests
	return g, repoDir
}

func TestCompletionGate_GreenFirstPass_NoFix(t *testing.T) {
	client := &fakeFixClient{}
	verify, vCalls := scriptedVerify(green())
	g, repoDir := newTestGate(t, client, 2, verify)

	passed, err := g.Run(context.Background(), "REQ-1", repoDir)
	if err != nil {
		t.Fatalf("unexpected gate error: %v", err)
	}

	if !passed {
		t.Fatal("expected gate to pass on a green first verification")
	}
	if client.callCount() != 0 {
		t.Errorf("expected no fix-agent calls on green, got %d", client.callCount())
	}
	if *vCalls != 1 {
		t.Errorf("expected exactly 1 verification, got %d", *vCalls)
	}
}

func TestCompletionGate_RedThenGreen_AutoFixes(t *testing.T) {
	client := &fakeFixClient{}
	verify, vCalls := scriptedVerify(red(), green())
	g, repoDir := newTestGate(t, client, 2, verify)

	passed, err := g.Run(context.Background(), "REQ-2", repoDir)
	if err != nil {
		t.Fatalf("unexpected gate error: %v", err)
	}

	if !passed {
		t.Fatal("expected gate to pass after one successful auto-fix")
	}
	if client.callCount() != 1 {
		t.Errorf("expected exactly 1 fix-agent call, got %d", client.callCount())
	}
	if *vCalls != 2 {
		t.Errorf("expected 2 verifications (initial + post-fix), got %d", *vCalls)
	}
}

func TestCompletionGate_StaysRed_Blocks(t *testing.T) {
	client := &fakeFixClient{}
	verify, _ := scriptedVerify(red()) // always red
	g, repoDir := newTestGate(t, client, 2, verify)

	passed, err := g.Run(context.Background(), "REQ-3", repoDir)
	if err != nil {
		t.Fatalf("unexpected gate error: %v", err)
	}

	if passed {
		t.Fatal("expected gate to block when verification never goes green")
	}
	if client.callCount() != 2 {
		t.Errorf("expected fix-agent invoked maxCycles=2 times, got %d", client.callCount())
	}
}

func TestCompletionGate_NilClient_DegradesToHardGate(t *testing.T) {
	verify, _ := scriptedVerify(red())
	g, repoDir := newTestGate(t, nil, 2, verify) // no godmode client wired

	passed, err := g.Run(context.Background(), "REQ-4", repoDir)
	if err != nil {
		t.Fatalf("unexpected gate error: %v", err)
	}

	if passed {
		t.Fatal("expected hard gate to block on red with no auto-fix client")
	}
}

func TestCompletionGate_WritesGapsFileOnRed(t *testing.T) {
	client := &fakeFixClient{}
	verify, _ := scriptedVerify(red())
	g, repoDir := newTestGate(t, client, 1, verify)

	if _, err := g.Run(context.Background(), "REQ-5", repoDir); err != nil {
		t.Fatalf("unexpected gate error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(repoDir, ".vxd-fix-gaps.md")); err != nil {
		t.Errorf("expected .vxd-fix-gaps.md to be written for operator transparency: %v", err)
	}
}

// writeGoModule writes a minimal buildable/unbuildable Go module into dir.
func writeGoModule(t *testing.T, dir, mainBody string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module gatecheck\n\ngo 1.21\n"), 0o600); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainBody), 0o600); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
}

// TestCompletionGate_RealVerify_BlocksBrokenGoModule drives the gate's REAL
// default verification (an actual `go build`) — not the scripted seam — against
// a module that does not compile, with no auto-fix client. The gate must block.
// This proves the real RunVerificationLoop → ShouldRunFixCycle → gate-decision
// path integrates on a real filesystem.
func TestCompletionGate_RealVerify_BlocksBrokenGoModule(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-build verification in -short mode")
	}
	repoDir := t.TempDir()
	writeGoModule(t, repoDir, "package main\n\nfunc main() {\n\tvar x int = \"not an int\"\n\t_ = x\n}\n")

	// nil client ⇒ hard gate (verify once, no auto-fix). Real verify seam.
	g := NewCompletionGate(nil, "", 0, 0, "main", nil, nil)
	g.pull = func(_, _ string) {}

	passed, err := g.Run(context.Background(), "REQ-REAL-RED", repoDir)
	if err != nil {
		t.Fatalf("unexpected gate error: %v", err)
	}
	if passed {
		t.Fatal("expected gate to BLOCK a composed mainline that does not compile")
	}
}

// TestCompletionGate_RealVerify_PassesHealthyGoModule is the positive control:
// a module that builds and has no failing tests passes the real verification.
func TestCompletionGate_RealVerify_PassesHealthyGoModule(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-build verification in -short mode")
	}
	repoDir := t.TempDir()
	writeGoModule(t, repoDir, "package main\n\nfunc main() {\n\tprintln(\"ok\")\n}\n")
	// README present so the doc gap (medium) is moot; build is the gating signal.
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# gatecheck\n"), 0o600); err != nil {
		t.Fatalf("write README: %v", err)
	}

	g := NewCompletionGate(nil, "", 0, 0, "main", nil, nil)
	g.pull = func(_, _ string) {}

	passed, err := g.Run(context.Background(), "REQ-REAL-GREEN", repoDir)
	if err != nil {
		t.Fatalf("unexpected gate error: %v", err)
	}
	if !passed {
		t.Fatal("expected gate to PASS a composed mainline that builds cleanly")
	}
}

// newOutcomeMonitor returns a Monitor over a real event log and projection
// with requirement reqID seeded (status "pending"), plus the log path so a
// test can assert nothing was appended.
func newOutcomeMonitor(t *testing.T, reqID string) (*Monitor, *state.SQLiteStore, string) {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "events.jsonl")
	es, err := state.NewFileStore(logPath)
	if err != nil {
		t.Fatalf("event store: %v", err)
	}
	ps, err := state.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("proj store: %v", err)
	}
	t.Cleanup(func() {
		es.Close()
		ps.Close()
	})
	if err := ps.Project(state.NewEvent(state.EventReqSubmitted, "test", "", map[string]any{"id": reqID, "title": "Gate"})); err != nil {
		t.Fatalf("seed requirement: %v", err)
	}
	return &Monitor{eventStore: es, projStore: ps}, ps, logPath
}

func requirementStatus(t *testing.T, ps *state.SQLiteStore, reqID string) string {
	t.Helper()
	req, err := ps.GetRequirement(reqID)
	if err != nil {
		t.Fatalf("get requirement: %v", err)
	}
	return req.Status
}

// TestRecordGateOutcome_Interrupted_EmitsNothing: an interrupted gate is no
// verdict — nothing is appended to the log and the requirement keeps its
// status. Deleting the gateErr arm in recordGateOutcome fails this test.
func TestRecordGateOutcome_Interrupted_EmitsNothing(t *testing.T) {
	m, ps, logPath := newOutcomeMonitor(t, "REQ-G2")
	before := requirementStatus(t, ps, "REQ-G2")

	m.recordGateOutcome("REQ-G2", t.TempDir(), false, context.Canceled)

	if got := requirementStatus(t, ps, "REQ-G2"); got != before {
		t.Fatalf("interrupted gate changed the requirement status %q -> %q", before, got)
	}
	if b, err := os.ReadFile(logPath); err == nil && len(b) != 0 {
		t.Fatalf("interrupted gate appended to the event log:\n%s", b)
	}
}

// TestRecordGateOutcome_Passed_EmitsCompleted: a green gate completes the requirement.
func TestRecordGateOutcome_Passed_EmitsCompleted(t *testing.T) {
	m, ps, _ := newOutcomeMonitor(t, "REQ-G3")
	m.recordGateOutcome("REQ-G3", t.TempDir(), true, nil)
	if got := requirementStatus(t, ps, "REQ-G3"); got != "completed" {
		t.Fatalf("want completed after a green gate, got %q", got)
	}
}

// TestRecordGateOutcome_Red_EmitsBlocked: a red gate (no error) blocks the requirement.
func TestRecordGateOutcome_Red_EmitsBlocked(t *testing.T) {
	m, ps, _ := newOutcomeMonitor(t, "REQ-G4")
	m.recordGateOutcome("REQ-G4", t.TempDir(), false, nil)
	if got := requirementStatus(t, ps, "REQ-G4"); got != "blocked" {
		t.Fatalf("want blocked after a red gate, got %q", got)
	}
}

// TestAdvisoryPath_CancelledContext_NoCompletedNoGapsFile: on the legacy path
// (no gate wired) a cancelled ctx must neither complete the requirement nor
// turn the "aborted" gap into a fix requirement. Deleting the ctx.Err() guard
// in recordAdvisoryOutcome fails this test.
func TestAdvisoryPath_CancelledContext_NoCompletedNoGapsFile(t *testing.T) {
	m, ps, logPath := newOutcomeMonitor(t, "REQ-A1")
	repoDir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	aborted := VerificationResult{BuildPasses: true, TestsFailing: 1, TestsTotal: 1, Gaps: []VerificationGap{
		{Category: "test", Severity: "critical", Detail: "test run aborted (context canceled) — no verdict"},
	}}

	m.recordAdvisoryOutcome("REQ-A1", repoDir, aborted, ctx.Err())

	if got := requirementStatus(t, ps, "REQ-A1"); got == "completed" {
		t.Fatal("a cancelled advisory run must not complete the requirement")
	}
	if b, err := os.ReadFile(logPath); err == nil && len(b) != 0 {
		t.Fatalf("cancelled advisory run appended to the event log:\n%s", b)
	}
	if _, err := os.Stat(filepath.Join(repoDir, ".vxd-fix-gaps.md")); !os.IsNotExist(err) {
		t.Fatalf("the aborted gap must not become a fix requirement (stat err=%v)", err)
	}
}

// TestAdvisoryPath_Red_WritesGapsFileAndCompletes pins the legacy contract:
// a red result is written for the operator, and the requirement completes anyway.
func TestAdvisoryPath_Red_WritesGapsFileAndCompletes(t *testing.T) {
	m, ps, _ := newOutcomeMonitor(t, "REQ-A2")
	repoDir := t.TempDir()

	m.recordAdvisoryOutcome("REQ-A2", repoDir, red(), nil)

	if got := requirementStatus(t, ps, "REQ-A2"); got != "completed" {
		t.Fatalf("advisory path completes regardless, got %q", got)
	}
	if _, err := os.Stat(filepath.Join(repoDir, ".vxd-fix-gaps.md")); err != nil {
		t.Fatalf("advisory path must write .vxd-fix-gaps.md on red: %v", err)
	}
}

// deletingPull mimics the real pull: pullBaseAfterMerge's pre-clean removes
// .vxd-fix-gaps.md before pulling.
func deletingPull(repoDir, _ string) {
	_ = os.Remove(filepath.Join(repoDir, ".vxd-fix-gaps.md"))
}

// TestCompletionGate_StaysRed_GapsFileSurvivesFinalCycle: with the real
// pull's pre-clean, the file written at the top of the last cycle is gone by
// the time Run returns false — the final red state must be written after the
// loop, or the "see .vxd-fix-gaps.md" hint points at nothing.
func TestCompletionGate_StaysRed_GapsFileSurvivesFinalCycle(t *testing.T) {
	client := &fakeFixClient{}
	verify, _ := scriptedVerify(red())
	g, repoDir := newTestGate(t, client, 2, verify)
	g.pull = deletingPull

	passed, err := g.Run(context.Background(), "REQ-FINAL", repoDir)
	if err != nil || passed {
		t.Fatalf("want a red verdict, got passed=%v err=%v", passed, err)
	}
	if _, err := os.Stat(filepath.Join(repoDir, ".vxd-fix-gaps.md")); err != nil {
		t.Fatalf(".vxd-fix-gaps.md must hold the final red state after the last cycle: %v", err)
	}
}

// TestCompletionGate_NoAutoFixCycles_Red_WritesGapsFile: a gate with
// maxCycles 0 (a negative qa.completion_fix_cycles; 0 means the default)
// never enters the loop; the red state must still be written.
func TestCompletionGate_NoAutoFixCycles_Red_WritesGapsFile(t *testing.T) {
	verify, _ := scriptedVerify(red())
	g, repoDir := newTestGate(t, &fakeFixClient{}, 0, verify)

	passed, err := g.Run(context.Background(), "REQ-ZERO", repoDir)
	if err != nil || passed {
		t.Fatalf("want a red verdict, got passed=%v err=%v", passed, err)
	}
	if _, err := os.Stat(filepath.Join(repoDir, ".vxd-fix-gaps.md")); err != nil {
		t.Fatalf(".vxd-fix-gaps.md must be written even with zero fix cycles: %v", err)
	}
}

// TestEmitRequirementOutcome_Blocked proves the monitor's terminal-event helper
// drives a real event + projection store: emitting REQ_BLOCKED transitions the
// requirement to "blocked" status (the gate's negative outcome), not "completed".
func TestEmitRequirementOutcome_Blocked(t *testing.T) {
	dir := t.TempDir()
	es, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("event store: %v", err)
	}
	defer es.Close()
	ps, err := state.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("proj store: %v", err)
	}
	defer ps.Close()

	if err := ps.Project(state.NewEvent(state.EventReqSubmitted, "test", "", map[string]any{"id": "REQ-G1", "title": "Gate"})); err != nil {
		t.Fatalf("seed requirement: %v", err)
	}

	m := &Monitor{eventStore: es, projStore: ps}
	m.emitRequirementOutcome("REQ-G1", state.EventReqBlocked, "REQ_BLOCKED")

	req, err := ps.GetRequirement("REQ-G1")
	if err != nil {
		t.Fatalf("get requirement: %v", err)
	}
	if req.Status != "blocked" {
		t.Errorf("expected status 'blocked' after REQ_BLOCKED, got %q", req.Status)
	}
}

// cancellingFixClient cancels the caller's context from inside Complete, the
// way a Ctrl-C lands during a 15-minute fix run.
type cancellingFixClient struct {
	cancel context.CancelFunc
}

func (c *cancellingFixClient) Complete(ctx context.Context, req llm.CompletionRequest) (llm.CompletionResponse, error) {
	c.cancel()
	return llm.CompletionResponse{}, ctx.Err()
}

// TestCompletionGate_CancelDuringAutoFix_NoVerdict: Ctrl-C during applyFix
// must surface as an error (no verdict), never as passed=false (REQ_BLOCKED).
func TestCompletionGate_CancelDuringAutoFix_NoVerdict(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &cancellingFixClient{cancel: cancel}
	verify, _ := scriptedVerify(red(), red())
	g, repoDir := newTestGate(t, client, 2, verify)

	passed, err := g.Run(ctx, "REQ-CANCEL-FIX", repoDir)
	if err == nil {
		t.Fatal("cancel during auto-fix must return the context error (no verdict)")
	}
	if passed {
		t.Fatal("an interrupted gate must never certify completion")
	}
}

// TestCompletionGate_AlreadyCancelled_NeverVerifies: a ctx cancelled before
// the gate (during documentation, pull or dangling-branch cleanup) must not
// start a verification — ensureDependencies and the build are not killable.
func TestCompletionGate_AlreadyCancelled_NeverVerifies(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	verify, vCalls := scriptedVerify(red())
	g, repoDir := newTestGate(t, &fakeFixClient{}, 2, verify)

	passed, err := g.Run(ctx, "REQ-PRE-CANCEL", repoDir)
	if err == nil || passed {
		t.Fatalf("want an error and no verdict, got passed=%v err=%v", passed, err)
	}
	if *vCalls != 0 {
		t.Fatalf("a cancelled gate must not verify, got %d verification(s)", *vCalls)
	}
}

// TestRecordRedCycle_WritesSummaryWhenNoGapCarriesDetail: a red result whose
// gaps are empty still leaves a document behind, so the operator hint and the
// webhook never point at a missing file.
func TestRecordRedCycle_WritesSummaryWhenNoGapCarriesDetail(t *testing.T) {
	g, repoDir := newTestGate(t, nil, 0, nil)
	g.recordRedCycle("REQ-SUM", repoDir, VerificationResult{BuildPasses: true, TestsPassing: 3, TestsFailing: 1, TestsTotal: 4})
	b, err := os.ReadFile(filepath.Join(repoDir, ".vxd-fix-gaps.md"))
	if err != nil {
		t.Fatalf("summary document must be written: %v", err)
	}
	if !strings.Contains(string(b), "3 passing / 1 failing / 4 total") || !strings.Contains(string(b), "vxd resume") {
		t.Fatalf("summary must carry the counts and the next step:\n%s", b)
	}
}

// TestCompletionGate_RealVerify_PlainTestFailure_WritesGapsFile drives the
// real verification against a module that builds but has one failing
// assertion — the most common red — and asserts the gate blocks AND writes
// .vxd-fix-gaps.md with the failing test's output.
func TestCompletionGate_RealVerify_PlainTestFailure_WritesGapsFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-build verification in -short mode")
	}
	repoDir := writeGoFixture(t, map[string]string{
		"go.mod":      "module example.com/m\n\ngo 1.22\n",
		"README.md":   "# m\n",
		"lib.go":      "package m\n\nfunc Add(a, b int) int { return a + b }\n",
		"lib_test.go": "package m\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 4 {\n\t\tt.Errorf(\"want 4, got %d\", Add(1, 2))\n\t}\n}\n",
	})
	g := NewCompletionGate(nil, "", 0, 0, "main", nil, nil)
	g.pull = func(_, _ string) {}

	passed, err := g.Run(context.Background(), "REQ-REAL-TESTFAIL", repoDir)
	if err != nil || passed {
		t.Fatalf("want a red verdict, got passed=%v err=%v", passed, err)
	}
	b, err := os.ReadFile(filepath.Join(repoDir, ".vxd-fix-gaps.md"))
	if err != nil {
		t.Fatalf(".vxd-fix-gaps.md must exist after a plain test failure: %v", err)
	}
	if !strings.Contains(string(b), "test(s) failed") || !strings.Contains(string(b), "want 4, got 3") {
		t.Fatalf("gaps file must name the failure and carry the test output:\n%s", b)
	}
}

// TestCompletionGate_TimeoutGap_BlocksWithoutAutoFix: a suite that did not
// finish within the gate's test timeout blocks the requirement — with the gap in
// .vxd-fix-gaps.md — but no fix agent is dispatched for it, however many
// cycles are configured.
func TestCompletionGate_TimeoutGap_BlocksWithoutAutoFix(t *testing.T) {
	client := &fakeFixClient{}
	timedOut := VerificationResult{BuildPasses: true, TestsFailing: 1, TestsTotal: 1, Gaps: []VerificationGap{
		{Category: "test", Severity: "critical", Kind: gapTimeout, Detail: timeoutDetailPrefix + verifyTestTimeout.String(), Output: "=== RUN   TestHangs"},
	}}
	verify, vCalls := scriptedVerify(timedOut)
	g, repoDir := newTestGate(t, client, 2, verify)

	passed, err := g.Run(context.Background(), "REQ-TO", repoDir)
	if err != nil {
		t.Fatalf("unexpected gate error: %v", err)
	}
	if passed {
		t.Fatal("a suite that did not finish must not pass the gate")
	}
	if client.callCount() != 0 {
		t.Fatalf("no fix agent may be dispatched for a timeout, got %d call(s)", client.callCount())
	}
	if *vCalls != 1 {
		t.Fatalf("expected exactly 1 verification, got %d", *vCalls)
	}
	b, readErr := os.ReadFile(filepath.Join(repoDir, ".vxd-fix-gaps.md"))
	if readErr != nil {
		t.Fatalf("the timeout must be on record in .vxd-fix-gaps.md: %v", readErr)
	}
	if !strings.Contains(string(b), timeoutDetailPrefix) {
		t.Fatalf("gaps file does not name the timeout:\n%s", b)
	}
}

func TestHasUnfixableGap(t *testing.T) {
	if hasUnfixableGap(red()) {
		t.Error("a compile break is fixable")
	}
	if hasUnfixableGap(VerificationResult{Gaps: []VerificationGap{{Category: "test", Detail: timeoutDetailPrefix + "20m0s"}}}) {
		t.Error("the wording alone must not decide: only the kind does")
	}
	if !hasUnfixableGap(VerificationResult{Gaps: []VerificationGap{{Category: "test", Kind: gapTimeout, Detail: "whatever"}}}) {
		t.Error("a timeout gap blocks auto-fix")
	}
}

// TestMonitor_NoAgents_RunsCompletionGate_RedThenGreen is the engine half of
// #138: a monitor started with no agents (what a gate-only `vxd resume`
// does) reaches the REAL completion gate from its first tick — dispatchNextWave
// → every story complete → CompletionGate.Run — not the legacy advisory path
// the dry-run CLI tests exercise. Red projects blocked; a second run with
// green projects completed. The pull runs against the cwd, so the test
// chdirs into a throwaway repo (no origin: the ff-pull fails and is logged).
func TestMonitor_NoAgents_RunsCompletionGate_RedThenGreen(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns git")
	}
	t.Chdir(setupCleanGitRepo(t))
	m, ps, _ := newOutcomeMonitor(t, "REQ-ZA1")
	for _, evt := range []state.Event{
		state.NewEvent(state.EventStoryCreated, "tech-lead", "STR-ZA1", map[string]any{
			"id": "STR-ZA1", "req_id": "REQ-ZA1", "title": "Merged story", "complexity": 3,
		}),
		state.NewEvent(state.EventStoryMerged, "", "STR-ZA1", nil),
	} {
		if err := ps.Project(evt); err != nil {
			t.Fatalf("seed %s: %v", evt.Type, err)
		}
	}
	m.config.Monitor.PollIntervalMs = 10
	m.SetAutoResume(&Dispatcher{}, &Executor{}) // never used: nothing is ready to dispatch
	verify, calls := scriptedVerify(red(), green())
	gate, _ := newTestGate(t, nil, 0, verify)
	m.SetCompletionGate(gate)
	rc := &RunContext{ReqID: "REQ-ZA1"}

	if err := m.RunWithContext(context.Background(), nil, ".", rc); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if got := requirementStatus(t, ps, "REQ-ZA1"); got != "blocked" {
		t.Fatalf("a red gate from a zero-agent monitor must block, got %q (verify calls: %d)", got, *calls)
	}
	if err := m.RunWithContext(context.Background(), nil, ".", rc); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if got := requirementStatus(t, ps, "REQ-ZA1"); got != "completed" {
		t.Fatalf("a green gate on the re-run must complete, got %q (verify calls: %d)", got, *calls)
	}
	if *calls != 2 {
		t.Fatalf("the gate must have verified once per run, got %d", *calls)
	}
}

// TestCompletionGate_TestTimeout_DefaultAndOverride: the gate carries the
// bound its verify passes down (qa.completion_test_timeout_s); zero and
// negative keep the default.
func TestCompletionGate_TestTimeout_DefaultAndOverride(t *testing.T) {
	g := NewCompletionGate(nil, "m", 1, 0, "main", nil, nil)
	if g.TestTimeout() != verifyTestTimeout {
		t.Fatalf("default bound: want %s, got %s", verifyTestTimeout, g.TestTimeout())
	}
	g.SetTestTimeout(0)
	g.SetTestTimeout(-time.Second)
	if g.TestTimeout() != verifyTestTimeout {
		t.Fatalf("0 and negative must keep the default, got %s", g.TestTimeout())
	}
	g.SetTestTimeout(45 * time.Minute)
	if g.TestTimeout() != 45*time.Minute {
		t.Fatalf("want 45m, got %s", g.TestTimeout())
	}
}

// countingDocClient counts the LLM calls documentation generation would make.
type countingDocClient struct {
	mu    sync.Mutex
	calls int
}

func (c *countingDocClient) Complete(_ context.Context, _ llm.CompletionRequest) (llm.CompletionResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return llm.CompletionResponse{Content: "# README\n"}, nil
}

func (c *countingDocClient) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// TestDispatchNextWave_GateOnly_SkipsDocumentation: a gate-only resume enters
// the all-stories-done branch for the gate alone. Regenerating the docs there
// would add a duplicate README section, another doc commit and another round
// of LLM spend on every attempt at fixing the gaps — and the commit would land
// before the fast-forward pull, which would then fail.
func TestDispatchNextWave_GateOnly_SkipsDocumentation(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns git")
	}
	repo := setupCleanGitRepo(t)
	t.Chdir(repo)
	head := func() string {
		out, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
		if err != nil {
			t.Fatalf("rev-parse: %v", err)
		}
		return strings.TrimSpace(string(out))
	}
	before := head()

	m, ps, _ := newOutcomeMonitor(t, "REQ-DOC1")
	for _, evt := range []state.Event{
		state.NewEvent(state.EventStoryCreated, "tech-lead", "STR-DOC1", map[string]any{
			"id": "STR-DOC1", "req_id": "REQ-DOC1", "title": "Merged story", "complexity": 3,
		}),
		state.NewEvent(state.EventStoryMerged, "", "STR-DOC1", nil),
	} {
		if err := ps.Project(evt); err != nil {
			t.Fatalf("seed %s: %v", evt.Type, err)
		}
	}
	docs := &countingDocClient{}
	m.SetDocGenerator(docs, "test-model")
	verify, _ := scriptedVerify(green())
	gate, _ := newTestGate(t, nil, 0, verify)
	m.SetCompletionGate(gate)

	m.dispatchNextWave(context.Background(), &RunContext{ReqID: "REQ-DOC1", GateOnly: true}, repo)

	if docs.callCount() != 0 {
		t.Fatalf("a gate-only run must not regenerate the documentation, got %d LLM call(s)", docs.callCount())
	}
	if head() != before {
		t.Fatalf("a gate-only run must not commit: HEAD moved %s -> %s", before, head())
	}
	if got := requirementStatus(t, ps, "REQ-DOC1"); got != "completed" {
		t.Fatalf("the gate must still run: want completed, got %q", got)
	}
}
