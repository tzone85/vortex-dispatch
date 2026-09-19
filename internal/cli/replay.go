package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/tzone85/vortex-dispatch/internal/engine"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

func newReplayCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "replay",
		Short: "Rebuild the SQLite projection from events.jsonl",
		Long: `Rebuild the materialized SQLite projection (vxd.db) from the append-only
event log (events.jsonl). Use this when the projection database is lost,
corrupt, or suspected of diverging from the event history.

The existing database is moved aside to vxd.db.bak-<timestamp> (with its
WAL/SHM sidecars) before a fresh one is created. A rebuild that fails removes
what it created at vxd.db and the error names the backup to move back.

The projection is disposable: events.jsonl is the source of truth. To undo a
replay by hand, see "Recovering from a failed replay":
https://github.com/tzone85/vortex-dispatch#recovering-from-a-failed-replay

Use --dry-run to validate the event log (decode every line, reporting
corrupt lines with line numbers) without touching SQLite.

Refuses to run while a live pipeline holds the project lock file.`,
		RunE: runReplay,
	}
	cmd.Flags().Bool("dry-run", false, "Validate the event log without modifying the SQLite projection")
	cmd.SilenceUsage = true
	return cmd
}

func runReplay(cmd *cobra.Command, _ []string) error {
	out := cmd.OutOrStdout()

	// Resolve config + project WITHOUT opening stores: loadStores would
	// recreate an empty vxd.db via NewSQLiteStore, defeating the whole point
	// of replaying after the database was lost or deleted.
	cfgPath, _ := cmd.Flags().GetString("config")
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return err
	}
	projectName, err := resolveProject(cmd)
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}
	projectDir := engine.ProjectDir(expandHome(cfg.Workspace.StateDir), projectName)
	eventsPath := filepath.Join(projectDir, "events.jsonl")
	dbPath := filepath.Join(projectDir, "vxd.db")

	dryRun, _ := cmd.Flags().GetBool("dry-run")

	// Refuse to run while a live pipeline holds the advisory lock. AcquireLock
	// reuses the PID-liveness check from internal/engine/lockfile_*.go: a dead
	// owner is reclaimed as stale, a live one fails with the owning req ID.
	lockPath := filepath.Join(projectDir, "vxd.lock")
	if _, err := engine.AcquireLock(lockPath, "replay"); err != nil {
		return fmt.Errorf("refusing to replay: %w", err)
	}
	defer engine.ReleaseLock(lockPath)

	// Validate the ENTIRE log before touching SQLite — in both modes. A
	// replay that silently skipped corrupt rows would rebuild a projection
	// that diverges from the source of truth.
	events, badLines, err := readEventsFile(eventsPath)
	if err != nil {
		return err
	}
	if len(badLines) > 0 {
		for _, b := range badLines {
			fmt.Fprintf(out, "corrupt line %d: %s\n", b.Line, b.Err)
		}
		return fmt.Errorf("event log has %d corrupt line(s) — fix events.jsonl before replaying", len(badLines))
	}

	if dryRun {
		fmt.Fprintf(out, "Event log OK: %d events, 0 corrupt lines\n", len(events))
		printEventTally(out, tallyEventTypes(events))
		fmt.Fprintln(out, "Dry run: SQLite projection not modified.")
		return nil
	}

	start := time.Now()

	bakPath, err := backupProjectionDB(dbPath)
	if err != nil {
		return fmt.Errorf("back up projection db: %w", err)
	}

	// The previous database is now at bakPath; every failure below goes
	// through failedRebuildErr.
	ps, err := openProjection(dbPath)
	if err != nil {
		return failedRebuildErr(fmt.Errorf("create fresh projection store: %w", err), dbPath, bakPath)
	}
	applied, err := rebuildAndClose(ps, events)
	if err != nil {
		return failedRebuildErr(err, dbPath, bakPath)
	}

	duration := time.Since(start).Round(time.Millisecond)
	fmt.Fprintf(out, "Replayed %d events from %s\n", applied, eventsPath)
	printEventTally(out, tallyEventTypes(events))
	fmt.Fprintf(out, "Projection rebuilt in %s: %s\n", duration, dbPath)
	if bakPath != "" {
		fmt.Fprintf(out, "Previous database backed up: %s\n", bakPath)
	}
	return nil
}

// failedRebuildErr is the one exit for a failure after the previous database
// was moved aside. Whatever is at dbPath is removed: left there, a re-run
// would move it aside as the newest "previous database", and its stale -wal
// would be replayed into a restored database. That includes a rebuild that
// applied every event and failed only to close, whose checkpoint may never
// have run.
func failedRebuildErr(cause error, dbPath, bakPath string) error {
	hint := restoreHint(dbPath, bakPath)
	if derr := discardFailedRebuild(dbPath); derr != nil {
		// errors.Join separates with newlines; the whole message is one line.
		return fmt.Errorf("%w; anything at %s is an untrusted partial rebuild: delete it and its -wal/-shm sidecars first; %s; cleanup failed: %s",
			cause, dbPath, hint, strings.ReplaceAll(derr.Error(), "\n", "; "))
	}
	return fmt.Errorf("%w; no partial rebuild left at %s; %s", cause, dbPath, hint)
}

// backupsBeside lists the backups beside dbPath, newest first. Only a name
// this command wrote counts, which is what parsing the stamp decides: a -wal
// or -shm sidecar is not a database and sorts after the one it belongs to
// (naming it would have an operator move a WAL file onto vxd.db), a
// hand-named vxd.db.bak-keep sorts after every digit and would otherwise win,
// and a directory is not a file to move back. Reading the directory rather
// than globbing the path: a project directory containing [, * or ? would make
// the pattern match nothing.
func backupsBeside(dbPath string) ([]string, error) {
	dir, prefix := filepath.Dir(dbPath), filepath.Base(dbPath)+".bak-"
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, prefix) {
			continue
		}
		if _, perr := time.Parse(backupStampLayout, strings.TrimPrefix(name, prefix)); perr != nil {
			continue
		}
		names = append(names, filepath.Join(dir, name))
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	return names, nil
}

// discardFailedRebuild removes whatever is at dbPath and its -wal/-shm
// sidecars (why: failedRebuildErr) — including an orphaned -wal that
// backupProjectionDB left, since a -wal without its database is unreadable. A
// missing file is fine.
func discardFailedRebuild(dbPath string) error {
	var errs []error
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(dbPath + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err) // a *PathError already reads "remove <path>: <reason>"
		}
	}
	return errors.Join(errs...)
}

// restoreHint is what both failure paths end with: what is on disk to restore
// from, and what to do with it. It reports; it does not decide. A run that
// moved something aside names it, and a run that did not can only report the
// newest backup it finds — either may be an earlier failure's, or a
// successful replay's from last month, and nothing on disk tells them apart.
// Where that ambiguity is real, the sentence says so rather than picking:
// "any command re-creates an empty vxd.db", so after one failed replay the
// newest backup can be that empty file and an older one the database.
func restoreHint(dbPath, bakPath string) string {
	if bakPath != "" {
		hint := fmt.Sprintf("the previous database is at %s: move it and any -wal/-shm beside it back to restore, or re-run vxd replay", bakPath)
		// A directory this run cannot list is not worth a second clause: the
		// backup it took is already named above, which is the answer.
		if baks, err := backupsBeside(dbPath); err == nil && len(baks) > 1 {
			hint += fmt.Sprintf("; %d backups are on disk, so if a replay already failed here, %s may be a database re-created since: compare their UTC timestamps first", len(baks), filepath.Base(bakPath))
		}
		return hint
	}
	baks, err := backupsBeside(dbPath)
	if err != nil {
		return fmt.Sprintf("this run moved nothing aside and the backups beside %s could not be listed (%v): look in that directory by hand, or re-run vxd replay", dbPath, err)
	}
	if len(baks) > 0 {
		return fmt.Sprintf("this run moved nothing aside; the newest backup on disk is %s (check its UTC timestamp before restoring it), or re-run vxd replay", baks[0])
	}
	return "re-run vxd replay"
}

// projectionSink is the slice of *state.SQLiteStore a rebuild needs. It is
// narrower than state.ProjectionStore on purpose: the rebuild only projects
// and closes, and a fake for the wider interface would carry dead methods.
type projectionSink interface {
	Project(state.Event) error
	Close() error
}

// openProjection opens the fresh projection store; tests swap it (like
// startBrowser in review_cmd.go) to drive runReplay's failure paths, which
// a real store cannot reach cheaply. On error the interface is nil, not a
// typed nil *SQLiteStore.
var openProjection = func(dbPath string) (projectionSink, error) {
	ps, err := state.NewSQLiteStore(dbPath)
	if err != nil {
		return nil, err
	}
	return ps, nil
}

// rebuildAndClose applies events in order and closes ps exactly once, so
// "Projection rebuilt" is never printed before the close has succeeded (the
// named-return idiom of engine.CreateBackup). A close failure is a failed
// recovery command even though each Project has already committed; a
// projection error explains the failure better, so it wins and the close
// error rides along.
func rebuildAndClose(ps projectionSink, events []state.Event) (applied int, retErr error) {
	defer func() {
		cerr := ps.Close()
		switch {
		case cerr == nil:
		case retErr == nil:
			retErr = fmt.Errorf("close rebuilt projection db: %w", cerr)
		default:
			// The projection error explains the failure; the close error is
			// still evidence about the file that is about to be removed, and
			// a caller has to be able to match it (fmt.Errorf takes several
			// %w verbs).
			retErr = fmt.Errorf("%w (close also failed: %w)", retErr, cerr)
		}
	}()
	for _, evt := range events {
		if err := ps.Project(evt); err != nil {
			return applied, fmt.Errorf("project event %s (%s) at index %d: %w", evt.ID, evt.Type, applied, err)
		}
		applied++
	}
	return applied, nil
}

// replayBadLine records a corrupt events.jsonl row with its 1-based line
// number so an operator can `sed -n '<N>p' events.jsonl` immediately.
type replayBadLine struct {
	Line int
	Err  string
}

// readEventsFile streams events.jsonl in file order (oldest first) and
// decodes every line. Corrupt lines are COLLECTED, not skipped: unlike the
// dashboard read path (which logs and continues so a single bad row cannot
// fault broad reads), a replay must see the whole truth or refuse to run.
func readEventsFile(path string) ([]state.Event, []replayBadLine, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("no event log found at %s — nothing to replay", path)
		}
		return nil, nil, fmt.Errorf("open event log: %w", err)
	}
	defer f.Close()

	var (
		events   []state.Event
		badLines []replayBadLine
	)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024) // match FileStore's max line size
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		if len(strings.TrimSpace(scanner.Text())) == 0 {
			continue // tolerate blank lines / trailing newline
		}
		var evt state.Event
		if err := json.Unmarshal(scanner.Bytes(), &evt); err != nil {
			badLines = append(badLines, replayBadLine{Line: lineNo, Err: err.Error()})
			continue
		}
		events = append(events, evt)
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("scan event log: %w", err)
	}
	return events, badLines, nil
}

// backupStampLayout is the fixed-width UTC stamp in vxd.db.bak-<ts>. Because
// it is fixed-width, those names sort lexically by age.
const backupStampLayout = "20060102-150405"

// backupNow stamps a backup's name. It is a package seam so a test can make
// two runs in the same second produce distinct backups — or collide on
// purpose — without sleeping.
var backupNow = func() time.Time { return time.Now().UTC() }

// backupProjectionDB moves the existing SQLite database (plus its WAL/SHM
// sidecars, kept together so the backup trio stays consistent) aside to
// *.bak-<timestamp>. Returns the main backup path, or "" when no database
// existed yet (fresh workspace).
func backupProjectionDB(dbPath string) (string, error) {
	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("stat %s: %w", dbPath, err)
	}

	mainBak := dbPath + ".bak-" + backupNow().Format(backupStampLayout)
	// The stamp is one-second resolution, so a second replay inside the same
	// second would rename over the first one's backup — os.Rename replaces
	// silently, and the backup is the only copy. The project lock keeps
	// pipelines out; this keeps a fast finger out.
	// Each of the trio is checked, not only the main file: a stray
	// vxd.db.bak-<ts>-wal (a hand-restore that moved only the database, or a
	// half-move from the branch below) would be renamed over in silence. A
	// DIRECTORY at one of those names is not a backup this command wrote —
	// backupsBeside ignores directories for the same reason — and rename
	// fails on it with an error that names it.
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if fi, err := os.Lstat(mainBak + suffix); err == nil && !fi.IsDir() {
			return "", fmt.Errorf("a backup from this second already exists at %s: wait a second and re-run vxd replay", mainBak+suffix)
		}
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		src := dbPath + suffix
		if _, err := os.Stat(src); err != nil {
			continue // sidecar may not exist
		}
		if err := os.Rename(src, mainBak+suffix); err != nil {
			if suffix != "" {
				// The database itself has already moved. Saying only that a
				// sidecar failed would leave the operator with no vxd.db and
				// nothing naming where it went.
				return "", fmt.Errorf("move %s aside (the database is already at %s: move it back before retrying): %w", src, mainBak, err)
			}
			return "", fmt.Errorf("move %s aside: %w", src, err)
		}
	}
	return mainBak, nil
}

func tallyEventTypes(events []state.Event) map[state.EventType]int {
	tally := make(map[state.EventType]int, len(events))
	for _, evt := range events {
		tally[evt.Type]++
	}
	return tally
}

// printEventTally renders the per-type counts sorted by type name so the
// output is deterministic across runs.
func printEventTally(out io.Writer, tally map[state.EventType]int) {
	if len(tally) == 0 {
		return
	}
	types := make([]string, 0, len(tally))
	for t := range tally {
		types = append(types, string(t))
	}
	sort.Strings(types)
	for _, t := range types {
		fmt.Fprintf(out, "  %-32s %d\n", t, tally[state.EventType(t)])
	}
}
