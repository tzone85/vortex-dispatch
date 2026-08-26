package cli

import (
	"os"
	"strings"
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
