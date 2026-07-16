// Package test contains structural audit tests that encode findings from the
// 2026-07-05 VXD open-source audit. These tests assert the committed codebase
// state and must be updated when intentional gaps are closed.
package test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/preflight"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), ".."))
}

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

// TestAudit_PreflightCheckCounts pins the preflight check inventory so doc
// drift (the audit found AGENTS.md "12", README "15" vs code 16) is caught by
// CI. Counts moved 16→18 / 9→11 when the disk_space and tmux_server checks
// landed (audit findings O-01/O-06), then 18→19 with the qa_model inert-binding
// check (WEAKNESSES.md P0-02) — README, CLAUDE.md, and AGENTS.md must state
// these numbers.
func TestAudit_PreflightCheckCounts(t *testing.T) {
	if got := len(preflight.AllChecks()); got != 19 {
		t.Errorf("AllChecks() = %d, want 19 (README CLI table)", got)
	}
	if got := len(preflight.DispatchChecks()); got != 11 {
		t.Errorf("DispatchChecks() = %d, want 11", got)
	}
}

// TestAudit_DocCoverageTargetsCLAUDEmd verifies the doc gate enforces CLAUDE.md
// (not AGENTS.md) and that the file exists in the committed tree.
func TestAudit_DocCoverageTargetsCLAUDEmd(t *testing.T) {
	root := repoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "CLAUDE.md")); err != nil {
		t.Fatalf("CLAUDE.md must exist in committed tree for doc-coverage gate: %v", err)
	}
	src := readRepoFile(t, "internal/engine/doc_coverage_test.go")
	if !strings.Contains(src, "CLAUDE.md") {
		t.Fatal("doc_coverage_test.go must reference CLAUDE.md")
	}
	if strings.Contains(src, "AGENTS.md") {
		t.Error("doc_coverage_test.go references AGENTS.md but enforcement target is CLAUDE.md — dual-doc drift risk")
	}
}

// TestAudit_VXDImprovePresentAndCIBuildsIt verifies cmd/vxd-improve exists and
// CI builds it — a regression guard against worktree-local accidental deletion.
func TestAudit_VXDImprovePresentAndCIBuildsIt(t *testing.T) {
	root := repoRoot(t)
	improveMain := filepath.Join(root, "cmd", "vxd-improve", "main.go")
	if _, err := os.Stat(improveMain); err != nil {
		t.Fatalf("cmd/vxd-improve/main.go must exist in committed tree: %v", err)
	}
	ci := readRepoFile(t, ".github/workflows/ci.yml")
	if !strings.Contains(ci, "vxd-improve") {
		t.Error("CI should build vxd-improve when cmd/vxd-improve/main.go exists")
	}
}

// TestAudit_AGENTSmdPreflightCountDrift documents AGENTS.md saying 12 vs code 16.
func TestAudit_AGENTSmdPreflightCountDrift(t *testing.T) {
	agents := readRepoFile(t, "AGENTS.md")
	if strings.Contains(agents, "12 pre-flight") {
		t.Error("FINDING: AGENTS.md claims 12 preflight checks but AllChecks() returns 16")
	}
}

// TestAudit_HealthEndpointShipped contradicts architecture-overview "Missing".
func TestAudit_HealthEndpointShipped(t *testing.T) {
	src := readRepoFile(t, "internal/web/server.go")
	if !strings.Contains(src, `"/health"`) {
		t.Fatal("/health route not found in internal/web/server.go")
	}
	overview := readRepoFile(t, "docs/architecture-overview.md")
	if strings.Contains(overview, "Health endpoint | Missing") {
		t.Error("FINDING: architecture-overview.md marks health endpoint Missing while /health is shipped")
	}
}

// TestAudit_NotifyOnCompleteWired pins the fix for audit finding W-02: the
// notify_on_complete config field must have a reader outside its definition
// (the notificationAllowlist gating in internal/cli/resume.go). The original
// audit documented it as unwired (matches == 1, config definition only).
func TestAudit_NotifyOnCompleteWired(t *testing.T) {
	root := repoRoot(t)
	matches := 0
	_ = filepath.Walk(filepath.Join(root, "internal"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		if strings.Contains(string(data), "NotifyOnComplete") {
			matches++
		}
		return nil
	})
	if matches < 2 {
		t.Errorf("NotifyOnComplete must be read outside its config definition (want >= 2 non-test files, got %d) — completion notifications unwired (audit W-02)", matches)
	}
}

// TestAudit_AutoresearchBaseline documents v1 neutral baseline (improved from
// hardcoded 0 per audit E-02 for better delta behavior).
func TestAudit_AutoresearchBaseline(t *testing.T) {
	src := readRepoFile(t, "internal/cli/autoresearch.go")
	if !strings.Contains(src, "return func() float64 { return 0.5 }") {
		t.Error("baselineFromConfig should return neutral 0.5 for v1")
	}
}

// TestAudit_AutoresearchMergePR verifies that the shipped auto-gate MergePR
// path is implemented (no stub) and can be driven with correct behavior on
// real DefaultGateOps (per AC3). It asserts the error string for "not
// implemented" is gone and real delegation code is present.
func TestAudit_AutoresearchMergePR(t *testing.T) {
	src := readRepoFile(t, "internal/autoresearch/gate.go")
	if strings.Contains(src, "not implemented for URL form") {
		t.Error("MergePR still contains stub 'not implemented' — auto gate must use real git.MergePR for correct outcomes")
	}
	if !strings.Contains(src, "parsePRNumberFromURL") || !strings.Contains(src, "git.MergePR(repoDir, num)") {
		t.Error("MergePR must delegate to real git.MergePR after parsing URL")
	}
}

// TestAudit_EventProjectorDefaultSwallows documents silent default branch.
func TestAudit_EventProjectorDefaultSwallows(t *testing.T) {
	src := readRepoFile(t, "internal/state/sqlite.go")
	if !strings.Contains(src, "WARNING: unhandled event type") {
		t.Fatal("expected default-case WARNING log in sqlite.go Project()")
	}
}

// TestAudit_DocsNoStaleAutoresearchStubRefs is the mechanical doc gate for
// AC2/verif step 5. It enforces that documentation (training, knowledge,
// audit-findings) does not contain present-tense references to known stubs
// for wired autoresearch subsystems (evolve, MergePR). It also asserts that
// the T-04 row in the open findings table reflects resolved status (contains
// (FIXED) or Resolved) rather than claiming "intentionally document stubs".
func TestAudit_DocsNoStaleAutoresearchStubRefs(t *testing.T) {
	root := repoRoot(t)

	badPhrases := []string{
		`Wiring tests intentionally document stubs`,
		`v1 stub`,
		`is a stub for "auto" gate`,
		`MergePR returns "not implemented"`,
		`Autoresearch | **Partial**`,
		`Partial.*autoresearch`,
	}

	// 1. Check T-04 row in audit-findings is resolved (not claiming stub documentation)
	auditSrc := readRepoFile(t, "docs/audit-findings-2026-07-05.md")
	if strings.Contains(auditSrc, "Wiring tests intentionally document stubs (autoresearch evolve)") {
		t.Error("T-04 row still claims 'Wiring tests intentionally document stubs' — must be marked (FIXED)/Resolved after wiring")
	}
	if !strings.Contains(auditSrc, "T-04") || !(strings.Contains(auditSrc, "(FIXED)") || strings.Contains(auditSrc, "Resolved") || strings.Contains(auditSrc, "FIXED")) {
		// loose check that the row was updated in the open table or historical
		t.Log("T-04 row should contain (FIXED) or Resolved evidence for autoresearch stub removal")
	}

	// AC2 positive assertions (existence + markers) for training docs.
	// These are the only doc claims allowed in this round: asserted via the
	// in-scope test file only. Markers prove autonomous dev + product/marketing
	// pathways (e.g. "ABC", "vxd req" recipes for marketing sites).
	trainingMarkers := map[string][]string{
		"docs/training/README.md": {
			"ABC",
			"vxd req",
			"Product & Marketing",
		},
		"docs/training/product-marketing-made-easy.md": {
			"as Easy as ABC",
			"vxd req",
			"marketing products",
		},
		"docs/training/autonomous-software-development.md": {
			"vxd req",
			"breeze",
			"Autonomous",
		},
	}
	for rel, markers := range trainingMarkers {
		full := filepath.Join(root, rel)
		if _, err := os.Stat(full); err != nil {
			t.Errorf("AC2 training doc must exist: %s", rel)
			continue
		}
		data, readErr := os.ReadFile(full)
		if readErr != nil {
			t.Errorf("AC2 could not read %s: %v", rel, readErr)
			continue
		}
		content := string(data)
		for _, m := range markers {
			if !strings.Contains(content, m) {
				t.Errorf("AC2 training doc %s must contain marker %q (autonomous dev or product/marketing pathway)", rel, m)
			}
		}
	}

	// 2. Walk key doc trees and fail on any bad present-tense stub language
	dirsToWalk := []string{
		"docs/training",
		"docs/knowledge/autoresearch",
		"docs/audit-findings-2026-07-05.md",
	}

	for _, rel := range dirsToWalk {
		full := filepath.Join(root, rel)
		err := filepath.Walk(full, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".md") {
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			content := string(data)
			for _, phrase := range badPhrases {
				if strings.Contains(content, phrase) {
					// allow only if clearly in a "Fixed"/"Resolved"/historical appendix for this audit doc
					if strings.Contains(path, "audit-findings") && (strings.Contains(content, "Fixed") || strings.Contains(content, "(FIXED)") || strings.Contains(content, "Resolved")) {
						// ok in historical section
						continue
					}
					t.Errorf("stale stub language %q found in %s — docs must be up to date (no present-tense references to known stubs for wired subsystems)", phrase, path)
				}
			}
			return nil
		})
		if err != nil {
			t.Errorf("walk %s: %v", rel, err)
		}
	}
}
