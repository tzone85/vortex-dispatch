package tmux_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/tmux"
)

// tmuxShowEnv returns the value of a global tmux environment variable, or
// empty string if unset. The second return value is false when the variable
// is not present.
func tmuxShowEnv(t *testing.T, key string) (string, bool) {
	t.Helper()
	cmd := exec.Command("tmux", "show-environment", "-g", key)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// "unknown variable" is returned when the var does not exist.
		return "", false
	}
	line := strings.TrimSpace(string(out))
	// Format: KEY=VALUE
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return "", false
	}
	return parts[1], true
}

func TestPropagateEnv_SetsVariables(t *testing.T) {
	skipIfNoTmux(t)

	// Use a unique test key unlikely to collide with real env.
	const testKey = "VXD_TEST_PROPAGATE_KEY"
	const testVal = "test-value-12345"

	// Ensure the variable is set in the current process.
	t.Setenv(testKey, testVal)

	// Ensure clean state: remove from tmux global env if present.
	exec.Command("tmux", "set-environment", "-g", "-u", testKey).Run()

	// We need a tmux server running; create a temporary session.
	name := "vxd-test-env-prop"
	tmux.KillSession(name)
	if err := tmux.CreateSession(name, "/tmp", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer tmux.KillSession(name)

	// Propagate the test variable.
	tmux.PropagateEnv([]string{testKey})

	// Verify it's in the tmux global environment.
	got, ok := tmuxShowEnv(t, testKey)
	if !ok {
		t.Fatalf("expected %s to be set in tmux global env", testKey)
	}
	if got != testVal {
		t.Fatalf("expected %s=%q, got %q", testKey, testVal, got)
	}

	// Clean up tmux env.
	exec.Command("tmux", "set-environment", "-g", "-u", testKey).Run()
}

func TestPropagateEnv_UnsetsRemovedVariables(t *testing.T) {
	skipIfNoTmux(t)

	const testKey = "VXD_TEST_PROPAGATE_UNSET"

	// We need a tmux server running; create a temporary session.
	name := "vxd-test-env-unset"
	tmux.KillSession(name)
	if err := tmux.CreateSession(name, "/tmp", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer tmux.KillSession(name)

	// Manually set in tmux global env to simulate a stale value.
	exec.Command("tmux", "set-environment", "-g", testKey, "stale-value").Run()

	// Make sure it's NOT in the current process env.
	os.Unsetenv(testKey)

	// Propagate -- should unset the stale value.
	tmux.PropagateEnv([]string{testKey})

	// Verify it's no longer in the tmux global environment.
	_, ok := tmuxShowEnv(t, testKey)
	if ok {
		t.Fatalf("expected %s to be unset in tmux global env after propagation", testKey)
	}
}

func TestPropagateCriticalEnv_DoesNotPanic(t *testing.T) {
	skipIfNoTmux(t)

	// We need a tmux server running; create a temporary session.
	name := "vxd-test-env-critical"
	tmux.KillSession(name)
	if err := tmux.CreateSession(name, "/tmp", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer tmux.KillSession(name)

	// Should not panic regardless of which env vars are set.
	tmux.PropagateCriticalEnv()
}
