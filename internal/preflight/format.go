package preflight

import (
	"fmt"
	"io"
	"strings"
)

// FormatVerbose prints all results with icons and a summary line.
// Used by `vxd preflight`.
func FormatVerbose(w io.Writer, report Report) {
	fmt.Fprintln(w, "VXD Pre-Flight Check")
	fmt.Fprintln(w, strings.Repeat("\u2500", 21))
	fmt.Fprintln(w)

	for _, r := range report.Results {
		icon := iconFor(r)
		fmt.Fprintf(w, "  %s %s\n", icon, r.Message)
	}

	fmt.Fprintln(w)

	critCount := 0
	warnCount := 0
	for _, r := range report.Results {
		if !r.Passed {
			switch r.Severity {
			case SeverityCritical:
				critCount++
			case SeverityWarning:
				warnCount++
			}
		}
	}

	switch {
	case critCount > 0 && warnCount > 0:
		fmt.Fprintf(w, "%d critical, %d warnings. Cannot dispatch.\n", critCount, warnCount)
	case critCount > 0:
		fmt.Fprintf(w, "%d critical issues. Cannot dispatch.\n", critCount)
	case warnCount > 0:
		fmt.Fprintf(w, "%d warnings. Ready to dispatch (non-critical).\n", warnCount)
	default:
		fmt.Fprintln(w, "All checks passed. Ready to dispatch.")
	}
}

// FormatCompact prints only failures. Used implicitly before vxd req / vxd resume.
// Prints nothing if all checks pass.
func FormatCompact(w io.Writer, report Report) {
	for _, r := range report.Results {
		if r.Passed {
			continue
		}
		switch r.Severity {
		case SeverityCritical:
			fmt.Fprintf(w, "\u2717 Pre-flight: %s\n", r.Message)
		case SeverityWarning:
			fmt.Fprintf(w, "\u26a0 Pre-flight: %s\n", r.Message)
		}
	}
	if report.HasCritical {
		critCount := 0
		for _, r := range report.Results {
			if !r.Passed && r.Severity == SeverityCritical {
				critCount++
			}
		}
		fmt.Fprintf(w, "Aborting: %d critical issues must be resolved before dispatching.\n", critCount)
	}
}

func iconFor(r Result) string {
	if r.Passed {
		if r.Severity == SeverityInfo {
			return "\u2139" // ℹ
		}
		return "\u2713" // ✓
	}
	switch r.Severity {
	case SeverityCritical:
		return "\u2717" // ✗
	case SeverityWarning:
		return "\u26a0" // ⚠
	default:
		return "\u2139" // ℹ
	}
}
