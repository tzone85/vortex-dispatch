package preflight

import (
	"fmt"
	"os"
)

// minFreeDiskBytes is the free-space floor below which CheckDiskSpace fails.
// The event log (events.jsonl, fsync'd append-only) and the SQLite projection
// (WAL mode) both live under the state dir; running them onto a full disk
// corrupts neither but strands the pipeline mid-requirement, so the operator
// is warned before dispatch rather than at append time (audit finding O-01).
const minFreeDiskBytes = 1 << 30 // 1 GiB

// CheckDiskSpace warns when the filesystem holding the state dir is nearly
// full. It probes $HOME (the parent of the default ~/.vxd state dir); projects
// overriding workspace.state_dir to another filesystem are rare enough that a
// probe of the common location is the right cost/coverage trade-off.
func CheckDiskSpace() Result {
	probe, err := os.UserHomeDir()
	if err != nil || probe == "" {
		probe = "."
	}
	free, ferr := freeDiskBytes(probe)
	return evaluateDiskSpace(probe, free, ferr)
}

// evaluateDiskSpace is the pure decision half of CheckDiskSpace, split out so
// thresholds are unit-testable without a filesystem.
func evaluateDiskSpace(path string, free uint64, err error) Result {
	if err != nil {
		return Result{Name: "disk_space", Severity: SeverityWarning, Passed: false,
			Message: fmt.Sprintf("Could not determine free disk space at %s: %v", path, err)}
	}
	if free < minFreeDiskBytes {
		return Result{Name: "disk_space", Severity: SeverityWarning, Passed: false,
			Message: fmt.Sprintf("Low disk space: %s free at %s — event-log appends and SQLite writes may fail mid-requirement; free at least %s",
				humanBytes(free), path, humanBytes(minFreeDiskBytes))}
	}
	return Result{Name: "disk_space", Severity: SeverityWarning, Passed: true,
		Message: fmt.Sprintf("Disk space OK (%s free at %s)", humanBytes(free), path)}
}

// humanBytes renders a byte count in binary units for operator-facing messages.
func humanBytes(b uint64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
