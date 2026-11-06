package improve_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/improve"
)

// fakeRunner is a scripted improve.CommandRunner. Keys are "name arg arg...".
type fakeRunner struct {
	out    map[string]string
	code   map[string]int
	err    map[string]error
	calls  []string
	fallbk string
	mutate func(key string)
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{out: map[string]string{}, code: map[string]int{}, err: map[string]error{}}
}

func (f *fakeRunner) key(name string, args ...string) string {
	return strings.TrimSpace(name + " " + strings.Join(args, " "))
}

func (f *fakeRunner) Run(_ context.Context, _ string, name string, args ...string) (string, int, error) {
	k := f.key(name, args...)
	f.calls = append(f.calls, k)
	if f.mutate != nil {
		f.mutate(k)
	}
	if err, ok := f.err[k]; ok {
		return f.out[k], f.code[k], err
	}
	if out, ok := f.out[k]; ok {
		return out, f.code[k], nil
	}
	return f.fallbk, 0, nil
}

func (f *fakeRunner) called(sub string) bool {
	for _, c := range f.calls {
		if strings.Contains(c, sub) {
			return true
		}
	}
	return false
}

// --- parser tests: real tool output shapes -------------------------------

func TestParseBuildOutput(t *testing.T) {
	out := "# github.com/tzone85/vortex-dispatch/internal/web\n" +
		"internal/web/server.go:65:12: undefined: notAThing\n" +
		"internal/web/ws.go:47:31: cannot use x (variable of type int) as string value\n"

	got := improve.ParseBuildOutput(out)
	if len(got) != 2 {
		t.Fatalf("want 2 findings, got %d: %+v", len(got), got)
	}
	if got[0].Check != improve.CheckBuild {
		t.Errorf("check = %q, want %q", got[0].Check, improve.CheckBuild)
	}
	if got[0].Target != "internal/web/server.go:65:12" {
		t.Errorf("target = %q", got[0].Target)
	}
	if !strings.Contains(got[0].Title, "undefined: notAThing") {
		t.Errorf("title = %q", got[0].Title)
	}
	if got[0].Severity != improve.SeverityCritical {
		t.Errorf("severity = %q, want critical", got[0].Severity)
	}
	if got[0].Deterministic {
		t.Error("build failures must never be marked deterministic/auto-fixable")
	}
}

func TestParseBuildOutputClean(t *testing.T) {
	if got := improve.ParseBuildOutput(""); len(got) != 0 {
		t.Fatalf("clean build must yield no findings, got %+v", got)
	}
	if got := improve.ParseBuildOutput("\n  \n"); len(got) != 0 {
		t.Fatalf("whitespace-only build output must yield no findings, got %+v", got)
	}
}

func TestParseVetOutput(t *testing.T) {
	out := "# github.com/tzone85/vortex-dispatch/internal/improve\n" +
		"vet: internal/improve/email.go:312:16: fmt.Sprintf format %d has arg s of wrong type string\n"

	got := improve.ParseVetOutput(out)
	if len(got) != 1 {
		t.Fatalf("want 1 finding, got %d: %+v", len(got), got)
	}
	if got[0].Check != improve.CheckVet {
		t.Errorf("check = %q", got[0].Check)
	}
	if got[0].Target != "internal/improve/email.go:312:16" {
		t.Errorf("target = %q (the 'vet: ' prefix must be stripped)", got[0].Target)
	}
	if got[0].Deterministic {
		t.Error("vet findings require judgement")
	}
}

func TestParseTestOutput(t *testing.T) {
	out := strings.Join([]string{
		"ok  \tgithub.com/tzone85/vortex-dispatch/internal/improve\t0.412s",
		"--- FAIL: TestSomethingImportant (0.00s)",
		"    thing_test.go:42: want 3, got 4",
		"FAIL\tgithub.com/tzone85/vortex-dispatch/internal/runtime\t1.204s",
		"ok  \tgithub.com/tzone85/vortex-dispatch/internal/web\t0.100s",
		"FAIL",
	}, "\n")

	got := improve.ParseTestOutput(out)
	if len(got) != 2 {
		t.Fatalf("want 2 findings (1 failed test + 1 failed package), got %d: %+v", len(got), got)
	}

	var sawTest, sawPkg bool
	for _, f := range got {
		if f.Check != improve.CheckTest {
			t.Errorf("check = %q", f.Check)
		}
		if f.Deterministic {
			t.Error("test failures require judgement, never auto-fix")
		}
		if f.Target == "TestSomethingImportant" {
			sawTest = true
		}
		if f.Target == "github.com/tzone85/vortex-dispatch/internal/runtime" {
			sawPkg = true
		}
	}
	if !sawTest {
		t.Error("missing finding for failing test name")
	}
	if !sawPkg {
		t.Error("missing finding for failing package")
	}
}

func TestParseTestOutputAllPass(t *testing.T) {
	out := "ok  \tgithub.com/tzone85/vortex-dispatch/internal/improve\t0.412s\n" +
		"ok  \tgithub.com/tzone85/vortex-dispatch/internal/web\t0.100s\n"
	if got := improve.ParseTestOutput(out); len(got) != 0 {
		t.Fatalf("all-pass output must yield no findings, got %+v", got)
	}
}

func TestParseTestOutputIgnoresNoTestFiles(t *testing.T) {
	out := "?   \tgithub.com/tzone85/vortex-dispatch/cmd/vxd\t[no test files]\n"
	if got := improve.ParseTestOutput(out); len(got) != 0 {
		t.Fatalf("[no test files] is not a defect, got %+v", got)
	}
}

func TestParseGofmtOutput(t *testing.T) {
	out := "internal/improve/config.go\ninternal/autoresearch/metric.go\n\n"
	got := improve.ParseGofmtOutput(out)
	if len(got) != 2 {
		t.Fatalf("want 2 findings, got %d: %+v", len(got), got)
	}
	for _, f := range got {
		if f.Check != improve.CheckGofmt {
			t.Errorf("check = %q", f.Check)
		}
		if !f.Deterministic {
			t.Error("gofmt drift must be deterministic (auto-fixable)")
		}
		if f.Severity != improve.SeverityLow {
			t.Errorf("severity = %q, want low", f.Severity)
		}
	}
	if got[0].Target != "internal/improve/config.go" {
		t.Errorf("target = %q", got[0].Target)
	}
}

func TestParseModTidyDrift(t *testing.T) {
	diff := "--- go.mod.orig\n+++ go.mod\n@@\n-\tgolang.org/x/text v0.39.0\n+\tgolang.org/x/text v0.41.0 // indirect\n"
	got := improve.ParseModTidyDrift(diff)
	if len(got) != 1 {
		t.Fatalf("want 1 finding, got %d: %+v", len(got), got)
	}
	if !got[0].Deterministic {
		t.Error("go mod tidy drift must be deterministic (auto-fixable)")
	}
	if got[0].Check != improve.CheckModTidy {
		t.Errorf("check = %q", got[0].Check)
	}
	if improve.ParseModTidyDrift("") != nil && len(improve.ParseModTidyDrift("")) != 0 {
		t.Error("no drift must yield no findings")
	}
}

func TestParseGovulncheckOutput(t *testing.T) {
	out := strings.Join([]string{
		"=== Symbol Results ===",
		"",
		"Vulnerability #1: GO-2026-6617",
		"    HTTP/2 server crash due to HPACK encoder race in net/http",
		"  More info: https://pkg.go.dev/vuln/GO-2026-6617",
		"  Standard library",
		"    Found in: net/http@go1.26.2",
		"    Fixed in: net/http@go1.26.9",
		"    Example traces found:",
		"      #1: internal/web/ws.go:47:31: web.Hub.HandleWebSocket calls websocket.Accept",
		"",
		"=== Package Results ===",
		"",
		"Vulnerability #1: GO-2026-6629",
		"    Panic when parsing crafted input in golang.org/x/text",
		"    Found in: golang.org/x/text/secure/precis@v0.39.0",
		"    Fixed in: golang.org/x/text/secure/precis@v0.41.0",
		"",
		"Your code is affected by 1 vulnerability from the Go standard library.",
	}, "\n")

	got := improve.ParseGovulncheckOutput(out)
	if len(got) != 2 {
		t.Fatalf("want 2 findings, got %d: %+v", len(got), got)
	}

	sym := got[0]
	if sym.Target != "GO-2026-6617" {
		t.Errorf("target = %q", sym.Target)
	}
	if sym.Severity != improve.SeverityCritical {
		t.Errorf("called (symbol-level) vuln severity = %q, want critical", sym.Severity)
	}
	if !strings.Contains(sym.Detail, "net/http@go1.26.9") {
		t.Errorf("detail must carry the fixed version, got %q", sym.Detail)
	}
	if sym.Deterministic {
		t.Error("vulnerabilities must never be auto-fixed")
	}

	pkg := got[1]
	if pkg.Target != "GO-2026-6629" {
		t.Errorf("target = %q", pkg.Target)
	}
	if pkg.Severity != improve.SeverityMedium {
		t.Errorf("imported-but-not-called vuln severity = %q, want medium", pkg.Severity)
	}
}

func TestParseGovulncheckNoVulns(t *testing.T) {
	out := "No vulnerabilities found.\n"
	if got := improve.ParseGovulncheckOutput(out); len(got) != 0 {
		t.Fatalf("clean scan must yield no findings, got %+v", got)
	}
}

// --- the deterministic / judgement split --------------------------------

func TestDeterministicSplitIsExplicit(t *testing.T) {
	auto := []improve.CheckName{improve.CheckGofmt, improve.CheckModTidy}
	manual := []improve.CheckName{improve.CheckBuild, improve.CheckVet, improve.CheckTest, improve.CheckVuln}

	for _, c := range auto {
		if !improve.IsDeterministic(c) {
			t.Errorf("%s must be deterministic", c)
		}
	}
	for _, c := range manual {
		if improve.IsDeterministic(c) {
			t.Errorf("%s must require human judgement", c)
		}
	}
	if improve.IsDeterministic(improve.CheckName("something_new")) {
		t.Error("unknown checks must default to judgement-required, not auto-fix")
	}
}

// --- auditor orchestration ----------------------------------------------

func TestSelfAuditorRunsAllChecksAndReportsClean(t *testing.T) {
	r := newFakeRunner()
	r.out["go build ./..."] = ""
	r.out["go vet ./..."] = ""
	r.out["gofmt -l ."] = ""
	r.out["go test -count=1 -timeout 15m ./..."] = "ok  \tx/y\t0.1s\n"
	r.out["govulncheck ./..."] = "No vulnerabilities found.\n"

	a := improve.NewSelfAuditor(t.TempDir(), r)
	rep, err := a.Audit(context.Background())
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if !rep.Clean() {
		t.Fatalf("expected clean report, got findings: %+v", rep.Findings)
	}
	if len(rep.Results) != 6 {
		t.Errorf("want 6 check results, got %d", len(rep.Results))
	}
	for _, want := range []string{"go build", "go vet", "gofmt", "go test", "govulncheck", "go mod tidy"} {
		if !r.called(want) {
			t.Errorf("auditor never ran %q (calls: %v)", want, r.calls)
		}
	}
}

func TestSelfAuditorCollectsFindingsFromEveryCheck(t *testing.T) {
	r := newFakeRunner()
	r.out["go build ./..."] = ""
	r.out["go vet ./..."] = "vet: internal/improve/email.go:312:16: bad format\n"
	r.code["go vet ./..."] = 1
	r.out["gofmt -l ."] = "internal/improve/config.go\n"
	r.out["go test -count=1 -timeout 15m ./..."] = "FAIL\tx/y\t1.0s\n"
	r.code["go test -count=1 -timeout 15m ./..."] = 1
	r.out["govulncheck ./..."] = "=== Symbol Results ===\n\nVulnerability #1: GO-2026-6617\n    boom\n    Found in: net/http@go1.26.2\n    Fixed in: net/http@go1.26.9\n"
	r.code["govulncheck ./..."] = 3

	a := improve.NewSelfAuditor(t.TempDir(), r)
	rep, err := a.Audit(context.Background())
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if rep.Clean() {
		t.Fatal("report must not be clean")
	}

	seen := map[improve.CheckName]bool{}
	for _, f := range rep.Findings {
		seen[f.Check] = true
	}
	for _, c := range []improve.CheckName{improve.CheckVet, improve.CheckGofmt, improve.CheckTest, improve.CheckVuln} {
		if !seen[c] {
			t.Errorf("no finding recorded for %s", c)
		}
	}

	det := rep.DeterministicFindings()
	if len(det) != 1 || det[0].Check != improve.CheckGofmt {
		t.Errorf("DeterministicFindings = %+v, want exactly the gofmt one", det)
	}
	if len(rep.JudgementFindings())+len(det) != len(rep.Findings) {
		t.Error("every finding must be classified as deterministic or judgement, with no overlap")
	}
}

func TestSelfAuditorRecordsToolErrorsWithoutAborting(t *testing.T) {
	r := newFakeRunner()
	r.err["govulncheck ./..."] = errors.New("executable file not found in $PATH")
	r.code["govulncheck ./..."] = -1
	r.out["go build ./..."] = ""
	r.out["go vet ./..."] = ""
	r.out["gofmt -l ."] = ""
	r.out["go test -count=1 -timeout 15m ./..."] = "ok  \tx/y\t0.1s\n"

	a := improve.NewSelfAuditor(t.TempDir(), r)
	rep, err := a.Audit(context.Background())
	if err != nil {
		t.Fatalf("a missing tool must not fail the whole audit: %v", err)
	}
	var vuln *improve.CheckResult
	for i := range rep.Results {
		if rep.Results[i].Check == improve.CheckVuln {
			vuln = &rep.Results[i]
		}
	}
	if vuln == nil {
		t.Fatal("no result recorded for govulncheck")
	}
	if vuln.Err == "" {
		t.Error("the tool error must be recorded on the result")
	}
	if vuln.Skipped != true {
		t.Error("an unavailable tool must be marked Skipped, not silently clean")
	}
}

// --- mod tidy drift is detected without touching the real go.mod --------

func TestSelfAuditorModTidyIsNonDestructive(t *testing.T) {
	dir := t.TempDir()
	gomod := filepath.Join(dir, "go.mod")
	original := "module example.com/x\n\ngo 1.26\n"
	if err := os.WriteFile(gomod, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	r := newFakeRunner()
	r.out["go mod tidy -diff"] = "--- go.mod\n+++ go.mod.tidy\n@@\n+require example.com/y v1.0.0\n"
	r.code["go mod tidy -diff"] = 1
	r.out["go build ./..."] = ""
	r.out["go vet ./..."] = ""
	r.out["gofmt -l ."] = ""
	r.out["go test -count=1 -timeout 15m ./..."] = "ok  \tx/y\t0.1s\n"
	r.out["govulncheck ./..."] = "No vulnerabilities found.\n"

	rep, err := improve.NewSelfAuditor(dir, r).Audit(context.Background())
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}

	got, err := os.ReadFile(gomod)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Errorf("go.mod must be untouched, got %q", got)
	}
	var n int
	for _, res := range rep.Results {
		if res.Check == improve.CheckModTidy {
			if !strings.HasSuffix(res.Command, "-diff") {
				t.Errorf("tidy probe must use -diff, ran %q", res.Command)
			}
		}
	}
	for _, f := range rep.Findings {
		if f.Check == improve.CheckModTidy {
			n++
		}
	}
	if n != 1 {
		t.Errorf("want exactly 1 tidy finding, got %d", n)
	}
	if r.called("go mod tidy\n") || len(r.calls) == 0 {
		t.Error("unexpected calls")
	}
}

// --- bridges into the existing audit/analyzer abstractions --------------

func TestReportBridgesToScoredFindings(t *testing.T) {
	r := newFakeRunner()
	r.out["gofmt -l ."] = "a.go\n"
	r.out["go build ./..."] = "x.go:1:1: undefined: q\n"
	r.code["go build ./..."] = 2
	r.out["go vet ./..."] = ""
	r.out["go test -count=1 -timeout 15m ./..."] = "ok  \tx/y\t0.1s\n"
	r.out["govulncheck ./..."] = "No vulnerabilities found.\n"

	rep, err := improve.NewSelfAuditor(t.TempDir(), r).Audit(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	scored := rep.ScoredFindings()
	if len(scored) != len(rep.Findings) {
		t.Fatalf("scored %d, findings %d", len(scored), len(rep.Findings))
	}
	ranked := improve.RankFindings(scored)
	if len(ranked) == 0 {
		t.Fatal("RankFindings dropped everything")
	}
	// the critical build break must outrank the cosmetic gofmt drift
	if ranked[0].Category != string(improve.CheckBuild) {
		t.Errorf("top ranked = %q, want the build failure first", ranked[0].Category)
	}
	for _, s := range scored {
		if s.Relevance < 1 || s.Relevance > 10 {
			t.Errorf("relevance %d out of the 1-10 range the analyzer expects", s.Relevance)
		}
		if s.SourceURL == "" {
			t.Error("SourceURL should identify the local tool that produced the finding")
		}
	}
}

func TestReportWritesToExistingAuditLog(t *testing.T) {
	dir := t.TempDir()
	r := newFakeRunner()
	r.out["gofmt -l ."] = "a.go\n"
	r.out["go build ./..."] = ""
	r.out["go vet ./..."] = ""
	r.out["go test -count=1 -timeout 15m ./..."] = "ok  \tx/y\t0.1s\n"
	r.out["govulncheck ./..."] = "No vulnerabilities found.\n"

	rep, err := improve.NewSelfAuditor(t.TempDir(), r).Audit(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	logDir := filepath.Join(dir, "audit")
	al := improve.NewAuditLog(logDir)
	entries := rep.AuditEntries("selfaudit-test-run")
	if len(entries) != len(rep.Findings) {
		t.Fatalf("entries %d != findings %d", len(entries), len(rep.Findings))
	}
	for _, e := range entries {
		if e.RunID != "selfaudit-test-run" {
			t.Errorf("run id = %q", e.RunID)
		}
		if e.Source != "self-audit" {
			t.Errorf("source = %q, want self-audit", e.Source)
		}
		if e.Disposition == "" {
			t.Error("disposition must say whether the finding is auto-fixable or deferred")
		}
		if err := al.Append(e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	back, err := al.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(back) != len(entries) {
		t.Fatalf("read back %d entries, wrote %d", len(back), len(entries))
	}
}

func TestReportMarkdown(t *testing.T) {
	r := newFakeRunner()
	r.out["gofmt -l ."] = "a.go\n"
	r.out["go build ./..."] = ""
	r.out["go vet ./..."] = ""
	r.out["go test -count=1 -timeout 15m ./..."] = "ok  \tx/y\t0.1s\n"
	r.out["govulncheck ./..."] = "No vulnerabilities found.\n"

	rep, err := improve.NewSelfAuditor(t.TempDir(), r).Audit(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	md := rep.Markdown()
	for _, want := range []string{"Self-Audit", "gofmt", "auto-fixable", "human judgement"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q:\n%s", want, md)
		}
	}
}

// --- phase 3 hardening ----------------------------------------------------

func TestParseGovulncheckIgnoresTrailer(t *testing.T) {
	out := "=== Package Results ===\n\nVulnerability #1: GO-2026-1\n    boom\n    Fixed in: x@v2\n\n" +
		"Your code is affected by 0 vulnerabilities.\nThis scan also found 1 vulnerability.\nUse '-show verbose' for more details.\n"
	got := improve.ParseGovulncheckOutput(out)
	if len(got) != 1 {
		t.Fatalf("want 1 finding, got %d", len(got))
	}
	if strings.Contains(got[0].Detail, "Your code is affected") || strings.Contains(got[0].Detail, "This scan") {
		t.Errorf("trailer leaked into detail: %q", got[0].Detail)
	}
}

func TestAuditDedupesVetAgainstBuild(t *testing.T) {
	r := newFakeRunner()
	r.out["go build ./..."] = "x.go:1:1: undefined: q\n"
	r.code["go build ./..."] = 2
	r.out["go vet ./..."] = "x.go:1:1: undefined: q\ny.go:2:2: bad\n"
	r.code["go vet ./..."] = 1
	r.out["go test -count=1 -timeout 15m ./..."] = "ok  \tx/y\t0.1s\n"
	rep, err := improve.NewSelfAuditor(t.TempDir(), r).Audit(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, f := range rep.Findings {
		if f.Target == "x.go:1:1" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("x.go:1:1 reported %d times, want 1", n)
	}
	var vetY bool
	for _, f := range rep.Findings {
		if f.Check == improve.CheckVet && f.Target == "y.go:2:2" {
			vetY = true
		}
	}
	if !vetY {
		t.Error("vet-only finding must be kept")
	}
}

func TestAuditMarksCancelledCheckSkipped(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := newFakeRunner()
	r.out["go test -count=1 -timeout 15m ./..."] = "partial"
	r.mutate = func(k string) {
		if strings.HasPrefix(k, "go test") {
			cancel()
		}
	}
	rep, err := improve.NewSelfAuditor(t.TempDir(), r).Audit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Skipped()) == 0 {
		t.Fatal("a cancelled check must be reported as skipped")
	}
	var found bool
	for _, s := range rep.Skipped() {
		if s.Check == improve.CheckTest && strings.Contains(s.Err, "cancelled") {
			found = true
		}
	}
	if !found {
		t.Errorf("go test not recorded as cancelled: %+v", rep.Skipped())
	}
}

func TestAuditSkipTests(t *testing.T) {
	r := newFakeRunner()
	a := improve.NewSelfAuditor(t.TempDir(), r)
	a.SkipTests = true
	rep, err := a.Audit(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.called("go test") {
		t.Error("go test must not run when SkipTests is set")
	}
	var skipped int
	for _, s := range rep.Skipped() {
		if s.Check == improve.CheckTest {
			skipped++
		}
	}
	if skipped != 1 {
		t.Errorf("want go test recorded as skipped once, got %d", skipped)
	}
}

func TestMergeHealResult(t *testing.T) {
	findings := []improve.SelfFinding{
		{Check: improve.CheckGofmt, Deterministic: true},
		{Check: improve.CheckBuild},
	}
	base := func() []improve.AuditEntry {
		return []improve.AuditEntry{{Disposition: "proposed"}, {Disposition: "proposed"}}
	}

	got := improve.MergeHealResult(base(), findings, improve.ImplementResult{Disposition: "implemented", PRURL: "u"}, true)
	if got[0].Disposition != "implemented" || got[0].PRURL != "u" || !got[0].TestsPassed {
		t.Errorf("deterministic entry not marked implemented: %+v", got[0])
	}
	if got[1].Disposition != "proposed" {
		t.Errorf("judgement entry must not change: %+v", got[1])
	}

	got = improve.MergeHealResult(base(), findings, improve.ImplementResult{Disposition: "aborted", Error: "gate x"}, true)
	if got[0].Disposition != "aborted" || got[0].Error != "gate x" {
		t.Errorf("deterministic entry not marked aborted: %+v", got[0])
	}

	got = improve.MergeHealResult(base(), findings, improve.ImplementResult{Disposition: "implemented"}, false)
	if got[0].Disposition != "proposed" {
		t.Errorf("attempted=false must change nothing: %+v", got[0])
	}
}

// --- behaviour verified against the real toolchain -------------------------

func TestAuditBrokenBuildSkipsTests(t *testing.T) {
	r := newFakeRunner()
	r.out["go build ./..."] = "x.go:1:1: undefined: q\n"
	r.code["go build ./..."] = 1
	rep, err := improve.NewSelfAuditor(t.TempDir(), r).Audit(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.called("go test") {
		t.Error("go test must not run on a broken build")
	}
	var found bool
	for _, s := range rep.Skipped() {
		if s.Check == improve.CheckTest && strings.Contains(s.Err, "build failed") {
			found = true
		}
	}
	if !found {
		t.Errorf("go test must be recorded as skipped because the build failed: %+v", rep.Skipped())
	}
	if len(rep.Findings) != 1 || rep.Findings[0].Check != improve.CheckBuild {
		t.Errorf("findings = %+v", rep.Findings)
	}
}

func TestAuditNonZeroExitWithoutFindingsIsNotClean(t *testing.T) {
	r := newFakeRunner()
	// govulncheck exits 1 (not 3) when it cannot load packages; no vulns parsed.
	r.out["govulncheck ./..."] = "govulncheck: loading packages:\nThere are errors with the provided package patterns:\n"
	r.code["govulncheck ./..."] = 1
	// go mod tidy -diff exits 1 on a network error with no diff.
	r.out["go mod tidy -diff"] = "go: example.com/t imports\n\texample.invalid/x: module lookup disabled by GOPROXY=off\n"
	r.code["go mod tidy -diff"] = 1
	rep, err := improve.NewSelfAuditor(t.TempDir(), r).Audit(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Clean() {
		t.Errorf("tool errors must not become findings: %+v", rep.Findings)
	}
	skipped := map[improve.CheckName]string{}
	for _, s := range rep.Skipped() {
		skipped[s.Check] = s.Err
	}
	for _, c := range []improve.CheckName{improve.CheckVuln, improve.CheckModTidy} {
		if msg, ok := skipped[c]; !ok {
			t.Errorf("%s exited non-zero without findings and must be marked skipped", c)
		} else if !strings.Contains(msg, "exit 1") {
			t.Errorf("%s skip reason should carry the exit code: %q", c, msg)
		}
	}
}

func TestParseTestOutputStripsBuildFailedSuffix(t *testing.T) {
	out := "FAIL\texample.com/rt/pkg/broken [build failed]\nFAIL\texample.com/rt/pkg/ok [setup failed]\nFAIL\n"
	got := improve.ParseTestOutput(out)
	if len(got) != 2 {
		t.Fatalf("want 2, got %d: %+v", len(got), got)
	}
	if got[0].Target != "example.com/rt/pkg/broken" || got[1].Target != "example.com/rt/pkg/ok" {
		t.Errorf("targets = %q, %q", got[0].Target, got[1].Target)
	}
}

func TestParseModTidyDriftRequiresDiff(t *testing.T) {
	if got := improve.ParseModTidyDrift("go: module lookup disabled by GOPROXY=off\n"); len(got) != 0 {
		t.Errorf("an error message is not drift: %+v", got)
	}
	real := "diff current/go.mod tidy/go.mod\n--- current/go.mod\n+++ tidy/go.mod\n@@ -1,5 +1,3 @@\n-require x v1\n"
	if got := improve.ParseModTidyDrift(real); len(got) != 1 {
		t.Errorf("a unified diff is drift: %+v", got)
	}
}
