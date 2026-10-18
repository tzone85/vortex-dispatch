package improve

// Self-audit: the "look at yourself" half of the self-improvement engine.
//
// Where discovery.go/research.go look OUTWARD (upstream releases, external
// signals) and opportunities.go/proposal.go turn those into proposals, this
// file looks INWARD: it executes the repository's own toolchain and parses the
// REAL output into the same Finding/ScoredFinding/AuditEntry vocabulary the
// rest of the package already uses. Nothing here is simulated and nothing here
// invents a parallel finding type: SelfFinding embeds Finding, ScoredFindings()
// feeds RankFindings(), and AuditEntries() feeds the existing AuditLog.
//
// # The deterministic / judgement split (deliberate, enforced by IsDeterministic)
//
// Deterministic (safe to auto-apply in self-heal mode): the fix is produced by
// a tool, is byte-for-byte reproducible, and carries no semantic choice.
//
//	gofmt       -> `gofmt -w <file>`   formatting only, never changes behaviour
//	go mod tidy -> `go mod tidy`       manifest hygiene, verified by a re-build
//
// Judgement-required (reported, NEVER auto-applied): fixing it means choosing
// a behaviour, and a wrong choice is worse than the defect.
//
//	go build    -> a compile error. The right fix is a design decision.
//	go vet      -> a suspicious construct. Sometimes the vet report is the bug.
//	go test     -> a failing test. Auto-"fixing" tests is how you delete coverage.
//	govulncheck -> a CVE. The fix is a dependency or toolchain bump with its own
//	               blast radius, and may need a human to accept the upgrade.
//
// An unknown check defaults to judgement-required. Opt in, never opt out.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// CheckName identifies one self-audit check.
type CheckName string

// The self-audit checks. Values are stable; they are persisted in audit logs.
const (
	CheckBuild   CheckName = "go_build"
	CheckVet     CheckName = "go_vet"
	CheckTest    CheckName = "go_test"
	CheckVuln    CheckName = "govulncheck"
	CheckGofmt   CheckName = "gofmt"
	CheckModTidy CheckName = "go_mod_tidy"
)

// Severity grades a self-audit finding.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
)

// deterministicChecks is the allow-list of checks whose findings self-heal mode
// may fix without a human. Adding a check here is a deliberate act.
var deterministicChecks = map[CheckName]bool{
	CheckGofmt:   true,
	CheckModTidy: true,
}

// IsDeterministic reports whether findings from check may be auto-fixed.
// Unknown checks are judgement-required by default.
func IsDeterministic(check CheckName) bool {
	return deterministicChecks[check]
}

// SelfFinding is one defect found by the self-audit. It embeds the package's
// existing Finding so self-audit output flows through the same pipeline as
// externally discovered findings.
type SelfFinding struct {
	Finding

	Check         CheckName `json:"check"`
	Severity      Severity  `json:"severity"`
	Target        string    `json:"target"` // file:line:col, package path, or vuln ID
	Detail        string    `json:"detail"`
	Deterministic bool      `json:"deterministic"`
	FixCommand    string    `json:"fix_command,omitempty"`
}

func newSelfFinding(check CheckName, sev Severity, target, title, detail, fixCmd string) SelfFinding {
	return SelfFinding{
		Finding: Finding{
			Title:     title,
			Content:   detail,
			SourceURL: "local://self-audit/" + string(check),
			Category:  string(check),
			Direction: "internal",
			ScrapedAt: time.Now().UTC(),
		},
		Check:         check,
		Severity:      sev,
		Target:        target,
		Detail:        detail,
		Deterministic: IsDeterministic(check),
		FixCommand:    fixCmd,
	}
}

// CheckResult records one check's execution: what ran, what it returned, and
// what we made of it.
type CheckResult struct {
	Check    CheckName     `json:"check"`
	Command  string        `json:"command"`
	ExitCode int           `json:"exit_code"`
	Duration time.Duration `json:"duration"`
	Output   string        `json:"output"` // trimmed raw tool output
	Err      string        `json:"err,omitempty"`
	Skipped  bool          `json:"skipped"` // tool unavailable; result is not evidence of health
	Findings []SelfFinding `json:"findings"`
}

// SelfAuditReport is one full pass over the repository.
type SelfAuditReport struct {
	RepoRoot    string        `json:"repo_root"`
	StartedAt   time.Time     `json:"started_at"`
	CompletedAt time.Time     `json:"completed_at"`
	Results     []CheckResult `json:"results"`
	Findings    []SelfFinding `json:"findings"`
}

// Clean reports whether the audit found nothing at all.
func (r SelfAuditReport) Clean() bool { return len(r.Findings) == 0 }

// DeterministicFindings returns the findings self-heal mode may fix.
func (r SelfAuditReport) DeterministicFindings() []SelfFinding {
	return r.filter(true)
}

// JudgementFindings returns the findings that must be left to a human.
func (r SelfAuditReport) JudgementFindings() []SelfFinding {
	return r.filter(false)
}

func (r SelfAuditReport) filter(deterministic bool) []SelfFinding {
	var out []SelfFinding
	for _, f := range r.Findings {
		if f.Deterministic == deterministic {
			out = append(out, f)
		}
	}
	return out
}

// Skipped lists checks whose tool was unavailable. A clean report with skipped
// checks is NOT a clean bill of health, and callers must say so.
func (r SelfAuditReport) Skipped() []CheckResult {
	var out []CheckResult
	for _, res := range r.Results {
		if res.Skipped {
			out = append(out, res)
		}
	}
	return out
}

// severityScores maps a severity onto the 1-10 scales the existing analyzer
// (ScoredFinding/RankFindings) works in.
func severityScores(sev Severity) (relevance, impact, risk int) {
	switch sev {
	case SeverityCritical:
		return 10, 10, 2
	case SeverityHigh:
		return 8, 7, 3
	case SeverityMedium:
		return 6, 5, 3
	default:
		return 3, 2, 1
	}
}

// ScoredFindings converts the report into the analyzer's ScoredFinding form so
// self-audit output can be ranked by the existing RankFindings(). Only
// deterministic findings are Actionable, so judgement findings can never reach
// the LLM implementer.
func (r SelfAuditReport) ScoredFindings() []ScoredFinding {
	out := make([]ScoredFinding, 0, len(r.Findings))
	for _, f := range r.Findings {
		rel, imp, risk := severityScores(f.Severity)
		rel, imp, risk = clampScore(rel), clampScore(imp), clampScore(risk)
		reasoning := fmt.Sprintf("%s reported %s", f.Check, f.Target)
		effort := "medium"
		if f.Deterministic {
			reasoning += " (auto-fixable by " + f.FixCommand + ")"
			effort = "small"
		} else {
			reasoning += " (needs human judgement)"
		}
		out = append(out, ScoredFinding{
			Finding:    f.Finding,
			Relevance:  rel,
			Impact:     imp,
			Risk:       risk,
			Effort:     effort,
			Reasoning:  reasoning,
			Rank:       (imp * 2) + rel - risk,
			Actionable: f.Deterministic,
		})
	}
	return out
}

// AuditEntries converts the report into the existing AuditLog's entry type.
// Entries start as "proposed"; self-heal later flips deterministic ones to
// "implemented" or "aborted" (see MergeHealResult).
func (r SelfAuditReport) AuditEntries(runID string) []AuditEntry {
	date := runID
	if len(runID) >= 10 {
		date = runID[:10]
	}
	out := make([]AuditEntry, 0, len(r.Findings))
	for i, f := range r.Findings {
		rel, imp, risk := severityScores(f.Severity)
		reasoning := "needs human judgement"
		if f.Deterministic {
			reasoning = "deterministic, auto-fixable"
			if f.FixCommand != "" {
				reasoning += " by: " + f.FixCommand
			}
		}
		reasoning += "\n" + f.Detail
		out = append(out, AuditEntry{
			RunID:       runID,
			FindingID:   fmt.Sprintf("sa-%s-%03d", date, i+1),
			Source:      "self-audit",
			Category:    string(f.Check),
			Title:       f.Title,
			Relevance:   clampScore(rel),
			Impact:      clampScore(imp),
			Risk:        clampScore(risk),
			Disposition: "proposed",
			Reasoning:   reasoning,
		})
	}
	return out
}

// Markdown renders a human-readable report, split by remediation class.
func (r SelfAuditReport) Markdown() string {
	var b strings.Builder
	b.WriteString("# Self-Audit Report\n\n")
	fmt.Fprintf(&b, "- Repo: `%s`\n", r.RepoRoot)
	fmt.Fprintf(&b, "- Started: %s\n", r.StartedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "- Duration: %s\n\n", r.CompletedAt.Sub(r.StartedAt).Round(time.Millisecond))

	b.WriteString("## Checks\n\n")
	for _, res := range r.Results {
		status := "clean"
		switch {
		case res.Skipped:
			status = "SKIPPED (" + res.Err + ")"
		case len(res.Findings) > 0:
			status = fmt.Sprintf("%d finding(s)", len(res.Findings))
		}
		fmt.Fprintf(&b, "- `%s` — %s (exit %d, %s)\n", res.Command, status, res.ExitCode, res.Duration.Round(time.Millisecond))
	}

	det := r.DeterministicFindings()
	jud := r.JudgementFindings()

	fmt.Fprintf(&b, "\n## Deterministic findings (auto-fixable, %d)\n\n", len(det))
	if len(det) == 0 {
		b.WriteString("_none_\n")
	}
	for _, f := range det {
		fmt.Fprintf(&b, "- [%s] `%s` — %s (fix: `%s`)\n", f.Severity, f.Target, f.Title, f.FixCommand)
	}

	fmt.Fprintf(&b, "\n## Findings requiring human judgement (never auto-applied, %d)\n\n", len(jud))
	if len(jud) == 0 {
		b.WriteString("_none_\n")
	}
	for _, f := range jud {
		fmt.Fprintf(&b, "- [%s] %s — %s\n", f.Severity, f.Target, f.Title)
	}
	return b.String()
}

// CommandRunner executes a tool and returns its combined output and exit code.
// Injected so the audit logic is testable without a real toolchain.
type CommandRunner interface {
	Run(ctx context.Context, dir string, name string, args ...string) (output string, exitCode int, err error)
}

// ExecRunner is the real CommandRunner, backed by os/exec.
type ExecRunner struct{}

// Run executes name with args in dir, returning combined stdout+stderr.
func (ExecRunner) Run(ctx context.Context, dir string, name string, args ...string) (string, int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	code := 0
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		code = ee.ExitCode()
		err = nil // a non-zero exit is data, not a failure to run
	default:
		code = -1
	}
	return string(out), code, err
}

// SelfAuditor runs the self-audit checks over a repository.
type SelfAuditor struct {
	Root        string
	Runner      CommandRunner
	TestTimeout string // value for `go test -timeout`
	SkipTests   bool   // record go test as skipped instead of running it
}

// NewSelfAuditor returns a SelfAuditor for root. A nil runner means the real
// toolchain.
func NewSelfAuditor(root string, runner CommandRunner) *SelfAuditor {
	if runner == nil {
		runner = ExecRunner{}
	}
	return &SelfAuditor{Root: root, Runner: runner, TestTimeout: "15m"}
}

const maxCapturedOutput = 64 * 1024

func trimOutput(s string) string {
	if len(s) <= maxCapturedOutput {
		return s
	}
	return s[:maxCapturedOutput] + "\n...[truncated]"
}

// toolMissing reports whether err means "the tool is not installed".
func toolMissing(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "executable file not found") || strings.Contains(msg, exec.ErrNotFound.Error())
}

func (a *SelfAuditor) run(ctx context.Context, check CheckName, parse func(string) []SelfFinding, name string, args ...string) CheckResult {
	start := time.Now()
	out, code, err := a.Runner.Run(ctx, a.Root, name, args...)
	res := CheckResult{
		Check:    check,
		Command:  strings.TrimSpace(name + " " + strings.Join(args, " ")),
		ExitCode: code,
		Duration: time.Since(start),
		Output:   trimOutput(out),
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		res.Err = "cancelled: " + ctxErr.Error()
		res.Skipped = true
		return res
	}
	if err != nil {
		res.Err = err.Error()
		res.Skipped = true
		return res
	}
	if parse != nil {
		res.Findings = parse(out)
	}
	if code != 0 && len(res.Findings) == 0 {
		// The tool failed to run properly (e.g. govulncheck could not load a
		// broken package, go mod tidy hit a network error). Without findings
		// to explain the exit code this is not evidence of health.
		res.Err = fmt.Sprintf("exit %d without parseable findings: %s", code, firstLine(out))
		res.Skipped = true
	}
	return res
}

func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			return t
		}
	}
	return "(no output)"
}

// Audit executes every check and returns a report. A check whose tool cannot be
// run is marked Skipped; it never aborts the audit and never counts as clean.
func (a *SelfAuditor) Audit(ctx context.Context) (SelfAuditReport, error) {
	rep := SelfAuditReport{RepoRoot: a.Root, StartedAt: time.Now().UTC()}

	timeout := a.TestTimeout
	if timeout == "" {
		timeout = "15m"
	}

	buildResult := a.run(ctx, CheckBuild, ParseBuildOutput, "go", "build", "./...")

	testResult := CheckResult{
		Check:   CheckTest,
		Command: "go test -count=1 -timeout " + timeout + " ./...",
		Skipped: true,
		Err:     "skipped by flag",
	}
	switch {
	case a.SkipTests:
	case len(buildResult.Findings) > 0:
		// Tests cannot run on a broken build; the compile errors are already
		// reported, so do not spend minutes producing "[build failed]" noise.
		testResult.Err = "skipped: go build failed"
	default:
		testResult = a.run(ctx, CheckTest, ParseTestOutput, "go", "test", "-count=1", "-timeout", timeout, "./...")
	}

	rep.Results = append(rep.Results,
		buildResult,
		a.run(ctx, CheckVet, ParseVetOutput, "go", "vet", "./..."),
		a.run(ctx, CheckGofmt, ParseGofmtOutput, "gofmt", "-l", "."),
		testResult,
		a.run(ctx, CheckVuln, ParseGovulncheckOutput, "govulncheck", "./..."),
		a.run(ctx, CheckModTidy, ParseModTidyDrift, "go", "mod", "tidy", "-diff"),
	)

	buildTargets := map[string]bool{}
	for _, res := range rep.Results {
		if res.Check == CheckBuild {
			for _, f := range res.Findings {
				buildTargets[f.Target] = true
			}
		}
	}
	for _, res := range rep.Results {
		for _, f := range res.Findings {
			// go vet re-reports compile errors that go build already found.
			if f.Check == CheckVet && buildTargets[f.Target] {
				continue
			}
			rep.Findings = append(rep.Findings, f)
		}
	}
	sort.SliceStable(rep.Findings, func(i, j int) bool {
		ri, _, _ := severityScores(rep.Findings[i].Severity)
		rj, _, _ := severityScores(rep.Findings[j].Severity)
		return ri > rj
	})
	rep.CompletedAt = time.Now().UTC()
	return rep, nil
}

// --- parsers over REAL tool output --------------------------------------

// toolDiagnostic matches the shared `path:line[:col]: message` diagnostic shape
// emitted by the Go compiler and vet.
func parseDiagnostics(out string, check CheckName, sev Severity) []SelfFinding {
	var findings []SelfFinding
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// `go vet` prefixes the first diagnostic with "vet: ".
		trimmed = strings.TrimPrefix(trimmed, "vet: ")
		loc, msg, ok := splitDiagnostic(trimmed)
		if !ok {
			continue
		}
		findings = append(findings, newSelfFinding(check, sev, loc,
			fmt.Sprintf("%s: %s", loc, msg), trimmed, ""))
	}
	return findings
}

// splitDiagnostic splits "file.go:12:3: message" into location and message.
func splitDiagnostic(s string) (loc, msg string, ok bool) {
	parts := strings.SplitN(s, ": ", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	loc = strings.TrimSpace(parts[0])
	msg = strings.TrimSpace(parts[1])
	if !strings.Contains(loc, ".go:") || msg == "" {
		return "", "", false
	}
	return loc, msg, true
}

// ParseBuildOutput turns `go build ./...` output into findings. Compile errors
// are critical and always judgement-required.
func ParseBuildOutput(out string) []SelfFinding {
	return parseDiagnostics(out, CheckBuild, SeverityCritical)
}

// ParseVetOutput turns `go vet ./...` output into findings.
func ParseVetOutput(out string) []SelfFinding {
	return parseDiagnostics(out, CheckVet, SeverityHigh)
}

// ParseTestOutput turns `go test ./...` output into findings: one per failing
// test name and one per failing package. "[no test files]" is not a defect.
func ParseTestOutput(out string) []SelfFinding {
	var findings []SelfFinding
	seen := map[string]bool{}

	add := func(target, title, detail string) {
		if target == "" || seen[target] {
			return
		}
		seen[target] = true
		findings = append(findings, newSelfFinding(CheckTest, SeverityCritical, target, title, detail, ""))
	}

	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "--- FAIL:"):
			name := strings.TrimSpace(strings.TrimPrefix(trimmed, "--- FAIL:"))
			if i := strings.Index(name, " "); i > 0 {
				name = name[:i]
			}
			add(name, "failing test: "+name, trimmed)
		case strings.HasPrefix(trimmed, "FAIL\t"), strings.HasPrefix(line, "FAIL\t"):
			fields := strings.Split(strings.TrimPrefix(trimmed, "FAIL\t"), "\t")
			pkg := strings.TrimSpace(fields[0])
			// "pkg [build failed]" / "pkg [setup failed]": the target is the package.
			if i := strings.Index(pkg, " "); i > 0 {
				pkg = pkg[:i]
			}
			add(pkg, "failing package: "+pkg, trimmed)
		}
	}
	return findings
}

// ParseGofmtOutput turns `gofmt -l .` output (one path per line) into
// deterministic findings.
func ParseGofmtOutput(out string) []SelfFinding {
	var findings []SelfFinding
	for _, raw := range strings.Split(out, "\n") {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		findings = append(findings, newSelfFinding(CheckGofmt, SeverityLow, path,
			"gofmt drift: "+path,
			path+" is not gofmt-clean",
			"gofmt -w "+path))
	}
	return findings
}

// ParseModTidyDrift turns a non-empty go.mod/go.sum drift description into a
// single deterministic finding.
func ParseModTidyDrift(drift string) []SelfFinding {
	if strings.TrimSpace(drift) == "" || !looksLikeDiff(drift) {
		// `go mod tidy -diff` exits 1 both for drift and for real errors
		// (network, missing module). Only a unified diff is drift; anything
		// else is left for run() to classify as a failed check.
		return nil
	}
	return []SelfFinding{newSelfFinding(CheckModTidy, SeverityLow, "go.mod",
		"go.mod/go.sum are not tidy",
		strings.TrimSpace(drift),
		"go mod tidy")}
}

// looksLikeDiff reports whether s contains unified-diff hunk markers.
func looksLikeDiff(s string) bool {
	for _, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(l, "@@") || strings.HasPrefix(l, "+++ ") || strings.HasPrefix(l, "--- ") {
			return true
		}
	}
	return false
}

// ParseGovulncheckOutput parses govulncheck's text report. Symbol-level
// results (the vulnerable code is actually reachable) are critical; package-
// and module-level results (imported but not called) are medium. Vulnerability
// findings are never auto-fixed.
func ParseGovulncheckOutput(out string) []SelfFinding {
	var findings []SelfFinding
	sev := SeverityCritical
	var cur *SelfFinding

	flush := func() {
		if cur != nil {
			findings = append(findings, *cur)
			cur = nil
		}
	}

	for _, raw := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(trimmed, "=== Symbol Results"):
			flush()
			sev = SeverityCritical
		case strings.HasPrefix(trimmed, "=== Package Results"), strings.HasPrefix(trimmed, "=== Module Results"):
			flush()
			sev = SeverityMedium
		case strings.HasPrefix(trimmed, "Your code is affected"),
			strings.HasPrefix(trimmed, "This scan also found"),
			strings.HasPrefix(trimmed, "Use '-show verbose'"),
			strings.HasPrefix(trimmed, "No vulnerabilities found"):
			flush()
		case strings.HasPrefix(trimmed, "Vulnerability #"):
			flush()
			id := trimmed
			if i := strings.LastIndex(trimmed, ": "); i >= 0 {
				id = strings.TrimSpace(trimmed[i+2:])
			}
			f := newSelfFinding(CheckVuln, sev, id, "vulnerability "+id, "", "")
			cur = &f
		case cur != nil && trimmed != "":
			if cur.Finding.Title == "vulnerability "+cur.Target && !strings.HasPrefix(trimmed, "More info:") &&
				!strings.HasPrefix(trimmed, "Found in:") && !strings.HasPrefix(trimmed, "Fixed in:") &&
				!strings.HasPrefix(trimmed, "Example traces") && !strings.HasPrefix(trimmed, "#") {
				cur.Finding.Title = cur.Target + ": " + trimmed
			}
			if cur.Detail == "" {
				cur.Detail = trimmed
			} else {
				cur.Detail += "\n" + trimmed
			}
			cur.Finding.Content = cur.Detail
		}
	}
	flush()
	return findings
}

// validRepoRelativeGoFile reports whether p is a safe, existing, repo-relative
// .go file under root. Used before any path is handed to a fixing tool.
func validRepoRelativeGoFile(root, p string) bool {
	if p == "" || filepath.IsAbs(p) || !strings.HasSuffix(p, ".go") {
		return false
	}
	clean := filepath.Clean(p)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || strings.Contains(p, "..") {
		return false
	}
	info, err := os.Stat(filepath.Join(root, clean))
	return err == nil && info.Mode().IsRegular()
}

// MergeHealResult updates the deterministic entries after a self-heal attempt.
// entries and findings must be parallel slices from the same report. Judgement
// entries are never touched.
func MergeHealResult(entries []AuditEntry, findings []SelfFinding, res ImplementResult, attempted bool) []AuditEntry {
	if !attempted {
		return entries
	}
	out := make([]AuditEntry, len(entries))
	copy(out, entries)
	for i := range out {
		if i >= len(findings) || !findings[i].Deterministic {
			continue
		}
		switch {
		case res.IsImplemented():
			out[i].Disposition = "implemented"
			out[i].PRURL = res.PRURL
			out[i].TestsPassed = true
		case res.Error != "":
			out[i].Disposition = "aborted"
			out[i].Error = res.Error
		}
	}
	return out
}

// BuildSelfAuditEmail summarizes a report for the daily email.
func BuildSelfAuditEmail(rep SelfAuditReport, heal ImplementResult, healed bool) *EmailSelfAudit {
	det := len(rep.DeterministicFindings())
	jud := len(rep.JudgementFindings())
	e := &EmailSelfAudit{
		Summary: fmt.Sprintf("%d checks, %d findings (%d auto-fixable, %d need judgement), %d skipped",
			len(rep.Results), len(rep.Findings), det, jud, len(rep.Skipped())),
	}
	for _, res := range rep.Results {
		status := "clean"
		switch {
		case res.Skipped:
			status = "SKIPPED (" + res.Err + ")"
		case len(res.Findings) > 0:
			status = fmt.Sprintf("%d finding(s)", len(res.Findings))
		}
		e.Checks = append(e.Checks, fmt.Sprintf("%s: %s (exit %d, %s)",
			res.Check, status, res.ExitCode, res.Duration.Round(time.Millisecond)))
	}
	for _, f := range rep.Findings {
		e.Findings = append(e.Findings, EmailSection{
			Title:     fmt.Sprintf("[%s] %s — %s", f.Severity, f.Target, f.Title),
			Content:   truncateForEmail(f.Detail, 300),
			SourceURL: f.SourceURL,
		})
	}
	for _, s := range rep.Skipped() {
		e.Skipped = append(e.Skipped, string(s.Check)+": "+s.Err)
	}
	if healed && heal.IsImplemented() {
		e.HealPRURL = heal.PRURL
	}
	return e
}

func truncateForEmail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
