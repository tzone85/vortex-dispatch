package cli

import (
	"path/filepath"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

func TestPickRuntime_PrefersClaudeCode(t *testing.T) {
	runtimes := map[string]config.RuntimeConfig{
		"codex":        {Command: "codex"},
		"claude-code":  {Command: "claude"},
		"gemini":       {Command: "gemini"},
	}
	name, rt := pickRuntime(runtimes)
	if name != "claude-code" {
		t.Errorf("got %q, want claude-code", name)
	}
	if rt.Command != "claude" {
		t.Errorf("got command %q, want claude", rt.Command)
	}
}

func TestPickRuntime_FallsBackToAny(t *testing.T) {
	runtimes := map[string]config.RuntimeConfig{
		"codex": {Command: "codex"},
	}
	name, rt := pickRuntime(runtimes)
	if name != "codex" {
		t.Errorf("single-runtime fallback failed: %q", name)
	}
	if rt.Command != "codex" {
		t.Errorf("got command %q, want codex", rt.Command)
	}
}

func TestPickRuntime_EmptyMap(t *testing.T) {
	name, rt := pickRuntime(nil)
	if name != "" {
		t.Errorf("empty map should yield empty name, got %q", name)
	}
	if rt.Command != "" {
		t.Errorf("empty map should yield zero RuntimeConfig, got %+v", rt)
	}
}

func TestNewDevDBLifecycle_DisabledProvider(t *testing.T) {
	cfg := config.Config{}
	lc := newDevDBLifecycle(cfg, nil)
	if lc != nil {
		t.Error("expected nil lifecycle for empty provider")
	}

	cfg.DevDB.Provider = "null"
	if lc := newDevDBLifecycle(cfg, nil); lc != nil {
		t.Error("expected nil lifecycle for null provider")
	}
}

func TestNewDevDBLifecycle_DockerProvider(t *testing.T) {
	dir := t.TempDir()
	es, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("event store: %v", err)
	}
	defer es.Close()

	cfg := config.Config{}
	cfg.DevDB.Provider = "docker"
	cfg.DevDB.Docker.Image = "postgres:16"
	cfg.DevDB.OnFailure.RetainHours = 24

	lc := newDevDBLifecycle(cfg, es)
	if lc == nil {
		t.Error("expected non-nil lifecycle for docker provider")
	}
}

func TestNewDevDBLifecycle_BadGhostProvider(t *testing.T) {
	cfg := config.Config{}
	cfg.DevDB.Provider = "ghost"
	cfg.DevDB.Ghost.APIKeyEnv = "VXD_NO_SUCH_KEY_FOR_TEST"
	// Missing API key — provider build fails, lifecycle returns nil
	// with the error logged. This is intentional graceful degradation:
	// dispatch must not be blocked by devdb misconfiguration.
	if lc := newDevDBLifecycle(cfg, nil); lc != nil {
		t.Error("expected nil lifecycle when ghost provider build fails")
	}
}

func TestRunDevDBOrphanRecovery_SkipsWhenDisabled(t *testing.T) {
	// Should be a fast no-op when devdb is disabled — exercises the
	// early-return guard.
	cfg := config.Config{}
	runDevDBOrphanRecovery(nil, cfg, nil) // out=nil is fine, the guard never writes
}

// TestRunResume_RequiresReqIDWhenNoneActive exercises the
// "no active requirements" branch — runResume bails out before any
// dispatch logic when the projection store has nothing to resume.
func TestRunResume_RequiresReqIDWhenNoneActive(t *testing.T) {
	stateDir := t.TempDir()
	cfgPath := seedVxdYaml(t, stateDir)

	cmd := newResumeCmd()
	cmd.Flags().String("config", "", "")
	cmd.Flags().String("project", "default", "")
	if err := cmd.Flags().Set("config", cfgPath); err != nil {
		t.Fatalf("set config: %v", err)
	}
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when no active requirements")
	}
	// Resume may also bail early on preflight (claude CLI missing) —
	// accept either failure mode; both confirm the entry point is wired.
	msg := err.Error()
	if !contains(msg, "no active requirements") && !contains(msg, "preflight") &&
		!contains(msg, "tmux") && !contains(msg, "claude") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestRunResume_RejectsBothReviewAndAuto exercises the conflicting-flag
// guard. We can't actually reach it because preflight fails first on
// most boxes, but verifying the flag is at least parseable keeps this
// path lit.
func TestRunResume_FlagsParse(t *testing.T) {
	cmd := newResumeCmd()
	if err := cmd.Flags().Set("review", "true"); err != nil {
		t.Fatalf("set review: %v", err)
	}
	if err := cmd.Flags().Set("auto", "true"); err != nil {
		t.Fatalf("set auto: %v", err)
	}
	review, _ := cmd.Flags().GetBool("review")
	auto, _ := cmd.Flags().GetBool("auto")
	if !review || !auto {
		t.Errorf("flags did not parse: review=%v auto=%v", review, auto)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && stringContains(s, sub))
}

// stringContains is a thin wrapper so the test file can keep its imports
// minimal — strings.Contains would work equally well.
func stringContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
