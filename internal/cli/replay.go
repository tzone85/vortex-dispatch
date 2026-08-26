package cli

import (
	"bufio"
	"encoding/json"
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
WAL/SHM sidecars) before a fresh one is created, so a replay can be undone
by restoring the backup.

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

	ps, err := state.NewSQLiteStore(dbPath)
	if err != nil {
		return fmt.Errorf("create fresh projection store: %w", err)
	}
	defer ps.Close()

	applied := 0
	for _, evt := range events {
		if err := ps.Project(evt); err != nil {
			return fmt.Errorf("project event %s (%s) at index %d: %w", evt.ID, evt.Type, applied, err)
		}
		applied++
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

	mainBak := dbPath + ".bak-" + time.Now().UTC().Format("20060102-150405")
	for _, suffix := range []string{"", "-wal", "-shm"} {
		src := dbPath + suffix
		if _, err := os.Stat(src); err != nil {
			continue // sidecar may not exist
		}
		if err := os.Rename(src, mainBak+suffix); err != nil {
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
