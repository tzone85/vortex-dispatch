package cli

import (
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

// fakeSink records projected events and fails on demand, to drive the
// rebuild error paths without a real SQLite store.
type fakeSink struct {
	projected  int
	failOnCall int   // the Nth Project call (1-based) fails; 0 = never. The error's "index" is 0-based.
	projectErr error // returned by the failing Project call
	closeErr   error
	closeCalls int
}

func (f *fakeSink) Project(state.Event) error {
	f.projected++
	if f.failOnCall != 0 && f.projected == f.failOnCall {
		return f.projectErr
	}
	return nil
}

func (f *fakeSink) Close() error { f.closeCalls++; return f.closeErr }

// Sentinels: errors.Is proves the %w chain holds end to end, which a
// substring match would not (it passes even after a %w -> %v regression).
var (
	errClose   = errors.New("fsync: input/output error")
	errProject = errors.New("disk full")
)

// swapOpenProjection points openProjection at open for the rest of the test,
// so a fake store (or a real one that fails on cue) stands in for SQLite. Not
// safe with t.Parallel(): it swaps a package var.
func swapOpenProjection(t *testing.T, open func(path string) (projectionSink, error)) {
	t.Helper()
	prev := openProjection
	openProjection = open
	t.Cleanup(func() { openProjection = prev })
}

func twoEvents() []state.Event {
	return []state.Event{
		state.NewEvent(state.EventReqSubmitted, "cli", "", map[string]any{"id": "R"}),
		state.NewEvent(state.EventStoryCreated, "cli", "S", map[string]any{"id": "S"}),
	}
}

// TestRebuildAndClose_CloseErrorIsReturned: the close is the last step of
// the rebuild, so its failure must change the outcome — a revert to
// `defer ps.Close()` would make this test fail.
func TestRebuildAndClose_CloseErrorIsReturned(t *testing.T) {
	sink := &fakeSink{closeErr: errClose}
	applied, err := rebuildAndClose(sink, twoEvents()[:1])
	if !errors.Is(err, errClose) || !strings.Contains(err.Error(), "close rebuilt projection db") {
		t.Fatalf("expected the close error to be returned (wrapped), got applied=%d err=%v", applied, err)
	}
	if applied != 1 || sink.closeCalls != 1 {
		t.Errorf("applied=%d closeCalls=%d, want 1/1", applied, sink.closeCalls)
	}
}

// TestRebuildAndClose_ProjectErrorWins: a projection failure is the error
// the operator sees and the store is still closed exactly once, even when
// that close fails too.
func TestRebuildAndClose_ProjectErrorWins(t *testing.T) {
	sink := &fakeSink{failOnCall: 2, projectErr: errProject, closeErr: errClose}
	applied, err := rebuildAndClose(sink, twoEvents())
	if !errors.Is(err, errProject) || !strings.Contains(err.Error(), "project event") {
		t.Fatalf("expected the projection error, got %v", err)
	}
	if !strings.Contains(err.Error(), "close also failed") {
		t.Fatalf("the close error must ride along, not vanish: %v", err)
	}
	if !errors.Is(err, errClose) {
		t.Fatalf("the close error must be wrapped, not formatted: errors.Is is false for %v", err)
	}
	if applied != 1 || sink.closeCalls != 1 {
		t.Errorf("applied=%d closeCalls=%d, want 1/1", applied, sink.closeCalls)
	}
}

// TestRebuildAndClose_Success closes exactly once and reports every event.
func TestRebuildAndClose_Success(t *testing.T) {
	sink := &fakeSink{}
	applied, err := rebuildAndClose(sink, twoEvents())
	if err != nil || applied != 2 || sink.closeCalls != 1 {
		t.Fatalf("applied=%d closeCalls=%d err=%v, want 2/1/nil", applied, sink.closeCalls, err)
	}
}

// TestRebuildAndClose_NoEvents: an empty log still closes the store once.
func TestRebuildAndClose_NoEvents(t *testing.T) {
	sink := &fakeSink{}
	applied, err := rebuildAndClose(sink, nil)
	if err != nil || applied != 0 || sink.closeCalls != 1 {
		t.Fatalf("applied=%d closeCalls=%d err=%v, want 0/1/nil", applied, sink.closeCalls, err)
	}
}

// TestReplay_CloseFailureFailsTheCommand: through the command, a close
// failure reaches the operator, and no "Projection rebuilt" line contradicts
// it.
func TestReplay_CloseFailureFailsTheCommand(t *testing.T) {
	dir, projectDir := setupReplayEnv(t)
	_, _ = seedReplayEvents(t, projectDir)
	swapOpenProjection(t, func(string) (projectionSink, error) { return &fakeSink{closeErr: errClose}, nil })

	cmd, buf := newReplayTestCmd(t, dir)
	err := cmd.Execute()
	if !errors.Is(err, errClose) {
		t.Fatalf("close failure must reach the command's caller, got %v", err)
	}
	if strings.Contains(buf.String(), "Projection rebuilt") {
		t.Fatalf("a failed rebuild must not report success:\n%s", buf.String())
	}
}

// TestOpenProjection_ErrorReturnsNilInterface: the real seam never returns a
// typed nil — with one, a caller's `ps != nil` check would pass and Close
// would dereference a nil *SQLiteStore. Opening a directory fails on darwin
// and linux (EISDIR); a driver that accepted it would make this vacuous.
func TestOpenProjection_ErrorReturnsNilInterface(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the EISDIR behaviour this pins is darwin/linux")
	}
	ps, err := openProjection(t.TempDir()) // a directory is not a database file
	if err == nil {
		if ps != nil {
			_ = ps.Close()
		}
		t.Fatal("opening a directory as a database must fail")
	}
	if ps != nil {
		t.Fatalf("on error the interface must be nil, got %#v", ps)
	}
}
