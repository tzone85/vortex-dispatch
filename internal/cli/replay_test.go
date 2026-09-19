package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/tzone85/vortex-dispatch/internal/engine"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// setupReplayEnv creates a bare VXD project state directory (no git repo
// needed — replay resolves the project via the explicit --project flag).
func setupReplayEnv(t *testing.T) (dir, projectDir string) {
	t.Helper()
	dir = t.TempDir()
	projectDir = filepath.Join(dir, ".vxd", "projects", "test-project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	return dir, projectDir
}

// newReplayTestCmd builds the replay command wired to the temp workspace:
// config points at a nonexistent file (falls back to defaults whose
// state_dir ~/.vxd resolves under HOME=tempdir), project is pinned.
func newReplayTestCmd(t *testing.T, dir string) (*cobra.Command, *bytes.Buffer) {
	t.Helper()
	cmd := newReplayCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent-vxd.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	t.Setenv("HOME", dir)
	return cmd, &buf
}

// seedReplayEvents appends the four lifecycle events from the spec to
// events.jsonl AND projects them, returning the pre-delete projection state.
func seedReplayEvents(t *testing.T, projectDir string) (state.Story, state.Requirement) {
	t.Helper()
	es, err := state.NewFileStore(filepath.Join(projectDir, "events.jsonl"))
	if err != nil {
		t.Fatalf("create event store: %v", err)
	}
	ps, err := state.NewSQLiteStore(filepath.Join(projectDir, "vxd.db"))
	if err != nil {
		es.Close()
		t.Fatalf("create sqlite store: %v", err)
	}

	events := []state.Event{
		state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
			"id":          "REQ00001",
			"title":       "Replay Req",
			"description": "seeded for replay test",
		}),
		state.NewEvent(state.EventStoryCreated, "tech-lead", "STR001", map[string]any{
			"id":         "STR001",
			"req_id":     "REQ00001",
			"title":      "Story One",
			"complexity": 3,
		}),
		state.NewEvent(state.EventStoryStarted, "", "STR001", nil),
		state.NewEvent(state.EventStoryCompleted, "", "STR001", nil),
	}
	for _, evt := range events {
		if err := es.Append(evt); err != nil {
			t.Fatalf("append event: %v", err)
		}
		if err := ps.Project(evt); err != nil {
			t.Fatalf("project event: %v", err)
		}
	}

	preStory, err := ps.GetStory("STR001")
	if err != nil {
		t.Fatalf("get pre-delete story: %v", err)
	}
	preReq, err := ps.GetRequirement("REQ00001")
	if err != nil {
		t.Fatalf("get pre-delete requirement: %v", err)
	}
	// Fixture sanity: the seeded sequence must end at story=review, req=pending.
	if preStory.Status != "review" {
		t.Fatalf("fixture drift: expected pre-delete story status 'review', got %q", preStory.Status)
	}
	if preReq.Status != "pending" {
		t.Fatalf("fixture drift: expected pre-delete req status 'pending', got %q", preReq.Status)
	}

	if err := es.Close(); err != nil {
		t.Fatalf("close event store: %v", err)
	}
	if err := ps.Close(); err != nil {
		t.Fatalf("close sqlite store: %v", err)
	}
	return preStory, preReq
}

func removeDBFiles(t *testing.T, projectDir string) {
	t.Helper()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(filepath.Join(projectDir, "vxd.db"+suffix)); err != nil && !os.IsNotExist(err) {
			t.Fatalf("remove vxd.db%s: %v", suffix, err)
		}
	}
}

// assertProjectionMatchesPreDelete reopens the rebuilt store and verifies the
// requirement + story statuses equal the state captured before deletion.
func assertProjectionMatchesPreDelete(t *testing.T, projectDir string, preStory state.Story, preReq state.Requirement) {
	t.Helper()
	ps, err := state.NewSQLiteStore(filepath.Join(projectDir, "vxd.db"))
	if err != nil {
		t.Fatalf("reopen rebuilt store: %v", err)
	}
	defer ps.Close()

	story, err := ps.GetStory("STR001")
	if err != nil {
		t.Fatalf("get rebuilt story: %v", err)
	}
	if story.Status != preStory.Status {
		t.Errorf("story status = %q, want %q (pre-delete)", story.Status, preStory.Status)
	}
	req, err := ps.GetRequirement("REQ00001")
	if err != nil {
		t.Fatalf("get rebuilt requirement: %v", err)
	}
	if req.Status != preReq.Status {
		t.Errorf("requirement status = %q, want %q (pre-delete)", req.Status, preReq.Status)
	}
}

func TestNewReplayCmd(t *testing.T) {
	cmd := newReplayCmd()
	if cmd.Use != "replay" {
		t.Errorf("Use = %q", cmd.Use)
	}
	if cmd.Short == "" {
		t.Error("Short is empty")
	}
	if cmd.Flags().Lookup("dry-run") == nil {
		t.Error("flag 'dry-run' not registered")
	}
}

// TestReplay_WiredIntoRoot proves the feature is ACTIVATED, not just
// implemented: the command must be registered on rootCmd.
func TestReplay_WiredIntoRoot(t *testing.T) {
	for _, c := range rootCmd.Commands() {
		if c.Name() == "replay" {
			return
		}
	}
	t.Fatal("replay command not registered on rootCmd — add rootCmd.AddCommand(newReplayCmd()) to internal/cli/root.go")
}

// TestReplay_RebuildsProjections covers the spec's disaster-recovery case:
// the SQLite projection is DELETED, then replay rebuilds it from the event
// log. No backup can exist here — there was no database left to move aside —
// so the absence of vxd.db.bak-* is itself asserted.
func TestReplay_RebuildsProjections(t *testing.T) {
	dir, projectDir := setupReplayEnv(t)
	preStory, preReq := seedReplayEvents(t, projectDir)

	// Simulate corruption/loss: the SQLite projection is gone.
	removeDBFiles(t, projectDir)

	cmd, buf := newReplayTestCmd(t, dir)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("replay failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Replayed 4 events") {
		t.Errorf("expected 'Replayed 4 events' in output, got: %s", output)
	}
	if !strings.Contains(output, "REQ_SUBMITTED") || !strings.Contains(output, "STORY_COMPLETED") {
		t.Errorf("expected per-type tally in output, got: %s", output)
	}

	assertProjectionMatchesPreDelete(t, projectDir, preStory, preReq)

	// Nothing existed to back up — replay must not fabricate one.
	baks, _ := filepath.Glob(filepath.Join(projectDir, "vxd.db.bak-*"))
	if len(baks) != 0 {
		t.Errorf("expected no backup when the db was deleted before replay, got %v", baks)
	}
}

// TestReplay_BackupsExistingDB covers the other recovery shape: the old
// projection still exists (corrupt/divergent rather than deleted). Replay
// must move it aside to vxd.db.bak-<timestamp> (never destroy it) and still
// produce a fresh projection matching the event log.
func TestReplay_BackupsExistingDB(t *testing.T) {
	dir, projectDir := setupReplayEnv(t)
	preStory, preReq := seedReplayEvents(t, projectDir)

	// Deliberately leave vxd.db in place.

	cmd, buf := newReplayTestCmd(t, dir)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("replay failed: %v", err)
	}
	if !strings.Contains(buf.String(), "backed up") {
		t.Errorf("expected backup path reported in output, got: %s", buf.String())
	}

	assertProjectionMatchesPreDelete(t, projectDir, preStory, preReq)

	baks, _ := filepath.Glob(filepath.Join(projectDir, "vxd.db.bak-*"))
	if len(baks) != 1 {
		t.Fatalf("expected exactly one vxd.db.bak-<timestamp> backup, got %d (%v)", len(baks), baks)
	}
	info, err := os.Stat(baks[0])
	if err != nil {
		t.Fatalf("stat backup: %v", err)
	}
	if info.Size() == 0 {
		t.Error("backup file is empty — old db was not preserved")
	}
}

func TestReplay_DryRunReportsCorruptLines(t *testing.T) {
	dir, projectDir := setupReplayEnv(t)
	_, _ = seedReplayEvents(t, projectDir) // 4 valid lines

	// Inject a garbage line — it lands on line 5.
	f, err := os.OpenFile(filepath.Join(projectDir, "events.jsonl"), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open events.jsonl: %v", err)
	}
	if _, err := f.WriteString("this is definitely not json\n"); err != nil {
		t.Fatalf("append garbage line: %v", err)
	}
	f.Close()

	// Snapshot the db so we can prove dry-run left it untouched.
	dbPath := filepath.Join(projectDir, "vxd.db")
	before, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("read db before dry-run: %v", err)
	}

	cmd, buf := newReplayTestCmd(t, dir)
	if err := cmd.Flags().Set("dry-run", "true"); err != nil {
		t.Fatalf("set dry-run: %v", err)
	}
	err = cmd.Execute()
	if err == nil {
		t.Fatal("expected non-zero exit for corrupt event log")
	}
	if !strings.Contains(buf.String(), "line 5") {
		t.Errorf("output should report the corrupt line number, got: %s", buf.String())
	}

	after, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("read db after dry-run: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("dry-run modified the SQLite database")
	}
}

func TestReplay_RefusesWhenLocked(t *testing.T) {
	dir, projectDir := setupReplayEnv(t)

	// Simulate a LIVE pipeline: a lock file owned by this (alive) process.
	lockInfo := engine.LockInfo{
		PID:       os.Getpid(),
		ReqID:     "REQ-LIVE-42",
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.Marshal(lockInfo)
	if err != nil {
		t.Fatalf("marshal lock info: %v", err)
	}
	lockPath := filepath.Join(projectDir, "vxd.lock")
	if err := os.WriteFile(lockPath, data, 0o600); err != nil {
		t.Fatalf("write lock file: %v", err)
	}

	cmd, _ := newReplayTestCmd(t, dir)
	err = cmd.Execute()
	if err == nil {
		t.Fatal("expected refusal while a live pipeline holds the lock")
	}
	if !strings.Contains(err.Error(), "REQ-LIVE-42") {
		t.Errorf("error should mention the running requirement, got: %v", err)
	}
}

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
	errOpen    = errors.New("unable to open database file")
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

// swapProjectionErr makes the store fail to open — after creating the file,
// as the real NewSQLiteStore does when the schema statement fails.
func swapProjectionErr(t *testing.T, err error) {
	t.Helper()
	swapOpenProjection(t, func(path string) (projectionSink, error) {
		if werr := os.WriteFile(path, nil, 0o600); werr != nil {
			t.Fatal(werr)
		}
		return nil, err
	})
}

// leftoverSink leaves a db trio on disk and fails its close — what a failed
// SQLite close leaves behind (no checkpoint, -wal kept).
type leftoverSink struct {
	fakeSink
	dbPath string
}

func (l *leftoverSink) Close() error {
	for _, s := range []string{"", "-wal", "-shm"} {
		if err := os.WriteFile(l.dbPath+s, []byte("partial"), 0o600); err != nil {
			return err
		}
	}
	return l.fakeSink.Close()
}

// swapProjectionLeftover makes the store leave a trio behind and fail to close.
func swapProjectionLeftover(t *testing.T, closeErr error) {
	t.Helper()
	swapOpenProjection(t, func(path string) (projectionSink, error) {
		return &leftoverSink{fakeSink: fakeSink{closeErr: closeErr}, dbPath: path}, nil
	})
}

// projectFailingStore is a real store whose Nth Project fails after the
// earlier ones committed; the failing call proves the partial file exists.
// failureRecord is what the failing store saw, read after the command
// returns: t.Fatalf from inside Project would unwind through cobra with the
// project lock still held. It outlives the store, which is created afresh on
// every open (each run counts its own Project calls).
type failureRecord struct {
	sawFailure bool
	partialErr error // os.Stat's answer at the moment of the failure
}

type projectFailingStore struct {
	*state.SQLiteStore
	dbPath            string
	calls, failOnCall int
	rec               *failureRecord
}

func (p *projectFailingStore) Project(evt state.Event) error {
	p.calls++
	if p.calls == p.failOnCall {
		_, p.rec.partialErr = os.Stat(p.dbPath)
		p.rec.sawFailure = true
		return errProject
	}
	return p.SQLiteStore.Project(evt)
}

// swapProjectionFailingReal opens a real store whose Nth Project fails, and
// returns it so the caller can assert what it saw once the command is done.
func swapProjectionFailingReal(t *testing.T, failOnCall int) *failureRecord {
	t.Helper()
	rec := &failureRecord{}
	swapOpenProjection(t, func(path string) (projectionSink, error) {
		ps, err := state.NewSQLiteStore(path)
		if err != nil {
			return nil, err
		}
		return &projectFailingStore{SQLiteStore: ps, dbPath: path, failOnCall: failOnCall, rec: rec}, nil
	})
	return rec
}

// assertPartialExistedAtFailure: the point of failing a real store mid-rebuild
// is that there IS a partial database to remove when it fails.
func assertPartialExistedAtFailure(t *testing.T, rec *failureRecord) {
	t.Helper()
	if !rec.sawFailure {
		t.Fatal("the store's failing call never ran")
	}
	if rec.partialErr != nil {
		t.Fatalf("the partial rebuild must exist when Project fails: %v", rec.partialErr)
	}
}

// assertBackupOnDisk: "previous database is at X" is a claim about the disk —
// exactly one backup exists, and it holds the pre-replay database. The
// sidecar filter is deliberately its own copy of latestBackup's: a test that
// shares the code it checks proves nothing.
func assertBackupOnDisk(t *testing.T, projectDir string, want []byte) string {
	t.Helper()
	all, _ := filepath.Glob(filepath.Join(projectDir, "vxd.db.bak-*"))
	var baks []string
	for _, b := range all {
		if !strings.HasSuffix(b, "-wal") && !strings.HasSuffix(b, "-shm") {
			baks = append(baks, b) // the sidecars travel with their database
		}
	}
	if len(baks) != 1 {
		t.Fatalf("want exactly one backup, got %v (all: %v)", baks, all)
	}
	got, err := os.ReadFile(baks[0])
	if err != nil {
		t.Fatalf("read backup %s: %v", baks[0], err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("backup %s does not hold the pre-replay database (%d bytes, want %d)", baks[0], len(got), len(want))
	}
	return baks[0]
}

// assertPartialRemoved: nothing this run created is left at vxd.db*.
func assertPartialRemoved(t *testing.T, projectDir string) {
	t.Helper()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if _, err := os.Stat(filepath.Join(projectDir, "vxd.db"+suffix)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("vxd.db%s must be removed after a failed rebuild (stat err=%v)", suffix, err)
		}
	}
}

// assertFailedRebuildErr checks the parts an operator acts on — nothing is
// left at vxd.db, the backup to move back is named (or no file is named when
// there is none), and re-running is offered — not the sentence they are
// wrapped in, which restoreHint owns.
func assertFailedRebuildErr(t *testing.T, err error, projectDir, bakPath string) {
	t.Helper()
	assertPartialRemoved(t, projectDir)
	msg := err.Error()
	for _, want := range []string{filepath.Join(projectDir, "vxd.db"), "no partial rebuild left", "re-run vxd replay"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("the error must carry %q, got %v", want, err)
		}
	}
	if bakPath == "" {
		if strings.Contains(msg, ".bak-") {
			t.Fatalf("there is no backup to name, got %v", err)
		}
		return
	}
	if !strings.Contains(msg, bakPath) {
		t.Fatalf("the error must name the backup to move back, got %v", err)
	}
}

// snapshotDB returns the bytes of vxd.db so a test can prove which file a
// backup holds.
func snapshotDB(t *testing.T, projectDir string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(projectDir, "vxd.db"))
	if err != nil {
		t.Fatalf("read vxd.db: %v", err)
	}
	return b
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

// TestReplay_CloseFailure_RemovesPartialAndNamesBackup: through the command,
// a close failure — the one case where SQLite leaves the -wal/-shm behind —
// removes the trio the run created, names the backup to move back and prints
// no "Projection rebuilt" line that would contradict it.
func TestReplay_CloseFailure_RemovesPartialAndNamesBackup(t *testing.T) {
	dir, projectDir := setupReplayEnv(t)
	_, _ = seedReplayEvents(t, projectDir) // vxd.db left in place -> a backup is taken
	before := snapshotDB(t, projectDir)
	swapProjectionLeftover(t, errClose)

	cmd, buf := newReplayTestCmd(t, dir)
	err := cmd.Execute()
	if !errors.Is(err, errClose) {
		t.Fatalf("close failure must reach the command's caller, got %v", err)
	}
	assertFailedRebuildErr(t, err, projectDir, assertBackupOnDisk(t, projectDir, before))
	if strings.Contains(buf.String(), "Projection rebuilt") {
		t.Fatalf("a failed rebuild must not report success:\n%s", buf.String())
	}
}

// TestReplay_ProjectFailure_RemovesPartialAndNamesBackup: a real store whose
// third Project fails after two committed — the partial file exists when the
// failure happens, and is gone afterwards; the error names the index.
func TestReplay_ProjectFailure_RemovesPartialAndNamesBackup(t *testing.T) {
	dir, projectDir := setupReplayEnv(t)
	_, _ = seedReplayEvents(t, projectDir)
	before := snapshotDB(t, projectDir)
	failing := swapProjectionFailingReal(t, 3)

	cmd, buf := newReplayTestCmd(t, dir)
	err := cmd.Execute()
	if !errors.Is(err, errProject) {
		t.Fatalf("projection failure must reach the command's caller, got %v", err)
	}
	if !strings.Contains(err.Error(), "at index 2") {
		t.Fatalf("error must name the failing index, got %v", err)
	}
	assertPartialExistedAtFailure(t, failing)
	assertFailedRebuildErr(t, err, projectDir, assertBackupOnDisk(t, projectDir, before))
	if strings.Contains(buf.String(), "Projection rebuilt") {
		t.Fatalf("a failed rebuild must not report success:\n%s", buf.String())
	}
}

// TestReplay_OpenFailure_RemovesPartialAndNamesBackup: the store could not
// be created after the previous database was moved aside — the real failure
// happens after SQLite created the file, and the seam does the same — and
// the file is gone afterwards.
func TestReplay_OpenFailure_RemovesPartialAndNamesBackup(t *testing.T) {
	dir, projectDir := setupReplayEnv(t)
	_, _ = seedReplayEvents(t, projectDir)
	before := snapshotDB(t, projectDir)
	swapProjectionErr(t, errOpen)

	cmd, buf := newReplayTestCmd(t, dir)
	err := cmd.Execute()
	if !errors.Is(err, errOpen) || !strings.Contains(err.Error(), "create fresh projection store") {
		t.Fatalf("open failure must reach the command's caller, got %v", err)
	}
	assertFailedRebuildErr(t, err, projectDir, assertBackupOnDisk(t, projectDir, before))
	if strings.Contains(buf.String(), "Projection rebuilt") {
		t.Fatalf("a failed open must not report success:\n%s", buf.String())
	}
}

// TestReplay_FailureWithoutBackup_SaysRerun: with no previous database there
// is nothing to move back, so the error says only to re-run — and no backup
// is fabricated.
func TestReplay_FailureWithoutBackup_SaysRerun(t *testing.T) {
	dir, projectDir := setupReplayEnv(t)
	_, _ = seedReplayEvents(t, projectDir)
	removeDBFiles(t, projectDir)
	swapProjectionLeftover(t, errClose)

	cmd, _ := newReplayTestCmd(t, dir)
	err := cmd.Execute()
	if !errors.Is(err, errClose) {
		t.Fatalf("close failure must reach the command's caller, got %v", err)
	}
	assertFailedRebuildErr(t, err, projectDir, "")
	if baks, _ := filepath.Glob(filepath.Join(projectDir, "vxd.db.bak-*")); len(baks) != 0 {
		t.Fatalf("no backup must be fabricated, got %v", baks)
	}
}

// TestReplay_RerunAfterFailure_NamesEarlierBackup: the first failure removed
// what it created, so the re-run finds nothing at vxd.db and takes no second
// backup — which, within the same second, would have overwritten the real
// one. The real database is still at the first run's backup name, and the
// second failure has to name it.
func TestReplay_RerunAfterFailure_NamesEarlierBackup(t *testing.T) {
	dir, projectDir := setupReplayEnv(t)
	_, _ = seedReplayEvents(t, projectDir)
	before := snapshotDB(t, projectDir)
	// A database mid-crash still has its sidecars, so the backup is a trio —
	// and the glob that finds it must not offer the -wal as the database.
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.WriteFile(filepath.Join(projectDir, "vxd.db"+suffix), []byte("sidecar"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	failing := swapProjectionFailingReal(t, 3)

	cmd, _ := newReplayTestCmd(t, dir)
	if err := cmd.Execute(); !errors.Is(err, errProject) {
		t.Fatalf("first run: %v", err)
	}
	assertPartialExistedAtFailure(t, failing)
	bak := assertBackupOnDisk(t, projectDir, before)

	cmd, _ = newReplayTestCmd(t, dir)
	err := cmd.Execute()
	if !errors.Is(err, errProject) {
		t.Fatalf("second run: %v", err)
	}
	assertFailedRebuildErr(t, err, projectDir, bak)
	if got := assertBackupOnDisk(t, projectDir, before); got != bak {
		t.Fatalf("the re-run must not touch the backup: %s -> %s", bak, got)
	}
}

// TestDiscardFailedRebuild: a missing file is not a failure; a file that
// cannot be removed is named.
func TestDiscardFailedRebuild(t *testing.T) {
	dir := t.TempDir()
	if err := discardFailedRebuild(filepath.Join(dir, "absent.db")); err != nil {
		t.Fatalf("a missing file is not a failure, got %v", err)
	}
	dbPath := filepath.Join(dir, "vxd.db")
	if err := os.MkdirAll(filepath.Join(dbPath, "x"), 0o755); err != nil { // a non-empty directory cannot be os.Remove'd
		t.Fatal(err)
	}
	err := discardFailedRebuild(dbPath)
	if err == nil || !strings.Contains(err.Error(), "remove "+dbPath) {
		t.Fatalf("want the failed removal named, got %v", err)
	}
}

// TestFailedRebuildErr_CleanupFailed: when the partial cannot be removed the
// error carries the manual recipe — with the backup to move back, or without
// one — and the cleanup errors on one line.
func TestFailedRebuildErr_CleanupFailed(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "vxd.db")
	if err := os.MkdirAll(filepath.Join(dbPath, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, bak, want string }{
		{"with backup", dbPath + ".bak-1", dbPath + ".bak-1"},
		{"no backup", "", "re-run vxd replay"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := failedRebuildErr(errProject, dbPath, tc.bak)
			if !errors.Is(err, errProject) {
				t.Fatalf("the cause must survive, got %v", err)
			}
			msg := err.Error()
			if !strings.Contains(msg, "cleanup failed") || !strings.Contains(msg, "delete it and its -wal/-shm sidecars first") || !strings.Contains(msg, tc.want) {
				t.Fatalf("want the manual recipe, got %v", err)
			}
			if strings.Contains(msg, "\n") {
				t.Fatalf("the error must be one line, got %q", msg)
			}
			if tc.bak == "" && strings.Contains(msg, ".bak-") {
				t.Fatalf("with no backup there is no file to name: %v", err)
			}
		})
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

// TestReplay_OrphanSidecarsWithoutDB: a -wal with no vxd.db beside it is not
// moved aside (backupProjectionDB stats the main file), so the fresh store
// opens next to it and the clean-up removes it with everything else.
func TestReplay_OrphanSidecarsWithoutDB(t *testing.T) {
	dir, projectDir := setupReplayEnv(t)
	_, _ = seedReplayEvents(t, projectDir)
	dbPath := filepath.Join(projectDir, "vxd.db")
	if err := os.Remove(dbPath); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.WriteFile(dbPath+"-wal", []byte("orphan"), 0o600); err != nil {
		t.Fatal(err)
	}
	swapProjectionLeftover(t, errClose)

	cmd, _ := newReplayTestCmd(t, dir)
	err := cmd.Execute()
	if !errors.Is(err, errClose) {
		t.Fatalf("a close failure must fail the command, got %v", err)
	}
	assertFailedRebuildErr(t, err, projectDir, "")
	if baks, _ := filepath.Glob(dbPath + ".bak-*"); len(baks) != 0 {
		t.Fatalf("no database existed, so nothing was moved aside: %v", baks)
	}
}

// TestReplay_CloseFailure_NoEvents: with an empty event log nothing is
// applied, so the close error is the command's only signal that the rebuild
// did not finish. It must still fail, and still leave nothing behind.
func TestReplay_CloseFailure_NoEvents(t *testing.T) {
	dir, projectDir := setupReplayEnv(t)
	_, _ = seedReplayEvents(t, projectDir)
	before := snapshotDB(t, projectDir)
	// The projection exists and is backed up; the log is empty, so the
	// rebuild applies nothing and the close is the only thing that can fail.
	if err := os.WriteFile(filepath.Join(projectDir, "events.jsonl"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	swapProjectionLeftover(t, errClose)

	cmd, buf := newReplayTestCmd(t, dir)
	err := cmd.Execute()
	if !errors.Is(err, errClose) {
		t.Fatalf("a close failure with no events must fail the command, got %v", err)
	}
	if strings.Contains(buf.String(), "Projection rebuilt") {
		t.Fatalf("a failed rebuild must not report success:\n%s", buf.String())
	}
	assertFailedRebuildErr(t, err, projectDir, assertBackupOnDisk(t, projectDir, before))
}

// TestLatestBackup_IgnoresSidecars: the glob matches the backup's -wal and
// -shm too, and they sort after it. Naming one as the file to restore would
// have an operator move a WAL file onto vxd.db.
func TestLatestBackup_IgnoresSidecars(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "vxd.db")
	for _, name := range []string{
		"vxd.db.bak-20260919-101010", "vxd.db.bak-20260919-101010-wal", "vxd.db.bak-20260919-101010-shm",
		"vxd.db.bak-20260918-090000", "vxd.db.bak-20260918-090000-wal",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := latestBackup(dbPath), filepath.Join(dir, "vxd.db.bak-20260919-101010"); got != want {
		t.Fatalf("latestBackup = %q, want the newest database (not a sidecar) %q", got, want)
	}
	if got := latestBackup(filepath.Join(t.TempDir(), "vxd.db")); got != "" {
		t.Fatalf("no backups, no name: %q", got)
	}
	// Sidecars alone are not a backup: there is nothing to restore.
	only := t.TempDir()
	if err := os.WriteFile(filepath.Join(only, "vxd.db.bak-20260919-101010-wal"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := latestBackup(filepath.Join(only, "vxd.db")); got != "" {
		t.Fatalf("a lone sidecar is not a backup: %q", got)
	}
}

// TestRestoreHint: one sentence, three states of the disk.
func TestRestoreHint(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "vxd.db")
	bak := dbPath + ".bak-20260919-101010"

	if got := restoreHint(dbPath, bak); !strings.Contains(got, bak) || strings.Contains(got, "check its timestamp") {
		t.Fatalf("this run's backup is named as the previous database: %q", got)
	}
	if got := restoreHint(dbPath, ""); got != "re-run vxd replay" {
		t.Fatalf("nothing on disk, nothing to name: %q", got)
	}
	if err := os.WriteFile(bak, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	got := restoreHint(dbPath, "")
	if !strings.Contains(got, bak) || !strings.Contains(got, "check its timestamp") {
		t.Fatalf("a backup this run did not take must be named with its caveat: %q", got)
	}
}

// TestLatestBackup_GlobMetacharInPath: the project directory is the
// operator's, and a [ in it would make a pattern match nothing — leaving a
// failed replay with no backup named at all.
func TestLatestBackup_GlobMetacharInPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "proj[1]")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "vxd.db")
	bak := dbPath + ".bak-20260919-101010"
	if err := os.WriteFile(bak, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := latestBackup(dbPath); got != bak {
		t.Fatalf("latestBackup = %q, want %q", got, bak)
	}
}

// TestReplay_Success_KeepsTheRebuildAndNamesTheBackup: the failure paths
// remove vxd.db, so the success path is pinned too.
func TestReplay_Success_KeepsTheRebuildAndNamesTheBackup(t *testing.T) {
	dir, projectDir := setupReplayEnv(t)
	_, _ = seedReplayEvents(t, projectDir)
	before := snapshotDB(t, projectDir)

	cmd, buf := newReplayTestCmd(t, dir)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("replay: %v\n%s", err, buf.String())
	}
	if _, err := os.Stat(filepath.Join(projectDir, "vxd.db")); err != nil {
		t.Fatalf("a successful replay keeps its rebuild: %v", err)
	}
	bak := assertBackupOnDisk(t, projectDir, before)
	for _, want := range []string{"Projection rebuilt", "Previous database backed up: " + bak} {
		if !strings.Contains(buf.String(), want) {
			t.Fatalf("output lacks %q:\n%s", want, buf.String())
		}
	}
}
