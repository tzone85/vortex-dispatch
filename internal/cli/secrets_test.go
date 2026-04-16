package cli

import (
	"fmt"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/config"
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

func TestConfigureSecretsFromConfig_DefaultIsEnv(t *testing.T) {
	original := secretsProvider
	defer SetSecretsProvider(original)

	cfg := config.DefaultConfig()
	configureSecretsFromConfig(cfg)
	if secretsProvider.Name() != "env" {
		t.Errorf("default should be env, got %q", secretsProvider.Name())
	}
}

func TestConfigureSecretsFromConfig_VaultProvider(t *testing.T) {
	original := secretsProvider
	defer SetSecretsProvider(original)

	cfg := config.DefaultConfig()
	cfg.Secrets.Provider = "vault"
	cfg.Secrets.VaultAddr = "http://127.0.0.1:8200"
	cfg.Secrets.VaultToken = "test-token"

	configureSecretsFromConfig(cfg)
	if secretsProvider.Name() != "vault" {
		t.Errorf("expected vault provider, got %q", secretsProvider.Name())
	}
}

func TestConfigureSecretsFromConfig_VaultMissingTokenFallsBackToEnv(t *testing.T) {
	original := secretsProvider
	defer SetSecretsProvider(original)

	t.Setenv("VAULT_TOKEN", "")

	cfg := config.DefaultConfig()
	cfg.Secrets.Provider = "vault"
	cfg.Secrets.VaultAddr = "http://127.0.0.1:8200"

	configureSecretsFromConfig(cfg)
	if secretsProvider.Name() != "env" {
		t.Errorf("missing vault token should fall back to env, got %q", secretsProvider.Name())
	}
}

func TestConfigureSecretsFromConfig_UnknownProviderFallsBackToEnv(t *testing.T) {
	original := secretsProvider
	defer SetSecretsProvider(original)

	cfg := config.DefaultConfig()
	cfg.Secrets.Provider = "bogus"
	configureSecretsFromConfig(cfg)
	if secretsProvider.Name() != "env" {
		t.Errorf("unknown provider should fall back to env, got %q", secretsProvider.Name())
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
