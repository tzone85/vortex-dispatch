package security

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestDetectLanguages(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module x\n")
	write("package.json", "{}")
	write("tsconfig.json", "{}")
	write("main.go", "package main")
	write("app.ts", "export {}")

	langs := DetectLanguages(dir)
	sort.Strings(langs)
	want := map[string]bool{"go": true, "typescript": true}
	for w := range want {
		found := false
		for _, l := range langs {
			if l == w {
				found = true
			}
		}
		if !found {
			t.Errorf("expected language %q detected, got %v", w, langs)
		}
	}
	// package.json + tsconfig.json ⇒ typescript, not bare javascript
	for _, l := range langs {
		if l == "javascript" {
			t.Errorf("tsconfig present should classify as typescript, not javascript: %v", langs)
		}
	}
}

func TestApplicableScanners(t *testing.T) {
	available := map[string]bool{"gosec": true, "govulncheck": true, "gitleaks": true, "semgrep": false, "npm": false}

	// Go repo: gosec + govulncheck + gitleaks (secrets, all langs). Not npm (absent + not node).
	got := applicableScanners([]string{"go"}, available)
	kinds := map[ScannerKind]bool{}
	for _, s := range got {
		kinds[s.Kind] = true
	}
	if !kinds[ScannerGosec] || !kinds[ScannerGovulncheck] || !kinds[ScannerGitleaks] {
		t.Errorf("go repo should run gosec+govulncheck+gitleaks, got %v", kinds)
	}
	if kinds[ScannerNpmAudit] {
		t.Error("npm-audit should not apply to a go-only repo")
	}
	if kinds[ScannerSemgrep] {
		t.Error("semgrep absent from PATH should be skipped")
	}

	// Python repo: gosec must NOT apply (Go-only tool); gitleaks still does.
	py := applicableScanners([]string{"python"}, available)
	for _, s := range py {
		if s.Kind == ScannerGosec || s.Kind == ScannerGovulncheck {
			t.Errorf("go-only tool %s should not apply to python", s.Kind)
		}
	}
}

func TestParseGosec(t *testing.T) {
	out := []byte(`{
	  "Issues": [
	    {"severity":"HIGH","confidence":"HIGH","cwe":{"id":"798"},"rule_id":"G101","details":"Potential hardcoded credentials","file":"/repo/auth.go","line":"42","code":"x"},
	    {"severity":"MEDIUM","confidence":"HIGH","cwe":{"id":"22"},"rule_id":"G304","details":"Potential file inclusion via variable","file":"/repo/io.go","line":"7","code":"y"}
	  ],
	  "Stats": {"files":3,"lines":120}
	}`)
	got, err := parseGosec(out, "/repo")
	if err != nil {
		t.Fatalf("parseGosec: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(got))
	}
	if got[0].Severity != SeverityHigh || got[0].RuleID != "G101" {
		t.Errorf("first finding wrong: %+v", got[0])
	}
	if got[0].File != "auth.go" { // path made repo-relative
		t.Errorf("expected repo-relative file, got %q", got[0].File)
	}
	if got[0].Line != 42 {
		t.Errorf("expected line 42, got %d", got[0].Line)
	}
	if got[0].Tool != "gosec" {
		t.Errorf("tool should be gosec, got %q", got[0].Tool)
	}
}

func TestParseGitleaks(t *testing.T) {
	out := []byte(`[
	  {"Description":"AWS Access Key","File":"config/prod.env","StartLine":3,"RuleID":"aws-access-token","Secret":"AKIAXXXXXXXX","Match":"AKIA..."},
	  {"Description":"Generic API Key","File":"src/client.ts","StartLine":12,"RuleID":"generic-api-key","Secret":"sk-...","Match":"key=sk-..."}
	]`)
	got, err := parseGitleaks(out, "/repo")
	if err != nil {
		t.Fatalf("parseGitleaks: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 secret findings, got %d", len(got))
	}
	// Secrets are always treated as critical.
	if got[0].Severity != SeverityCritical {
		t.Errorf("leaked secret should be critical, got %v", got[0].Severity)
	}
	if got[0].Line != 3 || got[0].File != "config/prod.env" {
		t.Errorf("wrong location: %+v", got[0])
	}
}

func TestParseSemgrep(t *testing.T) {
	out := []byte(`{
	  "results": [
	    {"check_id":"go.lang.security.audit.sqli","path":"/repo/db.go","start":{"line":55},"extra":{"message":"SQL injection","severity":"ERROR","metadata":{"cwe":["CWE-89: SQL Injection"],"owasp":["A03:2021"]}}}
	  ],
	  "errors": []
	}`)
	got, err := parseSemgrep(out, "/repo")
	if err != nil {
		t.Fatalf("parseSemgrep: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(got))
	}
	if got[0].Severity != SeverityHigh { // ERROR → high
		t.Errorf("ERROR should map to high, got %v", got[0].Severity)
	}
	if got[0].File != "db.go" || got[0].Line != 55 {
		t.Errorf("wrong location: %+v", got[0])
	}
}

// TestParseSemgrep_ScanErrorEnvelope pins that a scan-level failure (valid
// JSON, empty results, an error-level entry — e.g. rules failed to load on an
// offline host) is surfaced as a failure, not swallowed as a clean empty scan.
// Otherwise RunScanners counts it in `ran` (clean) instead of `failed`
// (coverage lost), and require_scanners strict mode would not block.
func TestParseSemgrep_ScanErrorEnvelope(t *testing.T) {
	out := []byte(`{"results":[],"errors":[{"level":"error","message":"unable to load config: registry unreachable"}]}`)
	got, err := parseSemgrep(out, "/repo")
	if err == nil {
		t.Fatalf("parseSemgrep = nil error, want scan failure surfaced; findings=%+v", got)
	}
	if !strings.Contains(err.Error(), "unable to load config") {
		t.Errorf("error %q does not name the scan error", err.Error())
	}

	// A scan that produced findings is NOT flipped to failed by a coexisting
	// non-fatal (warn-level) error entry — coverage clearly wasn't lost.
	okOut := []byte(`{"results":[{"check_id":"r","path":"/repo/x.go","start":{"line":1},"extra":{"message":"m","severity":"ERROR","metadata":{}}}],"errors":[{"level":"warn","message":"partial parse"}]}`)
	fs, err := parseSemgrep(okOut, "/repo")
	if err != nil {
		t.Fatalf("parseSemgrep with results + warn = %v, want nil", err)
	}
	if len(fs) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(fs))
	}
}

func TestParseNpmAudit(t *testing.T) {
	out := []byte(`{
	  "vulnerabilities": {
	    "lodash": {"name":"lodash","severity":"high","via":[{"title":"Prototype Pollution","url":"https://x","cwe":["CWE-1321"]}],"range":"<4.17.21"},
	    "minimist": {"name":"minimist","severity":"critical","via":[{"title":"Prototype Pollution"}],"range":"<1.2.6"}
	  },
	  "metadata": {"vulnerabilities":{"critical":1,"high":1,"moderate":0,"low":0,"total":2}}
	}`)
	got, err := parseNpmAudit(out)
	if err != nil {
		t.Fatalf("parseNpmAudit: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 dependency findings, got %d", len(got))
	}
	sevByPkg := map[string]Severity{}
	for _, f := range got {
		sevByPkg[f.Title] = f.Severity
	}
	// titles include the package name somewhere; just check severities present
	var hasCrit, hasHigh bool
	for _, f := range got {
		if f.Severity == SeverityCritical {
			hasCrit = true
		}
		if f.Severity == SeverityHigh {
			hasHigh = true
		}
	}
	if !hasCrit || !hasHigh {
		t.Errorf("expected one critical and one high dep finding, got %+v", got)
	}
}

// TestParseNpmAudit_ErrorEnvelope pins that npm's valid-JSON error envelope
// (emitted when the audit cannot run — e.g. no lockfile) is surfaced as an
// error rather than swallowed as a clean, empty scan. Otherwise RunScanners
// counts it in `ran` (clean coverage) instead of `failed` (coverage lost),
// letting a dependency-CVE scan silently no-op while the security gate — and
// require_scanners strict mode — believe the tool ran.
func TestParseNpmAudit_ErrorEnvelope(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want string
	}{
		{
			name: "ENOLOCK no lockfile",
			out:  `{"error":{"code":"ENOLOCK","summary":"This command requires an existing lockfile."}}`,
			want: "This command requires an existing lockfile",
		},
		{
			name: "error with only code",
			out:  `{"error":{"code":"EAUDITNOPJSON"}}`,
			want: "EAUDITNOPJSON",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseNpmAudit([]byte(tc.out))
			if err == nil {
				t.Fatalf("parseNpmAudit(%s) = nil error, want failure surfaced; findings=%+v", tc.out, got)
			}
			if got != nil {
				t.Errorf("expected no findings on error envelope, got %+v", got)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err.Error(), tc.want)
			}
		})
	}
}

func TestParseGovulncheck(t *testing.T) {
	// govulncheck text output (the human format): we extract called vulns.
	out := []byte(`=== Symbol Results ===

Vulnerability #1: GO-2024-1234
    A flaw in net/http allows request smuggling.
  More info: https://pkg.go.dev/vuln/GO-2024-1234
    Module: golang.org/x/net
      Found in: golang.org/x/net@v0.10.0
      Fixed in: golang.org/x/net@v0.17.0

Vulnerability #2: GO-2023-5678
    Another issue.
  More info: https://pkg.go.dev/vuln/GO-2023-5678
`)
	got, err := parseGovulncheck(out)
	if err != nil {
		t.Fatalf("parseGovulncheck: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 vuln findings, got %d", len(got))
	}
	if got[0].RuleID != "GO-2024-1234" {
		t.Errorf("expected GO-2024-1234, got %q", got[0].RuleID)
	}
	if got[0].Severity != SeverityHigh {
		t.Errorf("dependency CVE should be high, got %v", got[0].Severity)
	}
}

// TestParseGovulncheck_FatalErrorSurfaced pins that a govulncheck run that
// could not execute (e.g. the vulnerability DB is unreachable on an offline
// host) is surfaced as a failure, not swallowed as a clean, zero-finding scan.
// Text mode has no unmarshal error to route the failure to `failed`, so the
// "govulncheck: <msg>" fatal line is the signal.
func TestParseGovulncheck_FatalErrorSurfaced(t *testing.T) {
	out := []byte("govulncheck: loading vulnerability database: Get \"https://vuln.go.dev/index/db.json\": dial tcp: lookup vuln.go.dev: no such host\n")
	got, err := parseGovulncheck(out)
	if err == nil {
		t.Fatalf("parseGovulncheck = nil error, want fatal surfaced; findings=%+v", got)
	}
	if !strings.Contains(err.Error(), "vulnerability database") {
		t.Errorf("error %q does not name the fatal cause", err.Error())
	}

	// A clean run (no vulns, no fatal line) is NOT flipped to an error.
	clean := []byte("=== Symbol Results ===\n\nNo vulnerabilities found.\n")
	fs, err := parseGovulncheck(clean)
	if err != nil {
		t.Fatalf("parseGovulncheck(clean) = %v, want nil", err)
	}
	if len(fs) != 0 {
		t.Errorf("expected 0 findings on a clean scan, got %d", len(fs))
	}
}
