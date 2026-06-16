package dashstart

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// stubDoer lets tests substitute the HTTP probe.
type stubDoer struct {
	status int
	err    error
	calls  int32
}

func (s *stubDoer) Do(req *http.Request) (*http.Response, error) {
	atomic.AddInt32(&s.calls, 1)
	if s.err != nil {
		return nil, s.err
	}
	return &http.Response{
		StatusCode: s.status,
		Body:       http.NoBody,
	}, nil
}

func writePid(t *testing.T, dir string, pid int) string {
	t.Helper()
	p := filepath.Join(dir, "dashboard.pid")
	if err := os.WriteFile(p, []byte(strings.TrimSpace(formatPid(pid))+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func formatPid(p int) string {
	const digits = "0123456789"
	if p == 0 {
		return "0"
	}
	neg := p < 0
	if neg {
		p = -p
	}
	var buf [20]byte
	i := len(buf)
	for p > 0 {
		i--
		buf[i] = digits[p%10]
		p /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func TestIsAlive_HealthzOK(t *testing.T) {
	dir := t.TempDir()
	pidfile := writePid(t, dir, 12345)
	doer := &stubDoer{status: 200}

	ctx := context.Background()
	info, err := IsAlive(ctx, doer, pidfile, 8787)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if info.PID != 12345 {
		t.Errorf("PID = %d, want 12345", info.PID)
	}
	if info.Port != 8787 {
		t.Errorf("Port = %d, want 8787", info.Port)
	}
}

func TestIsAlive_HealthzUnreachable(t *testing.T) {
	dir := t.TempDir()
	writePid(t, dir, 12345)
	doer := &stubDoer{err: errors.New("connection refused")}

	_, err := IsAlive(context.Background(), doer, filepath.Join(dir, "dashboard.pid"), 8787)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrNotRunning) {
		t.Errorf("expected ErrNotRunning, got %v", err)
	}
}

func TestIsAlive_HealthzNon200(t *testing.T) {
	dir := t.TempDir()
	writePid(t, dir, 12345)
	doer := &stubDoer{status: 503}

	_, err := IsAlive(context.Background(), doer, filepath.Join(dir, "dashboard.pid"), 8787)
	if err == nil || !errors.Is(err, ErrNotRunning) {
		t.Fatalf("expected ErrNotRunning, got %v", err)
	}
}

func TestIsAlive_MissingPidfileStillProbes(t *testing.T) {
	// Daemon launched via launchd / systemd without a pidfile we know
	// about — healthz alone should still report alive.
	doer := &stubDoer{status: 200}
	info, err := IsAlive(context.Background(), doer, "/nonexistent/pid", 8787)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if info.PID != 0 {
		t.Errorf("PID should be 0 when pidfile missing, got %d", info.PID)
	}
	if info.Port != 8787 {
		t.Errorf("Port = %d, want 8787", info.Port)
	}
}

func TestWaitHealthy_BecomesHealthyMidPoll(t *testing.T) {
	doer := &flappingDoer{healthyAfter: 2}
	ctx := context.Background()
	_, err := WaitHealthy(ctx, doer, "/tmp/nope", 8787, 20*time.Millisecond, time.Second)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if doer.calls < 2 {
		t.Errorf("expected ≥2 probes before health, got %d", doer.calls)
	}
}

func TestWaitHealthy_Timeout(t *testing.T) {
	doer := &stubDoer{err: errors.New("connection refused")}
	ctx := context.Background()
	_, err := WaitHealthy(ctx, doer, "/tmp/nope", 8787, 20*time.Millisecond, 80*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

// flappingDoer becomes healthy after healthyAfter probes. Models a daemon
// that takes time to bind its listener.
type flappingDoer struct {
	healthyAfter int
	calls        int
}

func (f *flappingDoer) Do(req *http.Request) (*http.Response, error) {
	f.calls++
	if f.calls >= f.healthyAfter {
		return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
	}
	return nil, errors.New("connection refused")
}
