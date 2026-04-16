package cli

import (
	"fmt"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/secrets"
)

func TestResolveAPIKey_DefaultEnvProvider(t *testing.T) {
	t.Setenv("TEST_SECRET_FOR_RESOLVE", "test-value")
	got := resolveAPIKey("TEST_SECRET_FOR_RESOLVE")
	if got != "test-value" {
		t.Errorf("resolveAPIKey = %q, want %q", got, "test-value")
	}
}

func TestResolveAPIKey_MissingReturnsEmpty(t *testing.T) {
	got := resolveAPIKey("DEFINITELY_NOT_SET_KEY_XYZ")
	if got != "" {
		t.Errorf("missing key should return empty, got %q", got)
	}
}

func TestSetSecretsProvider_Swappable(t *testing.T) {
	// Save and restore default
	original := secretsProvider
	defer SetSecretsProvider(original)

	// Swap to a stub provider
	stub := &stubProvider{values: map[string]string{"FAKE_KEY": "from-stub"}}
	SetSecretsProvider(stub)

	got := resolveAPIKey("FAKE_KEY")
	if got != "from-stub" {
		t.Errorf("after swap: got %q, want %q", got, "from-stub")
	}
}

type stubProvider struct {
	values map[string]string
}

func (s *stubProvider) Get(key string) (string, error) {
	if v, ok := s.values[key]; ok {
		return v, nil
	}
	return "", fmt.Errorf("not found: %s", key)
}

func (s *stubProvider) Name() string { return "stub" }

// Compile-time check: stubProvider satisfies secrets.Provider.
var _ secrets.Provider = (*stubProvider)(nil)
