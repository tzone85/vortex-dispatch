package preflight

// Severity classifies the impact of a failed check.
type Severity int

const (
	SeverityCritical Severity = iota // Blocks execution
	SeverityWarning                  // Prints but proceeds
	SeverityInfo                     // Shown only in vxd preflight
)

// Result is the outcome of a single pre-flight check.
type Result struct {
	Name     string
	Severity Severity
	Passed   bool
	Message  string
}

// Check is a function that validates one aspect of the environment.
type Check func() Result

// Report is the collected output of all checks.
type Report struct {
	Results     []Result
	HasCritical bool
	HasWarning  bool
}

// RunAll executes all checks and collects results into a Report.
func RunAll(checks []Check) Report {
	var report Report
	for _, check := range checks {
		result := check()
		report.Results = append(report.Results, result)
		if !result.Passed {
			switch result.Severity {
			case SeverityCritical:
				report.HasCritical = true
			case SeverityWarning:
				report.HasWarning = true
			}
		}
	}
	return report
}
