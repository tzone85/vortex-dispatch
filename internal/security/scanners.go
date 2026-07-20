package security

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ScannerKind identifies a security scanner the agent can orchestrate.
type ScannerKind string

const (
	ScannerSemgrep     ScannerKind = "semgrep"     // multi-language SAST
	ScannerGosec       ScannerKind = "gosec"       // Go SAST
	ScannerGovulncheck ScannerKind = "govulncheck" // Go dependency CVEs
	ScannerGitleaks    ScannerKind = "gitleaks"    // secret scanning (all langs)
	ScannerNpmAudit    ScannerKind = "npm-audit"   // Node dependency CVEs
)

// scannerTimeout bounds a single scanner invocation.
const scannerTimeout = 4 * time.Minute

// Scanner describes a tool: the PATH binary that gates availability and the
// languages it applies to (empty = all languages).
type Scanner struct {
	Kind      ScannerKind
	Bin       string
	Languages []string
}

// allScanners is the registry of scanners the agent knows how to run.
func allScanners() []Scanner {
	return []Scanner{
		{Kind: ScannerGitleaks, Bin: "gitleaks"}, // secrets — every language
		{Kind: ScannerSemgrep, Bin: "semgrep"},   // multi-language SAST
		{Kind: ScannerGosec, Bin: "gosec", Languages: []string{"go"}},
		{Kind: ScannerGovulncheck, Bin: "govulncheck", Languages: []string{"go"}},
		{Kind: ScannerNpmAudit, Bin: "npm", Languages: []string{"javascript", "typescript"}},
	}
}

func langMatch(scannerLangs, repoLangs []string) bool {
	if len(scannerLangs) == 0 {
		return true
	}
	for _, a := range scannerLangs {
		for _, b := range repoLangs {
			if strings.EqualFold(a, b) {
				return true
			}
		}
	}
	return false
}

// applicableScanners returns the scanners that are both relevant to the repo's
// languages and present in PATH (per the available set, keyed by Bin).
func applicableScanners(langs []string, available map[string]bool) []Scanner {
	var out []Scanner
	for _, s := range allScanners() {
		if !available[s.Bin] {
			continue
		}
		if !langMatch(s.Languages, langs) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// RunScanners runs every applicable+available scanner against repoDir and
// returns deduped findings, the scanners that ran clean, the applicable scanners
// that were skipped because they are not installed, and the scanners that ran
// but errored (exec crash, timeout, parse failure). A scanner failing never
// aborts the scan, but its failure is NOT silently swallowed: it is logged and
// reported in `failed` rather than counted as a clean run — otherwise a tool
// that failed to inspect the code is indistinguishable from one that found
// nothing, and the security gate would report a build as scanned-clean when
// coverage was actually lost.
func RunScanners(ctx context.Context, repoDir string) (findings []Finding, ran, skipped, failed []ScannerKind) {
	langs := DetectLanguages(repoDir)
	available := map[string]bool{}
	for _, s := range allScanners() {
		if _, err := exec.LookPath(s.Bin); err == nil {
			available[s.Bin] = true
		}
	}
	for _, s := range allScanners() {
		if !langMatch(s.Languages, langs) {
			continue
		}
		if !available[s.Bin] {
			skipped = append(skipped, s.Kind)
			continue
		}
		fs, err := s.Run(ctx, repoDir)
		if err != nil {
			// Graceful degradation: keep scanning with the other tools, but make
			// the coverage loss visible — a failed scan must never masquerade as
			// a clean one.
			failed = append(failed, s.Kind)
			log.Printf("[security] scanner %s failed (coverage lost for this tool): %v", s.Kind, err)
			continue
		}
		ran = append(ran, s.Kind)
		findings = append(findings, fs...)
	}
	return DedupeFindings(findings), ran, skipped, failed
}

// KnownScanners returns the full scanner registry regardless of PATH
// availability or repo languages, so other packages — e.g. preflight — can
// report on missing tools without duplicating the list.
func KnownScanners() []Scanner {
	return allScanners()
}

// InstallHint returns the install command for a scanner binary, or "" when no
// hint is known. Hints target macOS/Homebrew and the Go toolchain — the two
// supported operator setups.
func InstallHint(bin string) string {
	switch bin {
	case "gosec":
		return "go install github.com/securego/gosec/v2/cmd/gosec@latest"
	case "govulncheck":
		return "go install golang.org/x/vuln/cmd/govulncheck@latest"
	case "gitleaks":
		return "brew install gitleaks"
	case "semgrep":
		return "brew install semgrep"
	case "npm":
		return "brew install node"
	default:
		return ""
	}
}

// DetectScanners returns the scanners applicable to repoDir and available on the
// host. Detection combines language inspection with exec.LookPath.
func DetectScanners(repoDir string) []Scanner {
	langs := DetectLanguages(repoDir)
	available := map[string]bool{}
	for _, s := range allScanners() {
		if _, err := exec.LookPath(s.Bin); err == nil {
			available[s.Bin] = true
		}
	}
	return applicableScanners(langs, available)
}

// relPath makes an absolute scanner path repo-relative for stable, readable
// findings. Paths already relative (or outside repoDir) are returned cleaned.
func relPath(repoDir, p string) string {
	if rel, err := filepath.Rel(repoDir, p); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return p
}

// ---- Parsers (pure: tool output → findings) -------------------------------

func parseGosec(out []byte, repoDir string) ([]Finding, error) {
	var doc struct {
		Issues []struct {
			Severity string `json:"severity"`
			RuleID   string `json:"rule_id"`
			Details  string `json:"details"`
			File     string `json:"file"`
			Line     string `json:"line"`
			CWE      struct {
				ID string `json:"id"`
			} `json:"cwe"`
		} `json:"Issues"`
		// gosec reports compile/package-load failures in "Golang errors"
		// (filename → errors) while still emitting valid JSON with an empty
		// Issues list. Decoding only Issues would return a clean empty scan
		// for a run that could not actually analyse the code — the same
		// masquerade the other parsers guard against. Fail closed only when
		// no issues were produced, so a scan that found issues is never
		// flipped to failed by a per-file parse error alongside real results.
		GolangErrors map[string]json.RawMessage `json:"Golang errors"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return nil, err
	}
	if len(doc.Issues) == 0 && len(doc.GolangErrors) > 0 {
		return nil, fmt.Errorf("gosec reported %d package-load error(s) and no issues; scan coverage lost", len(doc.GolangErrors))
	}
	findings := make([]Finding, 0, len(doc.Issues))
	for _, i := range doc.Issues {
		line, _ := strconv.Atoi(strings.SplitN(i.Line, "-", 2)[0]) // gosec may emit "12-14"
		cwe := ""
		if i.CWE.ID != "" {
			cwe = "CWE-" + i.CWE.ID
		}
		findings = append(findings, Finding{
			Tool:     "gosec",
			RuleID:   i.RuleID,
			Severity: ParseSeverity(i.Severity),
			File:     relPath(repoDir, i.File),
			Line:     line,
			Title:    i.Details,
			Detail:   cwe,
			Source:   "scanner",
		})
	}
	return findings, nil
}

func parseGitleaks(out []byte, repoDir string) ([]Finding, error) {
	var rows []struct {
		Description string `json:"Description"`
		File        string `json:"File"`
		StartLine   int    `json:"StartLine"`
		RuleID      string `json:"RuleID"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, err
	}
	findings := make([]Finding, 0, len(rows))
	for _, r := range rows {
		findings = append(findings, Finding{
			Tool:     "gitleaks",
			RuleID:   r.RuleID,
			Severity: SeverityCritical, // a committed live secret is always critical
			File:     relPath(repoDir, r.File),
			Line:     r.StartLine,
			Title:    r.Description,
			Detail:   "Hardcoded secret detected (CWE-798)",
			Category: "Cryptographic Failures",
			Source:   "scanner",
		})
	}
	return findings, nil
}

func parseSemgrep(out []byte, repoDir string) ([]Finding, error) {
	var doc struct {
		Results []struct {
			CheckID string `json:"check_id"`
			Path    string `json:"path"`
			Start   struct {
				Line int `json:"line"`
			} `json:"start"`
			Extra struct {
				Message  string `json:"message"`
				Severity string `json:"severity"`
				Metadata struct {
					CWE   []string `json:"cwe"`
					OWASP []string `json:"owasp"`
				} `json:"metadata"`
			} `json:"extra"`
		} `json:"results"`
		// semgrep always emits a top-level "errors" array alongside "results".
		// On a scan-level failure (e.g. `--config auto` cannot load rules on an
		// offline dispatch host) it emits valid JSON with results:[] and an
		// error-level entry, and exits non-zero. Decoding only results would
		// return ([], nil) — counted as a clean `ran` scan rather than `failed`,
		// hiding total coverage loss from the gate and require_scanners strict
		// mode. Inspect errors so that masquerade is surfaced.
		Errors []struct {
			Level   string `json:"level"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return nil, err
	}
	// Fail closed only when no results were produced AND a fatal error is
	// present: a scan that returned findings clearly wasn't a total loss, so
	// non-fatal per-file warnings that coexist with results never flip a
	// successful scan to failed.
	if len(doc.Results) == 0 {
		for _, e := range doc.Errors {
			if strings.EqualFold(e.Level, "error") {
				msg := e.Message
				if msg == "" {
					msg = "semgrep reported a scan error with no results"
				}
				return nil, fmt.Errorf("semgrep scan error: %s", msg)
			}
		}
	}
	findings := make([]Finding, 0, len(doc.Results))
	for _, r := range doc.Results {
		cwe := ""
		if len(r.Extra.Metadata.CWE) > 0 {
			cwe = r.Extra.Metadata.CWE[0]
		}
		cat := ""
		if len(r.Extra.Metadata.OWASP) > 0 {
			cat = r.Extra.Metadata.OWASP[0]
		}
		findings = append(findings, Finding{
			Tool:     "semgrep",
			RuleID:   r.CheckID,
			Severity: ParseSeverity(r.Extra.Severity),
			File:     relPath(repoDir, r.Path),
			Line:     r.Start.Line,
			Title:    r.Extra.Message,
			Detail:   cwe,
			Category: cat,
			Source:   "scanner",
		})
	}
	return findings, nil
}

func parseNpmAudit(out []byte) ([]Finding, error) {
	var doc struct {
		// npm emits a valid-JSON error envelope (e.g. {"error":{"code":
		// "ENOLOCK",...}}) when the audit cannot actually run — most commonly
		// a package.json with no lockfile. That envelope unmarshals cleanly
		// into a nil Vulnerabilities map, so without inspecting Error the scan
		// would look like a clean pass and be counted in RunScanners' `ran`
		// list — exactly the "failed scan masquerading as clean" that the
		// `failed` list exists to prevent (and that require_scanners strict
		// mode relies on). Surface it as an error so coverage loss is visible.
		Error *struct {
			Code    string `json:"code"`
			Summary string `json:"summary"`
		} `json:"error"`
		Vulnerabilities map[string]struct {
			Name     string `json:"name"`
			Severity string `json:"severity"`
			Range    string `json:"range"`
			Via      []json.RawMessage `json:"via"`
		} `json:"vulnerabilities"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return nil, err
	}
	if doc.Error != nil {
		msg := doc.Error.Summary
		if msg == "" {
			msg = doc.Error.Code
		}
		if msg == "" {
			msg = "npm audit reported an error with no detail"
		}
		return nil, fmt.Errorf("npm audit error: %s", msg)
	}
	findings := make([]Finding, 0, len(doc.Vulnerabilities))
	for pkg, v := range doc.Vulnerabilities {
		name := v.Name
		if name == "" {
			name = pkg
		}
		findings = append(findings, Finding{
			Tool:     "npm-audit",
			RuleID:   "npm:" + name,
			Severity: ParseSeverity(v.Severity),
			File:     "package.json",
			Title:    "Vulnerable dependency: " + name + " " + v.Range,
			Detail:   "Known advisory in dependency " + name,
			Category: "Vulnerable and Outdated Components",
			Source:   "scanner",
		})
	}
	return findings, nil
}

func parseGovulncheck(out []byte) ([]Finding, error) {
	var findings []Finding
	// govulncheck runs in text mode, so — unlike the JSON scanners — a failure
	// produces no unmarshal error to route it to `failed`. Its fatal errors
	// print as "govulncheck: <msg>" (e.g. the vulnerability DB is unreachable
	// on an offline dispatch host). Without detecting that, a scan that never
	// ran returns zero findings and is counted as a clean `ran` scan, hiding
	// total coverage loss from the gate and require_scanners strict mode — the
	// same masquerade the JSON parsers guard against.
	var scanErr string
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "govulncheck:") && scanErr == "" {
			scanErr = strings.TrimSpace(strings.TrimPrefix(line, "govulncheck:"))
		}
		// Lines look like: "Vulnerability #1: GO-2024-1234"
		if !strings.HasPrefix(line, "Vulnerability #") {
			continue
		}
		idx := strings.LastIndex(line, ":")
		if idx < 0 {
			continue
		}
		id := strings.TrimSpace(line[idx+1:])
		if id == "" {
			continue
		}
		findings = append(findings, Finding{
			Tool:     "govulncheck",
			RuleID:   id,
			Severity: SeverityHigh,
			File:     "go.mod",
			Title:    "Called vulnerability " + id,
			Detail:   "Dependency CVE reachable from your code (https://pkg.go.dev/vuln/" + id + ")",
			Category: "Vulnerable and Outdated Components",
			Source:   "scanner",
		})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	// A fatal error with no vulnerability findings means the scan did not run
	// — surface it as a failure rather than a clean empty result.
	if len(findings) == 0 && scanErr != "" {
		return nil, fmt.Errorf("govulncheck error: %s", scanErr)
	}
	return findings, nil
}

// Run executes the scanner against repoDir and returns parsed findings. A
// non-zero exit is expected (most scanners exit non-zero when they find issues),
// so output is parsed regardless of exit code; a parse error is returned so the
// caller can log and continue (graceful degradation — one tool failing never
// aborts the scan).
func (s Scanner) Run(ctx context.Context, repoDir string) ([]Finding, error) {
	ctx, cancel := context.WithTimeout(ctx, scannerTimeout)
	defer cancel()

	var cmd *exec.Cmd
	switch s.Kind {
	case ScannerGosec:
		cmd = exec.CommandContext(ctx, "gosec", "-fmt=json", "-quiet", "./...")
	case ScannerGovulncheck:
		cmd = exec.CommandContext(ctx, "govulncheck", "./...")
	case ScannerGitleaks:
		cmd = exec.CommandContext(ctx, "gitleaks", "detect", "--no-banner", "--report-format", "json", "--report-path", "/dev/stdout")
	case ScannerSemgrep:
		cmd = exec.CommandContext(ctx, "semgrep", "scan", "--config", "auto", "--json", "--quiet")
	case ScannerNpmAudit:
		cmd = exec.CommandContext(ctx, "npm", "audit", "--json")
	default:
		return nil, nil
	}
	cmd.Dir = repoDir
	out, _ := cmd.CombinedOutput() // exit code intentionally ignored; parse output

	switch s.Kind {
	case ScannerGosec:
		return parseGosec(out, repoDir)
	case ScannerGovulncheck:
		return parseGovulncheck(out)
	case ScannerGitleaks:
		return parseGitleaks(out, repoDir)
	case ScannerSemgrep:
		return parseSemgrep(out, repoDir)
	case ScannerNpmAudit:
		return parseNpmAudit(out)
	default:
		return nil, nil
	}
}
