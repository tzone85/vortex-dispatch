package autoresearch

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

type fakeGateOps struct {
	mu                sync.Mutex
	createdBranches   []string
	pushedBranches    []string
	prsCreated        []string
	prsMerged         []string
	fastForwarded     []string
	branchExists      map[string]bool
	createPRURL       string
	createPRError     error
	pushError         error
	fastForwardError  error
}

func newFakeGateOps() *fakeGateOps {
	return &fakeGateOps{
		branchExists: map[string]bool{},
		createPRURL:  "https://example/pr/1",
	}
}

func (f *fakeGateOps) CreateBranch(_, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createdBranches = append(f.createdBranches, name)
	f.branchExists[name] = true
	return nil
}
func (f *fakeGateOps) BranchExists(_, name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.branchExists[name]
}
func (f *fakeGateOps) PushBranch(_, branch string) error {
	if f.pushError != nil {
		return f.pushError
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pushedBranches = append(f.pushedBranches, branch)
	return nil
}
func (f *fakeGateOps) CreatePR(_, _, _, _, headBranch string) (string, error) {
	if f.createPRError != nil {
		return "", f.createPRError
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prsCreated = append(f.prsCreated, headBranch)
	return f.createPRURL, nil
}
func (f *fakeGateOps) MergePR(_, prURL string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prsMerged = append(f.prsMerged, prURL)
	return nil
}
func (f *fakeGateOps) RebaseOnto(_, _ string) error { return nil }
func (f *fakeGateOps) FastForwardWinning(_, source, _ string) error {
	if f.fastForwardError != nil {
		return f.fastForwardError
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fastForwarded = append(f.fastForwarded, source)
	return nil
}

func keptOutcome(id string) Outcome {
	return Outcome{
		Proposal:   Proposal{ID: id, Repo: "r1", Class: ClassPerf, PromptHash: "ph"},
		Score:      Score{Final: 90, LowerIsBetter: true},
		Baseline:   100,
		Delta:      10,
		Verdict:    VerdictOK,
		Kept:       true,
		BranchOrPR: "autoresearch/exp-" + id,
	}
}

func TestGateRouter_RoutesPR(t *testing.T) {
	ops := newFakeGateOps()
	r := NewGateRouter("main", ops)
	url, err := r.Route("/repo", GatePR, keptOutcome("a"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if url != "https://example/pr/1" {
		t.Errorf("got url %s", url)
	}
	if len(ops.pushedBranches) != 1 {
		t.Error("PR gate should push branch")
	}
	if len(ops.prsCreated) != 1 {
		t.Error("PR gate should create PR")
	}
	if len(ops.prsMerged) != 0 {
		t.Error("PR gate must NOT auto-merge")
	}
}

func TestGateRouter_RoutesAuto(t *testing.T) {
	ops := newFakeGateOps()
	r := NewGateRouter("main", ops)
	_, err := r.Route("/repo", GateAuto, keptOutcome("a"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(ops.prsMerged) != 1 {
		t.Errorf("auto gate should merge PR, got %v", ops.prsMerged)
	}
}

func TestGateRouter_RoutesWinning(t *testing.T) {
	ops := newFakeGateOps()
	r := NewGateRouter("main", ops)
	_, err := r.Route("/repo", GateWinning, keptOutcome("a"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !ops.branchExists["autoresearch/winning"] {
		t.Error("winning gate should create autoresearch/winning if missing")
	}
	if len(ops.fastForwarded) != 1 {
		t.Errorf("winning gate should fast-forward, got %v", ops.fastForwarded)
	}
}

func TestGateRouter_WinningExists_NoCreate(t *testing.T) {
	ops := newFakeGateOps()
	ops.branchExists["autoresearch/winning"] = true
	r := NewGateRouter("main", ops)
	_, err := r.Route("/repo", GateWinning, keptOutcome("a"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(ops.createdBranches) != 0 {
		t.Errorf("must not re-create existing winning branch, got %v", ops.createdBranches)
	}
}

func TestGateRouter_RejectsNonKept(t *testing.T) {
	r := NewGateRouter("main", newFakeGateOps())
	out := keptOutcome("a")
	out.Kept = false
	if _, err := r.Route("/repo", GateWinning, out); err == nil {
		t.Error("must reject non-kept outcome")
	}
}

func TestGateRouter_UnknownGate(t *testing.T) {
	r := NewGateRouter("main", newFakeGateOps())
	if _, err := r.Route("/repo", GateAction("yolo"), keptOutcome("a")); err == nil {
		t.Error("must reject unknown gate action")
	}
}

func TestGateRouter_PerRepoLockingSerializesWinning(t *testing.T) {
	// Two concurrent winners on the same repo must serialize through the
	// per-repo lock — fast-forwards observed in some order, no race.
	ops := newFakeGateOps()
	r := NewGateRouter("main", ops)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); r.Route("/repo", GateWinning, keptOutcome("a")) }()
	go func() { defer wg.Done(); r.Route("/repo", GateWinning, keptOutcome("b")) }()
	wg.Wait()
	if len(ops.fastForwarded) != 2 {
		t.Errorf("expected 2 fast-forwards, got %d", len(ops.fastForwarded))
	}
}

func TestGateRouter_FastForwardErrorSurfaces(t *testing.T) {
	ops := newFakeGateOps()
	ops.fastForwardError = errors.New("not a fast-forward")
	r := NewGateRouter("main", ops)
	if _, err := r.Route("/repo", GateWinning, keptOutcome("a")); err == nil {
		t.Error("FF error must surface")
	}
}

// TestDefaultGateOps_MergePR_RealImpl drives the *shipped* DefaultGateOps
// (not a fake) on the real MergePR code path. Uses a fake gh (via PATH)
// so it exercises parsePRNumberFromURL + git.MergePR delegation without
// needing live GitHub auth. This ensures auto-gate produces correct
// (non-error) outcomes when configured.
func TestDefaultGateOps_MergePR_RealImpl(t *testing.T) {
	fakeDir := t.TempDir()
	fakeScript := "#!/bin/sh\nexit 0\n"
	scriptPath := filepath.Join(fakeDir, "gh")
	if err := os.WriteFile(scriptPath, []byte(fakeScript), 0755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+origPath)

	ops := DefaultGateOps{}
	url := "https://github.com/owner/repo/pull/42"
	if err := ops.MergePR(t.TempDir(), url); err != nil {
		t.Fatalf("DefaultGateOps.MergePR with valid URL should succeed via real impl: %v", err)
	}

	// bad URL
	if err := ops.MergePR(t.TempDir(), ""); err == nil {
		t.Error("empty URL must error")
	}
	if err := ops.MergePR(t.TempDir(), "https://example.com/no/number"); err == nil {
		t.Error("unparseable URL must error")
	}
}
