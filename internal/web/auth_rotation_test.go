package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestAuth_TokenRotatesAfterTTL pins the age-based rotation contract
// (WEAKNESSES.md P0-04): a token file older than the TTL is replaced with a
// fresh token at load; a fresh file is kept; ttl<=0 never rotates; the
// VXD_DASHBOARD_TOKEN env override is never rotated.
func TestAuth_TokenRotatesAfterTTL(t *testing.T) {
	t.Setenv("VXD_DASHBOARD_TOKEN", "") // isolate from operator env

	path := filepath.Join(t.TempDir(), "dashboard.token")

	// First load mints a token.
	original, rotated, err := LoadOrGenerateTokenWithTTL(path, 168*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if rotated {
		t.Error("first mint must not report rotated")
	}
	if len(original) != 64 {
		t.Fatalf("expected 32-byte hex token, got %d chars", len(original))
	}

	// Fresh file within TTL: same token back.
	same, rotated, err := LoadOrGenerateTokenWithTTL(path, 168*time.Hour)
	if err != nil || rotated || same != original {
		t.Fatalf("fresh token must be kept: tok match=%v rotated=%v err=%v", same == original, rotated, err)
	}

	// Age the file past the TTL: rotation.
	old := time.Now().Add(-169 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	fresh, rotated, err := LoadOrGenerateTokenWithTTL(path, 168*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !rotated {
		t.Fatal("stale token must rotate")
	}
	if fresh == original {
		t.Fatal("rotation must mint a NEW token")
	}
	// The file now holds the new token with 0o600.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != fresh {
		t.Error("token file not replaced with the fresh token")
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("token file perms = %o, want 600", fi.Mode().Perm())
	}

	// ttl<=0 disables rotation even for an ancient file.
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	kept, rotated, err := LoadOrGenerateTokenWithTTL(path, 0)
	if err != nil || rotated || kept != fresh {
		t.Fatalf("ttl=0 must never rotate: rotated=%v err=%v", rotated, err)
	}

	// Env override is operator-managed — returned verbatim, never rotated.
	t.Setenv("VXD_DASHBOARD_TOKEN", "operator-token")
	envTok, rotated, err := LoadOrGenerateTokenWithTTL(path, time.Nanosecond)
	if err != nil || rotated || envTok != "operator-token" {
		t.Fatalf("env override must win untouched: tok=%q rotated=%v err=%v", envTok, rotated, err)
	}
}

// TestRotateTokenFile pins unconditional rotation used by
// `vxd dashboard rotate-token`.
func TestRotateTokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dashboard.token")
	first, err := RotateTokenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := RotateTokenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("each rotation must mint a distinct token")
	}
	data, _ := os.ReadFile(path)
	if strings.TrimSpace(string(data)) != second {
		t.Error("file must hold the latest token")
	}
}
