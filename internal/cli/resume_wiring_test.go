package cli

import (
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
)

// TestResume_WiresTechLeadFixer guards against a dead-wire regression: the
// post-merge integration-build feature (Monitor.SetTechLeadFixer +
// TechLeadFixer) was fully implemented and unit-tested, but runResume never
// called SetTechLeadFixer, so the stage never ran in production. The setter's
// own wiring test could not catch this. This test scans the resume source to
// confirm the fixer is actually constructed and attached.
func TestResume_WiresTechLeadFixer(t *testing.T) {
	src, err := os.ReadFile("resume.go")
	if err != nil {
		t.Fatalf("read resume.go: %v", err)
	}
	code := string(src)

	for _, want := range []string{"NewTechLeadFixer(", "SetTechLeadFixer("} {
		if !strings.Contains(code, want) {
			t.Errorf("resume.go must wire the post-merge integration fixer: missing %q", want)
		}
	}
}

// TestResume_WiresCompletionGate guards the requirement-completion gate against
// the same dead-wire class: the gate blocks REQ_COMPLETED on a red composed
// mainline, but only if runResume actually constructs and attaches it. This
// scans the resume source to confirm the gate is built and wired.
func TestResume_WiresCompletionGate(t *testing.T) {
	src, err := os.ReadFile("resume.go")
	if err != nil {
		t.Fatalf("read resume.go: %v", err)
	}
	code := string(src)

	for _, want := range []string{"NewCompletionGate(", "SetCompletionGate("} {
		if !strings.Contains(code, want) {
			t.Errorf("resume.go must wire the completion gate: missing %q", want)
		}
	}
}

// TestResume_WiresSecurityGate guards the per-story security gate against the
// dead-wire class: the gate scans + reviews each story before merge, but only if
// runResume constructs and attaches it.
func TestResume_WiresSecurityGate(t *testing.T) {
	src, err := os.ReadFile("resume.go")
	if err != nil {
		t.Fatalf("read resume.go: %v", err)
	}
	code := string(src)

	for _, want := range []string{"NewSecurityGate(", "SetSecurityGate(", "SetRequireScanners("} {
		if !strings.Contains(code, want) {
			t.Errorf("resume.go must wire the security gate: missing %q", want)
		}
	}
}

// TestResume_WiresCostMeter guards F2 cost tracking against the dead-wire
// class: usage is only recorded as STORY_COST_RECORDED if the resume path
// actually wraps its LLM clients in llm.NewMeteredClient with the
// store-backed costRecorder.
func TestResume_WiresCostMeter(t *testing.T) {
	src, err := os.ReadFile("resume.go")
	if err != nil {
		t.Fatalf("read resume.go: %v", err)
	}
	code := string(src)

	for _, want := range []string{"NewMeteredClient(", "costRecorder{"} {
		if !strings.Contains(code, want) {
			t.Errorf("resume.go must wire LLM cost metering: missing %q", want)
		}
	}
}

// TestResumeSignals_RespectsNohup: tmux kill-session (SIGTERM) and a
// terminal hangup (SIGHUP) cancel the monitor like Ctrl-C — unless the
// process started with SIGHUP ignored (nohup), in which case a hangup must
// keep being ignored rather than stop the run.
func TestResumeSignals_RespectsNohup(t *testing.T) {
	// The first half asserts what resumeSignals does when SIGHUP is NOT
	// ignored, which a test binary started under nohup cannot observe. Skip
	// rather than fail: the assertion below also relies on signal.Reset
	// restoring the default disposition, which is only the right one to
	// restore when the process did not start with SIGHUP ignored.
	if signal.Ignored(syscall.SIGHUP) {
		t.Skip("this process already ignores SIGHUP (nohup); resumeSignals' not-ignored branch is unreachable here")
	}
	has := func(sigs []os.Signal, want os.Signal) bool {
		for _, s := range sigs {
			if s == want {
				return true
			}
		}
		return false
	}
	sigs := resumeSignals()
	if !has(sigs, os.Interrupt) || !has(sigs, syscall.SIGTERM) || !has(sigs, syscall.SIGHUP) {
		t.Fatalf("with SIGHUP not ignored, want Interrupt, SIGTERM and SIGHUP, got %v", sigs)
	}

	signal.Ignore(syscall.SIGHUP)
	t.Cleanup(func() { signal.Reset(syscall.SIGHUP) })
	sigs = resumeSignals()
	if has(sigs, syscall.SIGHUP) {
		t.Fatalf("under nohup SIGHUP must stay ignored, got %v", sigs)
	}
	if !has(sigs, os.Interrupt) || !has(sigs, syscall.SIGTERM) {
		t.Fatalf("Interrupt and SIGTERM must remain, got %v", sigs)
	}
}

// TestResume_WiresResumeSignals guards the signal wiring the same way: the
// completion gate's test runner is only killed through the monitor ctx, so
// runResume must build that ctx from resumeSignals() — reverting it to
// signal.NotifyContext(ctx, os.Interrupt) passes every behavioural test
// (TestResumeSignals_RespectsNohup tests the helper alone).
func TestResume_WiresResumeSignals(t *testing.T) {
	src, err := os.ReadFile("resume.go")
	if err != nil {
		t.Fatalf("read resume.go: %v", err)
	}
	if want := "signal.NotifyContext(context.Background(), resumeSignals()...)"; !strings.Contains(string(src), want) {
		t.Errorf("resume.go must build the monitor ctx from resumeSignals(): missing %q", want)
	}
}

// TestRequirementSettled: with every story complete, resume stops only for a
// requirement that already has its verdict. A blocked one, and one the gate
// never reached a verdict on (the projection leaves it "planned"), re-run
// the completion gate (#138) — see TestRunResume_DryRun_UnsettledStaysUnsettled
// for what a run without a wired gate does with the same state.
func TestRequirementSettled(t *testing.T) {
	for status, want := range map[string]bool{"completed": true, "archived": true, "blocked": false, "planned": false, "": false} {
		if got := requirementSettled(status); got != want {
			t.Errorf("requirementSettled(%q) = %v, want %v", status, got, want)
		}
	}
}

// TestResume_WiresCompletionTestTimeout: deleting the two lines that pass
// qa.completion_test_timeout_s to the gate passes every behavioural test —
// the default bound simply applies — so the wiring is pinned by a source
// scan, as TestResume_WiresCostMeter pins its own.
func TestResume_WiresCompletionTestTimeout(t *testing.T) {
	src, err := os.ReadFile("resume.go")
	if err != nil {
		t.Fatalf("read resume.go: %v", err)
	}
	for _, want := range []string{"s.Config.QA.CompletionTestTimeoutS", "gate.SetTestTimeout("} {
		if !strings.Contains(string(src), want) {
			t.Errorf("resume.go must pass the configured suite timeout to the gate: missing %q", want)
		}
	}
}

// TestResume_UnblocksJustBeforeTheMonitor: unblockForGate emits REQ_RESUMED,
// which takes a red verdict off `vxd status` and stops `vxd watch` treating
// the requirement as terminal. Its placement is the invariant — everything
// that can fail has already succeeded, so a failure on the way leaves the
// verdict and its gaps file alone — and nothing but the order of two lines
// holds it. Moving the call up among the setup steps passes every
// behavioural test, which is what this guards.
func TestResume_UnblocksJustBeforeTheMonitor(t *testing.T) {
	src, err := os.ReadFile("resume.go")
	if err != nil {
		t.Fatalf("read resume.go: %v", err)
	}
	s := string(src)
	unblock := strings.Index(s, "unblockForGate(out, s, reqID, gateOnly)")
	run := strings.Index(s, "if err := monitor.RunWithContext(")
	if unblock < 0 || run < 0 {
		t.Fatalf("resume.go must unblock and then run the monitor (unblock=%d run=%d)", unblock, run)
	}
	if unblock > run {
		t.Fatal("unblockForGate must run before the monitor, not after it")
	}
	// A tripwire, not a proof: a future `if err = …` or a must-style helper
	// that panics would slip past it. It catches the change this is actually
	// about — moving the unblock back up among the setup steps.
	if between := s[unblock:run]; strings.Contains(between, "err :=") {
		t.Fatalf("nothing that can fail may run between the unblock and the monitor:\n%s", between)
	}
}
