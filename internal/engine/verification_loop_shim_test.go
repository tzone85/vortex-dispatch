//go:build !windows

package engine

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/sanitize"
)

// shimRunner puts a #!/bin/sh script named name (go, npx) first on PATH so
// checkTests runs it instead of the real runner. Unix-only, like the scripts.
func shimRunner(t *testing.T, name, script string) {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o700); err != nil {
		t.Fatalf("write fake %s: %v", name, err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/bin"+string(os.PathListSeparator)+"/usr/bin")
}

// goModule writes a minimal go.mod so testEcosystem picks the Go runner.
func goModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/m\n\ngo 1.22\n"), 0o600); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	return dir
}

// hangingRunner shims `go test` with a script that announces itself by
// creating $VXD_TEST_MARKER and then sleeps, and returns the marker path.
// Waiting for the marker before cancelling guarantees the cancel lands while
// the suite is RUNNING (the group-kill path), not before it started.
//
// `exec sleep`, so the shim is ONE process. Without exec, sh forks the sleep,
// and a cancel that lands inside the fork window kills sh while the child —
// already holding the output pipe — is missed by the signal scan: Wait then
// sits out the wait delay and the cancel test's "the group kill landed" bound
// fails (seen once under -race). The leaked-child tests fork on purpose and
// keep their `&`.
func hangingRunner(t *testing.T) string {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "started")
	t.Setenv("VXD_TEST_MARKER", marker)
	shimRunner(t, "go", "#!/bin/sh\ncase \"$1\" in test) : > \"$VXD_TEST_MARKER\"; exec sleep 30;; *) exit 0;; esac\n")
	return marker
}

// awaitMarker blocks until the shim has started (or fails the test).
func awaitMarker(t *testing.T, marker string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the shimmed test runner never started")
}

// TestCheckTests_RunnerExitsNonZeroWithNoEvents_FailsClosed shims `go` on PATH
// with a script that prints nothing and exits 2, exercising the reconcileExit
// branch that no parsed event can reach.
func TestCheckTests_RunnerExitsNonZeroWithNoEvents_FailsClosed(t *testing.T) {
	shimRunner(t, "go", "#!/bin/sh\nexit 2\n")
	dir := goModule(t)
	passing, failing, total, gaps := checkTests(context.Background(), dir, defaultBounds())
	gap := oneGap(t, gaps)
	if passing != 0 || failing != 1 || total != 1 {
		t.Fatalf("expected 0/1/1 when the runner exits non-zero with no events; got %d/%d/%d", passing, failing, total)
	}
	if gap == nil || gap.Category != "test" || gap.Severity != "critical" || !strings.Contains(gap.Detail, "exited before or without reporting") {
		t.Fatalf("expected a critical 'exited before or without reporting' gap, got %+v", gap)
	}
}

// TestCheckTests_LegacyToolchainPlainStderr: an older toolchain prints the
// compiler error as plain stderr and only the "[build failed]" JSON line; the
// gap must still carry the error. PATH-shimmed runner.
func TestCheckTests_LegacyToolchainPlainStderr(t *testing.T) {
	shimRunner(t, "go", "#!/bin/sh\ncase \"$1\" in test) printf '%s\\n' '# example.com/m [example.com/m.test]' './lib_test.go:6:8: undefined: Undefined' '{\"Action\":\"output\",\"Package\":\"example.com/m\",\"Output\":\"FAIL\\texample.com/m [build failed]\\n\"}' '{\"Action\":\"fail\",\"Package\":\"example.com/m\"}'; exit 1;; *) exit 0;; esac\n")
	dir := goModule(t)
	passing, failing, total, gaps := checkTests(context.Background(), dir, defaultBounds())
	gap := oneGap(t, gaps)
	if passing != 0 || failing != 1 || total != 1 {
		t.Fatalf("want 0/1/1, got %d/%d/%d", passing, failing, total)
	}
	if gap == nil || !strings.Contains(gap.Detail, "failed to compile") || !strings.Contains(gap.Output, "undefined: Undefined") {
		t.Fatalf("expected a compile gap carrying the plain-stderr error, got %+v", gap)
	}
}

// TestCheckTests_LegacyBuildFailedLineWithExitZeroStaysGreen: even the exact
// legacy line cannot turn a run red when the runner exited 0 — the exit
// status vetoes untrusted output.
func TestCheckTests_LegacyBuildFailedLineWithExitZeroStaysGreen(t *testing.T) {
	script := "#!/bin/sh\ncase \"$1\" in test) printf '%s\\n' '{\"Action\":\"output\",\"Package\":\"example.com/m\",\"Output\":\"FAIL\\texample.com/m [build failed]\\n\"}' '{\"Action\":\"pass\",\"Package\":\"example.com/m\",\"Test\":\"TestOK\"}'; exit 0;; *) exit 0;; esac\n"
	shimRunner(t, "go", script)
	dir := goModule(t)
	passing, failing, total, gaps := checkTests(context.Background(), dir, defaultBounds())
	gap := oneGap(t, gaps)
	if passing != 1 || failing != 0 || total != 1 || gap != nil {
		t.Fatalf("exit 0 must veto the legacy line: want 1/0/1 no gap, got %d/%d/%d %+v", passing, failing, total, gap)
	}
}

// TestCheckTests_PlainTestFailure_GapCarriesTestOutput: the most common red —
// tests that fail — is a gap too, with the failing tests' output.
func TestCheckTests_PlainTestFailure_GapCarriesTestOutput(t *testing.T) {
	script := "#!/bin/sh\ncase \"$1\" in test) printf '%s\\n' '{\"Action\":\"run\",\"Package\":\"example.com/m\",\"Test\":\"TestBad\"}' '{\"Action\":\"output\",\"Package\":\"example.com/m\",\"Test\":\"TestBad\",\"Output\":\"    bad_test.go:9: want 3, got 4\\n\"}' '{\"Action\":\"fail\",\"Package\":\"example.com/m\",\"Test\":\"TestBad\"}' '{\"Action\":\"fail\",\"Package\":\"example.com/m\"}'; exit 1;; *) exit 0;; esac\n"
	shimRunner(t, "go", script)
	dir := goModule(t)
	passing, failing, total, gaps := checkTests(context.Background(), dir, defaultBounds())
	gap := oneGap(t, gaps)
	if passing != 0 || failing != 1 || total != 1 {
		t.Fatalf("want 0/1/1, got %d/%d/%d", passing, failing, total)
	}
	if gap == nil || !strings.Contains(gap.Detail, "1 test(s) failed") || !strings.Contains(gap.Output, "want 3, got 4") {
		t.Fatalf("expected a test-failure gap carrying the failing test's output, got %+v", gap)
	}
}

// TestCheckTests_NodeRunnerExitsNonZero_FailsClosed: the fail-closed rule and
// the gap apply to jest/vitest too, with ecosystem-neutral wording.
func TestCheckTests_NodeRunnerExitsNonZero_FailsClosed(t *testing.T) {
	shimRunner(t, "npx", "#!/bin/sh\necho 'Error: Jest: Failed to parse the TypeScript config'; exit 1\n")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"m"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	passing, failing, total, gaps := checkTests(context.Background(), dir, defaultBounds())
	gap := oneGap(t, gaps)
	if passing != 0 || failing != 1 || total != 1 {
		t.Fatalf("want 0/1/1, got %d/%d/%d", passing, failing, total)
	}
	if gap == nil || strings.Contains(gap.Detail, "compile") || !strings.Contains(gap.Output, "Failed to parse the TypeScript config") {
		t.Fatalf("expected an ecosystem-neutral gap carrying the runner output, got %+v", gap)
	}
}

// TestCheckTests_NodeGreenJSON_IsGreen: jest --json prints "numPassedTests"
// and "numFailedTests":0 on ONE line, which the line scan reads as a failure
// (#136) — the gate would then dispatch a fix agent against green code and
// block. The runner exited 0, and for jest and vitest alike that is the
// verdict.
func TestCheckTests_NodeGreenJSON_IsGreen(t *testing.T) {
	shimRunner(t, "npx", "#!/bin/sh\necho '{\"numFailedTestSuites\":0,\"numFailedTests\":0,\"numPassedTests\":3,\"success\":true}'\nexit 0\n")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"m"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	passing, failing, total, gaps := checkTests(context.Background(), dir, defaultBounds())
	if failing != 0 || len(gaps) != 0 {
		t.Fatalf("a suite that exited 0 is green: got %d/%d/%d gaps=%+v", passing, failing, total, gaps)
	}
	if passing == 0 || total != passing {
		t.Fatalf("the summary line is still counted as a pass: got %d/%d/%d", passing, failing, total)
	}
}

// TestCheckTests_CancelledContext_NoVerdict: a ctx cancelled while the suite
// is running must not read as "failed to compile"; it fails closed with an
// "aborted" gap so the gate can refuse to record a verdict, and the process
// group kill brings checkTests back well within verifyWaitDelay.
func TestCheckTests_CancelledContext_NoVerdict(t *testing.T) {
	marker := hangingRunner(t)
	dir := goModule(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type result struct {
		passing, failing, total int
		gap                     *VerificationGap
	}
	done := make(chan result, 1)
	go func() {
		p, f, n, gs := checkTests(ctx, dir, defaultBounds())
		done <- result{p, f, n, firstGap(gs)}
	}()
	awaitMarker(t, marker)
	cancelledAt := time.Now()
	cancel()
	r := <-done
	if elapsed := time.Since(cancelledAt); elapsed >= verifyWaitDelay {
		t.Fatalf("checkTests took %s after cancel — the group kill missed and the WaitDelay fallback saved the run", elapsed)
	}
	if r.passing != 0 || r.failing != 1 || r.total != 1 {
		t.Fatalf("want 0/1/1 on cancel, got %d/%d/%d", r.passing, r.failing, r.total)
	}
	if r.gap == nil || !strings.Contains(r.gap.Detail, "aborted") || strings.Contains(r.gap.Detail, "compile") {
		t.Fatalf("expected an 'aborted — no verdict' gap, got %+v", r.gap)
	}
}

// TestExitStatus: an *exec.ExitError renders the exit status, an *exec.Error
// says the runner could not start, anything else is a bare failure.
func TestExitStatus(t *testing.T) {
	cmd := exec.Command("sh", "-c", "exit 3")
	err := cmd.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("precondition: want an ExitError, got %v", err)
	}
	if got := exitStatus(err); got != "exit status 3" {
		t.Errorf("ExitError: got %q", got)
	}
	if got := exitStatus(&exec.Error{Name: "go", Err: exec.ErrNotFound}); !strings.HasPrefix(got, "could not start: ") {
		t.Errorf("exec.Error: got %q", got)
	}
	if got := exitStatus(errors.New("boom")); got != "failed: boom" {
		t.Errorf("plain error: got %q", got)
	}
}

// TestCheckTests_RunnerExitsZero_LeakedChildHoldsPipe_StaysGreen: a runner
// that exits 0 while a grandchild keeps the output pipe open (a jest
// globalSetup dev server, say) returns exec.ErrWaitDelay from CombinedOutput.
// The exit status is the verdict; the leaked child must not turn a green run
// red with a "could not start" message.
func TestCheckTests_RunnerExitsZero_LeakedChildHoldsPipe_StaysGreen(t *testing.T) {
	// 200ms, by argument: the shim's child outlives the delay on purpose, and
	// the production 5s would just be dead time.
	bounds := defaultBounds()
	bounds.waitDelay = 200 * time.Millisecond
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	t.Setenv("VXD_TEST_PIDFILE", pidFile)
	shimRunner(t, "go", "#!/bin/sh\ncase \"$1\" in test) sleep 30 & echo $! > \"$VXD_TEST_PIDFILE\"; echo '{\"Action\":\"pass\",\"Package\":\"example.com/m\",\"Test\":\"TestOK\"}'; exit 0;; *) exit 0;; esac\n")
	dir := goModule(t)
	passing, failing, total, gaps := checkTests(context.Background(), dir, bounds)
	gap := oneGap(t, gaps)
	if passing != 1 || failing != 0 || total != 1 {
		t.Fatalf("want 1/0/1 for a green run with a leaked child, got %d/%d/%d", passing, failing, total)
	}
	if gap != nil {
		t.Fatalf("no gap expected, got %+v", gap)
	}
	// The leaked child must not outlive the gate: checkTests kills the
	// runner's process group once WaitDelay has expired.
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("the shim did not record its child's pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("bad pid %q: %v", raw, err)
	}
	if !processGone(pid, 3*time.Second) {
		t.Fatalf("leaked child %d is still running after checkTests returned", pid)
	}
}

// TestCheckTests_RunnerExitsNonZero_LeakedChild_IsKilled: the same leak under
// a FAILING runner. Wait reports the ExitError, never ErrWaitDelay, so the
// kill must not hang off the ErrWaitDelay branch — a jest globalSetup dev
// server plus a failing test would otherwise outlive the gate and the fix
// agent would run against a port that is still bound.
func TestCheckTests_RunnerExitsNonZero_LeakedChild_IsKilled(t *testing.T) {
	bounds := defaultBounds()
	bounds.waitDelay = 200 * time.Millisecond
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	t.Setenv("VXD_TEST_PIDFILE", pidFile)
	shimRunner(t, "go", "#!/bin/sh\ncase \"$1\" in test) sleep 30 & echo $! > \"$VXD_TEST_PIDFILE\"; printf '%s\\n' '{\"Action\":\"run\",\"Package\":\"example.com/m\",\"Test\":\"TestBad\"}' '{\"Action\":\"fail\",\"Package\":\"example.com/m\",\"Test\":\"TestBad\"}' '{\"Action\":\"fail\",\"Package\":\"example.com/m\"}'; exit 1;; *) exit 0;; esac\n")
	dir := goModule(t)
	passing, failing, total, gaps := checkTests(context.Background(), dir, bounds)
	if passing != 0 || failing != 1 || total != 1 {
		t.Fatalf("want 0/1/1 for a red run with a leaked child, got %d/%d/%d", passing, failing, total)
	}
	if g := oneGap(t, gaps); g == nil || !strings.Contains(g.Detail, "1 test(s) failed") {
		t.Fatalf("want the test-failure gap, got %+v", g)
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("the shim did not record its child's pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("bad pid %q: %v", raw, err)
	}
	if !processGone(pid, 3*time.Second) {
		t.Fatalf("leaked child %d of a failing runner is still running after checkTests returned", pid)
	}
}

// processGone reports whether pid is dead (or a zombie nobody has reaped)
// within the wait; SIGKILL delivery is asynchronous.
func processGone(pid int, wait time.Duration) bool {
	deadline := time.Now().Add(wait)
	for {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return true
		}
		if out, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output(); err == nil && strings.Contains(string(out), "Z") {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestCheckTests_SecretInRunnerOutput_IsRedacted: a token echoed by the
// suite (an env var in a failure message, a fixture) must not reach the gap,
// which is written to .vxd-fix-gaps.md and sent in the fix prompt.
func TestCheckTests_SecretInRunnerOutput_IsRedacted(t *testing.T) {
	shimRunner(t, "go", "#!/bin/sh\ncase \"$1\" in test) echo 'FAIL: token was ghp_ABCDEFghijklmnop1234567890abcdefghij'; exit 2;; *) exit 0;; esac\n")
	dir := goModule(t)
	_, failing, _, gaps := checkTests(context.Background(), dir, defaultBounds())
	gap := oneGap(t, gaps)
	if failing != 1 || gap == nil {
		t.Fatalf("expected a fail-closed gap, got failing=%d gap=%+v", failing, gap)
	}
	if strings.Contains(gap.Output, "ghp_ABCDEF") {
		t.Fatalf("token leaked into the gap output: %q", gap.Output)
	}
	if !strings.Contains(gap.Output, "token was "+sanitize.Redacted) {
		t.Fatalf("expected the redaction marker in place of the token, got %q", gap.Output)
	}
}

// TestRunVerificationLoop_Cancelled_NoSideEffects: once the suite is aborted
// there is no verdict, so the loop must not go on to scan, clean the
// workspace (which commits) or judge the README.
func TestRunVerificationLoop_Cancelled_NoSideEffects(t *testing.T) {
	marker := hangingRunner(t)
	dir := goModule(t)
	artifact := filepath.Join(dir, "WAVE_CONTEXT.md")
	if err := os.WriteFile(artifact, []byte("wave\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan VerificationResult, 1)
	go func() { done <- RunVerificationLoop(ctx, dir, 1) }()
	awaitMarker(t, marker)
	cancel()
	res := <-done
	if res.TestsFailing != 1 || len(res.Gaps) != 1 || !strings.Contains(res.Gaps[0].Detail, "aborted") {
		t.Fatalf("want the single aborted gap, got failing=%d gaps=%+v", res.TestsFailing, res.Gaps)
	}
	if res.CleanArtifacts {
		t.Fatal("an aborted cycle must not report the workspace as cleaned")
	}
	if _, err := os.Stat(artifact); err != nil {
		t.Fatalf("an aborted cycle must leave the working tree alone: %v", err)
	}
}

// TestCheckTests_BuildFailPlusTestFailure_BothGapsCarryEvidence: package A
// has a failing test and package B does not compile — both are reported,
// each with its own evidence, so the fix agent sees A on the first cycle.
func TestCheckTests_BuildFailPlusTestFailure_BothGapsCarryEvidence(t *testing.T) {
	script := "#!/bin/sh\ncase \"$1\" in test) printf '%s\\n' " +
		"'{\"Action\":\"run\",\"Package\":\"example.com/a\",\"Test\":\"TestBad\"}' " +
		"'{\"Action\":\"output\",\"Package\":\"example.com/a\",\"Test\":\"TestBad\",\"Output\":\"    bad_test.go:9: want 3, got 4\\n\"}' " +
		"'{\"Action\":\"fail\",\"Package\":\"example.com/a\",\"Test\":\"TestBad\"}' " +
		"'{\"Action\":\"fail\",\"Package\":\"example.com/a\"}' " +
		"'{\"Action\":\"build-output\",\"ImportPath\":\"example.com/b [example.com/b.test]\",\"Output\":\"# example.com/b [example.com/b.test]\\n./b_test.go:5:2: undefined: Missing\\n\"}' " +
		"'{\"Action\":\"build-fail\",\"ImportPath\":\"example.com/b [example.com/b.test]\"}' " +
		"'{\"Action\":\"fail\",\"Package\":\"example.com/b\",\"FailedBuild\":\"example.com/b [example.com/b.test]\"}'; exit 1;; *) exit 0;; esac\n"
	shimRunner(t, "go", script)
	dir := goModule(t)
	passing, failing, total, gaps := checkTests(context.Background(), dir, defaultBounds())
	if passing != 0 || failing != 2 || total != 2 {
		t.Fatalf("want 0/2/2 (one failing test, one package that did not build), got %d/%d/%d", passing, failing, total)
	}
	if len(gaps) != 2 {
		t.Fatalf("want one gap per failure class, got %+v", gaps)
	}
	if g := gapWith(t, gaps, "undefined: Missing"); !strings.Contains(g.Detail, "failed to compile") {
		t.Fatalf("the compile gap must say so, got %+v", g)
	}
	if g := gapWith(t, gaps, "want 3, got 4"); !strings.Contains(g.Detail, "1 test(s) failed") {
		t.Fatalf("the test-failure gap must count the failing test, got %+v", g)
	}
}

// TestCheckTests_EarlyExitPlusTestFailure_BothGapsCarryEvidence: package A
// has a failing test and package B's TestMain exited before its tests ran —
// both are reported with their own evidence.
func TestCheckTests_EarlyExitPlusTestFailure_BothGapsCarryEvidence(t *testing.T) {
	script := "#!/bin/sh\ncase \"$1\" in test) printf '%s\\n' " +
		"'{\"Action\":\"run\",\"Package\":\"example.com/a\",\"Test\":\"TestBad\"}' " +
		"'{\"Action\":\"output\",\"Package\":\"example.com/a\",\"Test\":\"TestBad\",\"Output\":\"    bad_test.go:9: want 3, got 4\\n\"}' " +
		"'{\"Action\":\"fail\",\"Package\":\"example.com/a\",\"Test\":\"TestBad\"}' " +
		"'{\"Action\":\"fail\",\"Package\":\"example.com/a\"}' " +
		"'{\"Action\":\"output\",\"Package\":\"example.com/b\",\"Output\":\"TestMain: database unreachable, exiting\\n\"}' " +
		"'{\"Action\":\"fail\",\"Package\":\"example.com/b\"}'; exit 1;; *) exit 0;; esac\n"
	shimRunner(t, "go", script)
	dir := goModule(t)
	passing, failing, total, gaps := checkTests(context.Background(), dir, defaultBounds())
	if passing != 0 || failing != 2 || total != 2 {
		t.Fatalf("want 0/2/2 (one failing test, one early exit), got %d/%d/%d", passing, failing, total)
	}
	if len(gaps) != 2 {
		t.Fatalf("want one gap per failure class, got %+v", gaps)
	}
	if g := gapWith(t, gaps, "database unreachable"); !strings.Contains(g.Detail, "ended without reporting test-level failures") {
		t.Fatalf("the early-exit gap must say so, got %+v", g)
	}
	if g := gapWith(t, gaps, "want 3, got 4"); !strings.Contains(g.Detail, "1 test(s) failed") {
		t.Fatalf("the test-failure gap must count the failing test, got %+v", g)
	}
}

// TestCheckTests_Timeout_YieldsUnfixableTimeoutGap: a suite that does not
// finish within the bound it was given is a timeout gap — gapTimeout, so the
// gate blocks on it without an auto-fix cycle — built by checkTests itself,
// not by hand.
func TestCheckTests_Timeout_YieldsUnfixableTimeoutGap(t *testing.T) {
	// 2s, not 200ms: on a loaded -race runner sh could be killed before it
	// touches the marker, and awaitMarker would then burn 10s to fail with
	// "never started". The bound is the parameter the gate passes, so no
	// package state changes.
	const timeout = 2 * time.Second
	marker := hangingRunner(t)
	dir := goModule(t)
	passing, failing, total, gaps := checkTests(context.Background(), dir, defaultBounds().withTestTimeout(timeout))
	awaitMarker(t, marker) // the shim did start; the timeout, not a start failure, ended it
	if passing != 0 || failing != 1 || total != 1 {
		t.Fatalf("want 0/1/1 on timeout, got %d/%d/%d", passing, failing, total)
	}
	gap := oneGap(t, gaps)
	if gap == nil || gap.Kind != gapTimeout || gap.Detail != timeoutDetailPrefix+timeout.String() {
		t.Fatalf("want a timeout gap naming the bound, got %+v", gap)
	}
	if !hasUnfixableGap(VerificationResult{TestsFailing: failing, Gaps: gaps}) {
		t.Fatal("the gate must refuse to auto-fix the real timeout result")
	}
}

// TestCheckTests_Timeout_RedactsJSONSecretInGoOutput: the timeout gap is the
// one place where the raw `go test -json` stream is all the evidence there
// is. A secret a test printed appears there JSON-escaped, which no secret
// pattern matches, so the gap must carry the DECODED text — redacted.
func TestCheckTests_Timeout_RedactsJSONSecretInGoOutput(t *testing.T) {
	const timeout = 2 * time.Second
	marker := filepath.Join(t.TempDir(), "started")
	t.Setenv("VXD_TEST_MARKER", marker)
	shimRunner(t, "go", "#!/bin/sh\ncase \"$1\" in test) "+
		"printf '%s\\n' '{\"Action\":\"output\",\"Package\":\"p\",\"Test\":\"TestX\",\"Output\":\"{\\\"password\\\": \\\"hunter2\\\"}\\n\"}'; "+
		": > \"$VXD_TEST_MARKER\"; exec sleep 30;; *) exit 0;; esac\n")
	dir := goModule(t)
	_, failing, _, gaps := checkTests(context.Background(), dir, defaultBounds().withTestTimeout(timeout))
	awaitMarker(t, marker) // the shim did start; the timeout, not a start failure, ended it
	gap := oneGap(t, gaps)
	if failing != 1 || gap == nil || gap.Kind != gapTimeout {
		t.Fatalf("want the single timeout gap, got failing=%d gap=%+v", failing, gap)
	}
	if strings.Contains(gap.Output, "hunter2") {
		t.Fatalf("the JSON-escaped secret reached the gap: %q", gap.Output)
	}
	if !strings.Contains(gap.Output, sanitize.Redacted) {
		t.Fatalf("want the redaction marker in the decoded output, got %q", gap.Output)
	}
}

// TestCheckTests_EarlyExit_RawFallback_Redacted: a package that ends without
// a test-level failure and without package-level output falls back to the
// runner's own stream. That fallback goes through the same decode, so the
// secret is redacted there too — the shim attaches its output to a TEST, so
// failedPkgOutput is empty and every line is an event (nonJSONLines is empty
// too): the fallback is the only branch left.
func TestCheckTests_EarlyExit_RawFallback_Redacted(t *testing.T) {
	shimRunner(t, "go", "#!/bin/sh\ncase \"$1\" in test) "+
		"printf '%s\\n' '{\"Action\":\"output\",\"Package\":\"p\",\"Test\":\"TestX\",\"Output\":\"{\\\"password\\\": \\\"hunter2\\\"}\\n\"}'; "+
		"printf '%s\\n' '{\"Action\":\"fail\",\"Package\":\"p\"}'; exit 1;; *) exit 0;; esac\n")
	dir := goModule(t)
	_, failing, _, gaps := checkTests(context.Background(), dir, defaultBounds())
	gap := oneGap(t, gaps)
	if failing != 1 || gap == nil || gap.Kind != gapEarlyExit {
		t.Fatalf("want the single early-exit gap, got failing=%d gap=%+v", failing, gap)
	}
	if strings.Contains(gap.Output, "hunter2") {
		t.Fatalf("the JSON-escaped secret reached the gap: %q", gap.Output)
	}
	if !strings.Contains(gap.Output, sanitize.Redacted) {
		t.Fatalf("want the redaction marker in the decoded output, got %q", gap.Output)
	}
}
