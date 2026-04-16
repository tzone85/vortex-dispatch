package cli

import (
	"github.com/tzone85/vortex-dispatch/internal/secrets"
)

// secretsProvider is the package-level secret store. Defaults to env vars.
// Phase 2: replaced with VaultProvider when config.Secrets.Provider == "vault".
var secretsProvider secrets.Provider = secrets.NewEnvProvider()

// resolveAPIKey returns the secret value for the given name via the
// configured secrets provider. Returns empty string if not found
// (callers check for empty to mirror existing os.Getenv behavior).
func resolveAPIKey(name string) string {
	val, err := secretsProvider.Get(name)
	if err != nil {
		return ""
	}
	return val
}

// SetSecretsProvider replaces the package-level secrets provider.
// Used by tests and (future) by config-based initialization.
func SetSecretsProvider(p secrets.Provider) {
	secretsProvider = p
}
