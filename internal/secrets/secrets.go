// Package secrets provides an abstraction over secret stores so VXD can
// use environment variables (Phase 1), HashiCorp Vault (Phase 2), or
// other providers without code changes outside this package.
package secrets

import (
	"context"
	"fmt"
	"os"
)

// Provider retrieves secrets by key.
//
// The context governs network-backed providers (Vault, AWS Secrets Manager,
// etc.): callers cancelling ctx stop the underlying request promptly rather
// than waiting for the provider's internal timeout. The EnvProvider ignores
// ctx because the lookup is local and synchronous.
type Provider interface {
	Get(ctx context.Context, key string) (string, error)
	Name() string
}

// EnvProvider reads secrets from environment variables.
// This is the default Phase 1 provider.
type EnvProvider struct{}

// NewEnvProvider creates an environment variable-based provider.
func NewEnvProvider() *EnvProvider {
	return &EnvProvider{}
}

// Get returns the value of the environment variable named by key. Ignores
// ctx — env lookups are local and synchronous. Returns an error if the
// variable is unset (not just empty).
func (p *EnvProvider) Get(_ context.Context, key string) (string, error) {
	val, ok := os.LookupEnv(key)
	if !ok {
		return "", fmt.Errorf("env secret not found: %s", key)
	}
	return val, nil
}

// Name returns the provider name for logging.
func (p *EnvProvider) Name() string { return "env" }
