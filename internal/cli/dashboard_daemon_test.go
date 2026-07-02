package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDashboardStatus_NotRunning(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "dashboard.pid") // never created
	cmd := newDashboardStatusCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--pidfile", pidfile})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("status must not error when daemon absent: %v", err)
	}
	if !strings.Contains(out.String(), "not running") {
		t.Errorf("expected 'not running', got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), pidfile) {
		t.Errorf("status should print the pidfile path it checked:\n%s", out.String())
	}
}

func TestDashboardStop_NoPidfile(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "dashboard.pid")
	cmd := newDashboardStopCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--pidfile", pidfile})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("stop must be idempotent with no pidfile: %v", err)
	}
	if !strings.Contains(out.String(), "not running") {
		t.Errorf("expected 'not running' note, got:\n%s", out.String())
	}
}

func TestDashboardStop_MalformedPidfile(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "dashboard.pid")
	if err := os.WriteFile(pidfile, []byte("not-a-pid"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := newDashboardStopCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--pidfile", pidfile})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "malformed pidfile") {
		t.Fatalf("expected malformed-pidfile error, got %v", err)
	}
}

func TestDashboardStop_StalePid(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "dashboard.pid")
	// PID far above pid_max on macOS/Linux defaults — signal gets ESRCH.
	if err := os.WriteFile(pidfile, []byte("99999999"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := newDashboardStopCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--pidfile", pidfile})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("stop on stale pid must be idempotent: %v", err)
	}
	if _, err := os.Stat(pidfile); !os.IsNotExist(err) {
		t.Error("stale pidfile must be removed")
	}
}

func TestDefaultDashboardPidfile_UnderHome(t *testing.T) {
	got := defaultDashboardPidfile()
	if !strings.HasSuffix(got, filepath.Join(".vxd", "dashboard.pid")) &&
		!strings.HasSuffix(got, "vxd-dashboard.pid") {
		t.Errorf("unexpected pidfile location %q", got)
	}
}

func TestPidStarted_BestEffort(t *testing.T) {
	// On hosts without /proc (macOS) this returns zero time and nil error; on
	// Linux it returns a real time. Either way it must never return an error.
	ts, err := pidStarted(os.Getpid())
	if err != nil {
		t.Fatalf("pidStarted must be best-effort, got error: %v", err)
	}
	_ = ts
}

func TestRunWatch_UnknownRequirement(t *testing.T) {
	cmd := newWatchCmd()
	driveWithVxdYaml(t, cmd, "does-not-exist")
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "get requirement") {
		t.Fatalf("expected get-requirement error for unknown req, got %v", err)
	}
}
