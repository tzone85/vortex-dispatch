package cli

import (
	"log"
	"os"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/secrets"
)

// secretsProvider is the package-level secret store. Defaults to env vars.
// Replaced with VaultProvider when config.Secrets.Provider == "vault".
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
