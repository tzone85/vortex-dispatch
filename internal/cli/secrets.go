package cli

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/secrets"
)

// secretsProvider is the package-level secret store. Defaults to env vars.
// Replaced with VaultProvider when config.Secrets.Provider == "vault".
var secretsProvider secrets.Provider = secrets.NewEnvProvider()

// secretsLookupTimeout caps each provider call. Env lookups complete in
// microseconds; Vault has its own 10 s client timeout. This is an outer
// guard that fires only when the provider itself stalls.
const secretsLookupTimeout = 10 * time.Second

// resolveAPIKey returns the secret value for the given name via the
// configured secrets provider. Returns empty string if not found
// (callers check for empty to mirror existing os.Getenv behavior).
//
// Uses an internal bounded context so a slow provider cannot block the
// caller indefinitely. Prefer resolveAPIKeyCtx when a real request
// context is in scope (CLI handlers via cmd.Context()).
func resolveAPIKey(name string) string {
	ctx, cancel := context.WithTimeout(context.Background(), secretsLookupTimeout)
	defer cancel()
	return resolveAPIKeyCtx(ctx, name)
}

// resolveAPIKeyCtx is the context-aware lookup. Cancelling ctx cancels
// the in-flight provider call.
func resolveAPIKeyCtx(ctx context.Context, name string) string {
	val, err := secretsProvider.Get(ctx, name)
	if err != nil {
		return ""
	}
	return val
}

// SetSecretsProvider replaces the package-level secrets provider.
// Used by tests and by config-based initialization.
func SetSecretsProvider(p secrets.Provider) {
	secretsProvider = p
}

// configureSecretsFromConfig initializes the package-level secretsProvider
// based on cfg.Secrets. Default ("" or "env") uses EnvProvider. "vault"
// uses VaultProvider with token from config or VAULT_TOKEN env var.
// Falls back to EnvProvider if Vault is misconfigured (logs a warning).
func configureSecretsFromConfig(cfg config.Config) {
	switch cfg.Secrets.Provider {
	case "", "env":
		secretsProvider = secrets.NewEnvProvider()
	case "vault":
		token := cfg.Secrets.VaultToken
		if token == "" {
			token = os.Getenv("VAULT_TOKEN")
		}
		if token == "" {
			log.Printf("[secrets] vault provider configured but no token found (config or VAULT_TOKEN env); falling back to env")
			secretsProvider = secrets.NewEnvProvider()
			return
		}
		secretsProvider = secrets.NewVaultProvider(secrets.VaultConfig{
			Addr:       cfg.Secrets.VaultAddr,
			Token:      token,
			MountPath:  cfg.Secrets.VaultMount,
			SecretPath: cfg.Secrets.VaultPath,
		})
		log.Printf("[secrets] using vault provider at %s", cfg.Secrets.VaultAddr)
	default:
		log.Printf("[secrets] unknown provider %q, falling back to env", cfg.Secrets.Provider)
		secretsProvider = secrets.NewEnvProvider()
	}
}
