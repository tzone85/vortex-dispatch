package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// TestDashboardRotateTokenCmd pins the manual rotation command
// (WEAKNESSES.md P0-04): the token file is replaced with a fresh 0o600
// token, the new token is printed, and the operator is told to restart a
// running daemon.
func TestDashboardRotateTokenCmd(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "dashboard.token")
	if err := os.WriteFile(tokenFile, []byte("old-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := newDashboardRotateTokenCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--token-file", tokenFile})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("rotate-token: %v", err)
	}

	data, err := os.ReadFile(tokenFile)
	if err != nil {
		t.Fatal(err)
	}
	newTok := strings.TrimSpace(string(data))
	if newTok == "old-token" {
		t.Fatal("token file was not replaced")
	}
	if len(newTok) != 64 {
		t.Fatalf("expected 32-byte hex token, got %d chars", len(newTok))
	}
	if fi, _ := os.Stat(tokenFile); fi.Mode().Perm() != 0o600 {
		t.Errorf("token file perms = %o, want 600", fi.Mode().Perm())
	}
	if !strings.Contains(out.String(), newTok) {
		t.Error("command must print the new token")
	}
	if !strings.Contains(out.String(), "vxd dashboard stop") {
		t.Error("command must tell the operator to restart a running daemon")
	}
}

// TestDashboardTokenTTL pins the token_ttl_hours mapping: 0 → 168h default,
// negative → disabled, positive → verbatim.
func TestDashboardTokenTTL(t *testing.T) {
	if got := dashboardTokenTTL(0); got != 168*time.Hour {
		t.Errorf("default = %v, want 168h", got)
	}
	if got := dashboardTokenTTL(-1); got != 0 {
		t.Errorf("negative must disable rotation, got %v", got)
	}
	if got := dashboardTokenTTL(24); got != 24*time.Hour {
		t.Errorf("verbatim = %v, want 24h", got)
	}
}
