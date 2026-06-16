package web

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestWritePidfile_AtomicCreate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dashboard.pid")
	if err := writePidfile(path, 4242); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pidfile: %v", err)
	}
	got, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("pidfile not numeric: %v", err)
	}
	if got != 4242 {
		t.Errorf("pid = %d, want 4242", got)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("pidfile mode = %o, want 0o600", mode)
	}
}

func TestWritePidfile_EmptyPathIsNoop(t *testing.T) {
	if err := writePidfile("", 1); err != nil {
		t.Errorf("empty path should be no-op, got err: %v", err)
	}
}

func TestWriteBootstrapFile_Mode0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dashboard.bootstrap")
	if err := writeBootstrapFile(path, "abc123"); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != "abc123" {
		t.Errorf("contents = %q, want abc123", string(data))
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("bootstrap mode = %o, want 0o600", mode)
	}
}

func TestWriteBootstrapFile_NewDirectoryMode0700(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "dashboard.bootstrap")
	if err := writeBootstrapFile(path, "abc"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	// MkdirAll only narrows permissions if the directory doesn't exist;
	// when it already exists with looser perms we leave them alone.
	// New subdir should be 0o700.
	if mode := info.Mode().Perm(); mode != 0o700 {
		t.Errorf("dir mode = %o, want 0o700", mode)
	}
}

func TestRemovePidfileIfMine_OnlyRemovesOwnedPid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dashboard.pid")
	if err := os.WriteFile(path, []byte("99\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Different PID — must NOT remove.
	removePidfileIfMine(path, 42)
	if _, err := os.Stat(path); err != nil {
		t.Errorf("pidfile owned by another pid should remain: %v", err)
	}

	// Matching PID — must remove.
	removePidfileIfMine(path, 99)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected pidfile removed for matching pid, stat err: %v", err)
	}
}

func TestIsLoopback(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:9090", true},
		{"127.0.0.1", true},
		{"[::1]:9090", true},
		{"::1", true},
		{"10.0.0.1:80", false},
		{"192.168.1.1:80", false},
		{"example.com:80", false},
	}
	for _, tc := range cases {
		got := isLoopback(tc.addr)
		if got != tc.want {
			t.Errorf("isLoopback(%q) = %v, want %v", tc.addr, got, tc.want)
		}
	}
}
