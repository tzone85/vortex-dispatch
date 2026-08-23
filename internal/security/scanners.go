package security

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os/exec"
	"path/filepath"
	"sort"
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
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return nil, err
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
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return nil, err
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

// parseGovulncheckJSON parses the `govulncheck -json` message stream (a sequence
// of concatenated JSON objects) and extracts CALLED vulnerabilities — those with
// a symbol-level trace frame (trace[0].function set). Unlike govulncheck's text
// mode, the JSON stream lets us positively tell "ran clean" from "failed to run":
// a successful run always emits a leading `config` handshake message. sawConfig
// reports whether that handshake was seen; a decode error on a non-EOF token
// (truncated/garbage output, e.g. a plain-text fatal error appended after a
// network failure) is returned so the caller can route the scan to `failed`
// instead of reporting a broken dependency scan as clean.
func parseGovulncheckJSON(out []byte) (findings []Finding, sawConfig bool, err error) {
	type traceFrame struct {
		Function string `json:"function"`
	}
	var msg struct {
		Config  json.RawMessage `json:"config"`
		Finding *struct {
			OSV   string       `json:"osv"`
			Trace []traceFrame `json:"trace"`
		} `json:"finding"`
	}
	called := map[string]bool{}
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		msg.Config = nil
		msg.Finding = nil
		if e := dec.Decode(&msg); e != nil {
			if errors.Is(e, io.EOF) {
				break
			}
			return nil, sawConfig, e
		}
		if len(msg.Config) > 0 {
			sawConfig = true
		}
		// govulncheck emits one finding per OSV per trace granularity
		// (module, package, symbol). The symbol-level finding — the only one
		// with a function in its top trace frame — is the "called" signal.
		if f := msg.Finding; f != nil && f.OSV != "" && len(f.Trace) > 0 && f.Trace[0].Function != "" {
			called[f.OSV] = true
		}
	}
	ids := make([]string, 0, len(called))
	for id := range called {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
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
	return findings, sawConfig, nil
}

// runGovulncheck runs govulncheck in JSON mode and, crucially, treats a failed
// run as a failure rather than a clean scan. The four JSON scanners (gosec,
// gitleaks, semgrep, npm-audit) get failure detection for free — a crash emits
// non-JSON that fails json.Unmarshal, routing them to `failed`. govulncheck's
// text mode had no such signal: a network/build/timeout error simply produced
// output with no "Vulnerability #" line, indistinguishable from a clean scan, so
// a dependency scan that never ran was reported as scanned-clean (defeating even
// `require_scanners: true`, which only inspects skipped/failed). In -json mode
// govulncheck exits non-zero ONLY on a tool error — vulnerabilities found do NOT
// set a non-zero code — and always emits a `config` handshake on a real run, so
// both a non-zero exit and a missing/garbled stream are surfaced as errors.
func runGovulncheck(ctx context.Context, repoDir string) ([]Finding, error) {
	ctx, cancel := context.WithTimeout(ctx, scannerTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "govulncheck", "-json", "./...")
	cmd.Dir = repoDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	findings, sawConfig, perr := parseGovulncheckJSON(stdout.Bytes())
	if perr != nil {
		return nil, fmt.Errorf("govulncheck: unparseable JSON output (scan did not complete): %w", perr)
	}
	if runErr != nil {
		return nil, fmt.Errorf("govulncheck: %w: %s", runErr, strings.TrimSpace(tailString(stderr.String(), 300)))
	}
	if !sawConfig {
		return nil, fmt.Errorf("govulncheck: no config message on stdout (scan did not run)")
	}
	return findings, nil
}

// tailString returns the last n bytes of s (used to bound scanner stderr in
// error messages so a large trace can't flood logs).
func tailString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// Run executes the scanner against repoDir and returns parsed findings. A
// non-zero exit is expected (most scanners exit non-zero when they find issues),
// so output is parsed regardless of exit code; a parse error is returned so the
// caller can log and continue (graceful degradation — one tool failing never
// aborts the scan).
func (s Scanner) Run(ctx context.Context, repoDir string) ([]Finding, error) {
	// govulncheck runs in JSON mode with its own exit-code handling so a failed
	// run is reported as failed, never as a clean scan (see runGovulncheck).
	if s.Kind == ScannerGovulncheck {
		return runGovulncheck(ctx, repoDir)
	}

	ctx, cancel := context.WithTimeout(ctx, scannerTimeout)
	defer cancel()

	var cmd *exec.Cmd
	switch s.Kind {
	case ScannerGosec:
		cmd = exec.CommandContext(ctx, "gosec", "-fmt=json", "-quiet", "./...")
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
