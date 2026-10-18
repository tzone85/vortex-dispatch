package improve_test

import (
	"os"
	"strings"
	"testing"
)

// TestWiring_SelfAuditActivatedInDailyRun verifies the daily runner actually
// calls the self-audit, not merely that the package implements it.
func TestWiring_SelfAuditActivatedInDailyRun(t *testing.T) {
	src, err := os.ReadFile("../../cmd/vxd-improve/main.go")
	if err != nil {
		t.Skipf("cannot read vxd-improve main.go: %v", err)
	}
	body := string(src)
	for _, want := range []string{
		"Phase 0: Self-Audit",
		"NewSelfAuditor(",
		"MergeHealResult(",
		"BuildSelfAuditEmail(",
		"cfg.SelfHeal",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("WIRING FAILURE: vxd-improve main.go does not contain %q", want)
		}
	}
	if strings.Index(body, "Phase 0: Self-Audit") > strings.Index(body, "Phase 1: Research") {
		t.Error("WIRING FAILURE: self-audit must run before research")
	}
}
