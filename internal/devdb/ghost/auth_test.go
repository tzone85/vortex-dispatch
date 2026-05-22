package ghost_test

import (
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/devdb/ghost"
)

func TestResolveAPIKey_ExplicitWins(t *testing.T) {
	t.Setenv("GHOST_API_KEY_TEST_EXPLICIT", "from-env")
	got, err := ghost.ResolveAPIKey("GHOST_API_KEY_TEST_EXPLICIT", "explicit-value")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "explicit-value" {
		t.Errorf("explicit key should win; got %q", got)
	}
}

func TestResolveAPIKey_FromEnv(t *testing.T) {
	t.Setenv("GHOST_API_KEY_UNIT", "env-key-123")
	got, err := ghost.ResolveAPIKey("GHOST_API_KEY_UNIT", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "env-key-123" {
		t.Errorf("expected env-key-123, got %q", got)
	}
}

func TestResolveAPIKey_DefaultEnvName(t *testing.T) {
	// When envVar is empty, ResolveAPIKey falls back to GHOST_API_KEY.
	t.Setenv("GHOST_API_KEY", "default-env-key")
	got, err := ghost.ResolveAPIKey("", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "default-env-key" {
		t.Errorf("expected default-env-key, got %q", got)
	}
}

func TestResolveAPIKey_Missing(t *testing.T) {
	// Use a unique env var name that is definitely not set.
	_, err := ghost.ResolveAPIKey("GHOST_API_KEY_NOT_SET_XYZ_12345", "")
	if err == nil {
		t.Error("expected error when no key is available")
	}
}
