package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

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
// sidecar filter is deliberately its own copy of backupsBeside's: a test that
// shares the code it checks proves nothing. It reads the directory for the
// same reason backupsBeside does, rather than globbing a path.
func assertBackupOnDisk(t *testing.T, projectDir string, want []byte) string {
	t.Helper()
	ents, err := os.ReadDir(projectDir)
	if err != nil {
		t.Fatalf("read %s: %v", projectDir, err)
	}
	var all, baks []string
	for _, e := range ents {
		if !strings.HasPrefix(e.Name(), "vxd.db.bak-") {
			continue
		}
		all = append(all, e.Name())
		if !strings.HasSuffix(e.Name(), "-wal") && !strings.HasSuffix(e.Name(), "-shm") {
			baks = append(baks, filepath.Join(projectDir, e.Name())) // the sidecars travel with their database
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
	// and the search that finds it must not offer the -wal as the database.
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
	for _, tc := range []struct{ name, bakSuffix, want string }{
		{"with backup", ".bak-20260919-101010", ".bak-20260919-101010"},
		{"no backup", "", "re-run vxd replay"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A directory per case, so one case's leftovers cannot be
			// what the next one reads.
			dbPath := filepath.Join(t.TempDir(), "vxd.db")
			if err := os.MkdirAll(filepath.Join(dbPath, "x"), 0o755); err != nil {
				t.Fatal(err)
			}
			bak, want := "", tc.want
			if tc.bakSuffix != "" {
				bak, want = dbPath+tc.bakSuffix, dbPath+tc.want
			}
			err := failedRebuildErr(errProject, dbPath, bak)
			if !errors.Is(err, errProject) {
				t.Fatalf("the cause must survive, got %v", err)
			}
			msg := err.Error()
			if !strings.Contains(msg, "cleanup failed") || !strings.Contains(msg, "delete it and its -wal/-shm sidecars first") || !strings.Contains(msg, want) {
				t.Fatalf("want the manual recipe, got %v", err)
			}
			if strings.Contains(msg, "\n") {
				t.Fatalf("the error must be one line, got %q", msg)
			}
			if bak == "" && strings.Contains(msg, ".bak-") {
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

// TestReplay_PartialBackupMove_NamesWhereTheDatabaseWent: backupProjectionDB
// moves the trio one file at a time. If the database has moved and a sidecar
// cannot follow, the operator is left with no vxd.db, so an error naming only
// the sidecar would not say where the database went.
func TestReplay_PartialBackupMove_NamesWhereTheDatabaseWent(t *testing.T) {
	dir, projectDir := setupReplayEnv(t)
	_, _ = seedReplayEvents(t, projectDir)
	t1 := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	swapBackupNow(t, t1)
	stamp := t1.Format(backupStampLayout)
	if err := os.WriteFile(filepath.Join(projectDir, "vxd.db-wal"), []byte("wal"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A non-empty directory cannot be renamed over, so the -wal move fails
	// after the database itself has already moved.
	blocked := filepath.Join(projectDir, "vxd.db.bak-"+stamp+"-wal")
	if err := os.MkdirAll(filepath.Join(blocked, "x"), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd, _ := newReplayTestCmd(t, dir)
	err := cmd.Execute()
	if err == nil {
		t.Fatal("a sidecar that cannot be moved must fail the command")
	}
	mainBak := filepath.Join(projectDir, "vxd.db.bak-"+stamp)
	if !strings.Contains(err.Error(), mainBak) {
		t.Fatalf("the error must say where the database went (%s), got %v", mainBak, err)
	}
	if _, serr := os.Stat(mainBak); serr != nil {
		t.Fatalf("the database really is at %s: %v", mainBak, serr)
	}
}

// TestReplay_PartialBackupMove_LeavesNoFreshDB: the command returns before
// openProjection, so the half-moved backup is not joined on disk by a fresh
// empty vxd.db — which would become the newest "previous database" on the
// next run, and read as the one to restore.
func TestReplay_PartialBackupMove_LeavesNoFreshDB(t *testing.T) {
	dir, projectDir := setupReplayEnv(t)
	_, _ = seedReplayEvents(t, projectDir)
	t1 := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	swapBackupNow(t, t1)
	stamp := t1.Format(backupStampLayout)
	if err := os.WriteFile(filepath.Join(projectDir, "vxd.db-wal"), []byte("wal"), 0o600); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(projectDir, "vxd.db.bak-"+stamp+"-wal")
	if err := os.MkdirAll(filepath.Join(blocked, "x"), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd, _ := newReplayTestCmd(t, dir)
	if err := cmd.Execute(); err == nil {
		t.Fatal("a sidecar that cannot be moved must fail the command")
	}
	if _, err := os.Stat(filepath.Join(projectDir, "vxd.db")); !os.IsNotExist(err) {
		t.Fatalf("nothing may re-create vxd.db beside a half-moved backup (stat err: %v)", err)
	}
}
