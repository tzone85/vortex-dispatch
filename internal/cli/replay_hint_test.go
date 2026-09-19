package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

// Which backup a failed replay tells the operator to restore: the directory
// search behind it and the sentences restoreHint can produce. The rebuild
// and the clean-up themselves are in replay_recovery_test.go.

// newestBackup is the first name backupsBeside returns, or "".
func newestBackup(t *testing.T, dbPath string) string {
	t.Helper()
	baks, err := backupsBeside(dbPath)
	if err != nil {
		t.Fatalf("list backups beside %s: %v", dbPath, err)
	}
	if len(baks) == 0 {
		return ""
	}
	return baks[0]
}

// swapBackupNow pins the stamps backupProjectionDB writes, so two runs inside
// one second produce two distinct backups without the test sleeping. Not safe
// with t.Parallel(): it swaps a package var.
func swapBackupNow(t *testing.T, stamps ...time.Time) {
	t.Helper()
	prev := backupNow
	i := 0
	backupNow = func() time.Time {
		if i < len(stamps) {
			i++
			return stamps[i-1]
		}
		return prev()
	}
	t.Cleanup(func() { backupNow = prev })
}

// TestReplay_FailureWithOlderBackup_NamesOnlyThisRunsBackup: a project that
// replayed successfully months ago still has that backup beside the database.
// The failure names the one it just took, and says another exists — ranking
// them would be a guess, and the older one may be from before everything
// since.
func TestReplay_FailureWithOlderBackup_NamesOnlyThisRunsBackup(t *testing.T) {
	dir, projectDir := setupReplayEnv(t)
	_, _ = seedReplayEvents(t, projectDir)
	old := filepath.Join(projectDir, "vxd.db.bak-20260301-120000")
	if err := os.WriteFile(old, []byte("a successful replay's backup, months ago"), 0o600); err != nil {
		t.Fatal(err)
	}
	swapBackupNow(t, time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC))
	swapProjectionFailingReal(t, 3)

	cmd, _ := newReplayTestCmd(t, dir)
	err := cmd.Execute()
	if !errors.Is(err, errProject) {
		t.Fatalf("run: %v", err)
	}
	msg := err.Error()
	if strings.Contains(msg, old) {
		t.Fatalf("a successful replay's old backup is not an earlier failure's database: %v", err)
	}
	if !strings.Contains(msg, filepath.Join(projectDir, "vxd.db.bak-20260919-100000")) {
		t.Fatalf("the hint must name the backup this run took, got %v", err)
	}
}

// TestBackupsBeside_IgnoresSidecars: a directory listing turns up the
// backup's -wal and -shm too, and they sort after it. Naming one as the file
// to restore would have an operator move a WAL file onto vxd.db.
func TestBackupsBeside_IgnoresSidecars(t *testing.T) {
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
	if got, want := newestBackup(t, dbPath), filepath.Join(dir, "vxd.db.bak-20260919-101010"); got != want {
		t.Fatalf("newest backup = %q, want the newest database (not a sidecar) %q", got, want)
	}
	if got := newestBackup(t, filepath.Join(t.TempDir(), "vxd.db")); got != "" {
		t.Fatalf("no backups, no name: %q", got)
	}
	// Sidecars alone are not a backup: there is nothing to restore.
	only := t.TempDir()
	if err := os.WriteFile(filepath.Join(only, "vxd.db.bak-20260919-101010-wal"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := newestBackup(t, filepath.Join(only, "vxd.db")); got != "" {
		t.Fatalf("a lone sidecar is not a backup: %q", got)
	}
}

// TestBackupsBeside_IgnoresNonTimestampNames: only a name this command wrote
// is a file to restore. vxd.db.bak-keep sorts after every digit, so a hint
// that took the lexically newest name would send an operator to it.
func TestBackupsBeside_IgnoresNonTimestampNames(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "vxd.db")
	if err := os.WriteFile(filepath.Join(dir, "vxd.db.bak-20260919-101010"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vxd.db.bak-keep"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "vxd.db.bak-zz"), 0o755); err != nil {
		t.Fatal(err)
	}
	baks, err := backupsBeside(dbPath)
	if err != nil {
		t.Fatalf("list backups: %v", err)
	}
	if len(baks) != 1 || filepath.Base(baks[0]) != "vxd.db.bak-20260919-101010" {
		t.Fatalf("only the timestamped backup is one: %v", baks)
	}
}

// TestRestoreHint: one sentence per state of the disk.
func TestRestoreHint(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "vxd.db")
	bak := dbPath + ".bak-20260919-101010"

	if got := restoreHint(dbPath, bak); !strings.Contains(got, bak) || strings.Contains(got, "UTC timestamp") {
		t.Fatalf("this run's backup is named as the previous database, with no caveat: %q", got)
	}
	if got := restoreHint(dbPath, ""); got != "re-run vxd replay" {
		t.Fatalf("nothing on disk, nothing to name: %q", got)
	}
	if err := os.WriteFile(bak, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	got := restoreHint(dbPath, "")
	if !strings.Contains(got, bak) || !strings.Contains(got, "check its UTC timestamp") {
		t.Fatalf("a backup this run did not take must be named with its caveat: %q", got)
	}

	// A directory that cannot be listed is said out loud, not answered as
	// though there were no backups.
	missing := filepath.Join(t.TempDir(), "gone", "vxd.db")
	if got := restoreHint(missing, ""); !strings.Contains(got, "could not be listed") || strings.Contains(got, ".bak-") {
		t.Fatalf("a listing failure must be reported: %q", got)
	}

	// With more than one backup beside the database, the hint names the one
	// this run took and says the others exist — it does not rank them.
	older := dbPath + ".bak-20260301-120000"
	if err := os.WriteFile(older, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	got = restoreHint(dbPath, bak)
	if !strings.Contains(got, "the previous database is at "+bak) {
		t.Fatalf("this run's backup is still the one it names: %q", got)
	}
	if !strings.Contains(got, "2 backups are on disk") || !strings.Contains(got, "compare their UTC timestamps") {
		t.Fatalf("the ambiguity must be stated, not resolved: %q", got)
	}
	if strings.Contains(got, older) {
		t.Fatalf("naming the older one would be a guess: %q", got)
	}
}

// TestReplay_SameSecondBackup_Refuses: the stamp has one-second resolution
// and os.Rename replaces silently, so a second replay inside the same second
// would write over the only copy of the database. It refuses instead.
func TestReplay_SameSecondBackup_Refuses(t *testing.T) {
	dir, projectDir := setupReplayEnv(t)
	_, _ = seedReplayEvents(t, projectDir)
	before := snapshotDB(t, projectDir)
	t1 := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	swapBackupNow(t, t1, t1) // the same second twice
	swapProjectionFailingReal(t, 3)

	cmd, _ := newReplayTestCmd(t, dir)
	if err := cmd.Execute(); !errors.Is(err, errProject) {
		t.Fatalf("first run: %v", err)
	}
	bak := assertBackupOnDisk(t, projectDir, before)

	// Something re-creates vxd.db, so the second run has one to move aside.
	ps, err := state.NewSQLiteStore(filepath.Join(projectDir, "vxd.db"))
	if err != nil {
		t.Fatalf("re-create the projection: %v", err)
	}
	if err := ps.Close(); err != nil {
		t.Fatal(err)
	}

	cmd, _ = newReplayTestCmd(t, dir)
	err = cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("a second backup in the same second must be refused, got %v", err)
	}
	got, rerr := os.ReadFile(bak)
	if rerr != nil {
		t.Fatalf("read the backup: %v", rerr)
	}
	if !bytes.Equal(got, before) {
		t.Fatalf("the first backup must be untouched (%d bytes, want %d)", len(got), len(before))
	}
}

// TestBackupsBeside_GlobMetacharInPath: the project directory is the
// operator's, and a [ in it would make a pattern match nothing — leaving a
// failed replay with no backup named at all.
func TestBackupsBeside_GlobMetacharInPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "proj[1]")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "vxd.db")
	bak := dbPath + ".bak-20260919-101010"
	if err := os.WriteFile(bak, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := newestBackup(t, dbPath); got != bak {
		t.Fatalf("newest backup = %q, want %q", got, bak)
	}
}

// TestBackupProjectionDB_SameSecondSidecarOnly_Refuses: a backup sidecar can
// outlive its main file — a hand-restore that moved only the database, or a
// half-move that failed on the -wal. os.Rename would write over it without a
// word, so the guard checks each of the trio, not only vxd.db.bak-<ts>.
func TestBackupProjectionDB_SameSecondSidecarOnly_Refuses(t *testing.T) {
	projectDir := t.TempDir()
	dbPath := filepath.Join(projectDir, "vxd.db")
	if err := os.WriteFile(dbPath, []byte("database"), 0o600); err != nil {
		t.Fatal(err)
	}
	t1 := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	swapBackupNow(t, t1)
	orphan := dbPath + ".bak-" + t1.Format(backupStampLayout) + "-wal"
	want := []byte("the only copy of a wal")
	if err := os.WriteFile(orphan, want, 0o600); err != nil {
		t.Fatal(err)
	}

	bak, err := backupProjectionDB(dbPath)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("backupProjectionDB = %q, %v; want a refusal naming the orphan", bak, err)
	}
	if !strings.Contains(err.Error(), orphan) {
		t.Fatalf("the refusal must name the file in the way (%s), got %v", orphan, err)
	}
	got, rerr := os.ReadFile(orphan)
	if rerr != nil {
		t.Fatalf("read the orphan sidecar: %v", rerr)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("the orphan sidecar must be untouched, got %q", got)
	}
	if _, serr := os.Stat(dbPath); serr != nil {
		t.Fatalf("a refusal moves nothing: %v", serr)
	}
}
