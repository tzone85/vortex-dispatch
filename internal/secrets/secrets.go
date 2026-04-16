// Package secrets provides an abstraction over secret stores so VXD can
// use environment variables (Phase 1), HashiCorp Vault (Phase 2), or
// other providers without code changes outside this package.
package secrets

import (
	"fmt"
	"os"
)

// Provider retrieves secrets by key.
type Provider interface {
	Get(key string) (string, error)
	Name() string
}

// EnvProvider reads secrets from environment variables.
// This is the default Phase 1 provider.
type EnvProvider struct{}

// NewEnvProvider creates an environment variable-based provider.
func NewEnvProvider() *EnvProvider {
	return &EnvProvider{}
}

// Get returns the value of the environment variable named by key.
// Returns an error if the variable is unset (not just empty).
func (p *EnvProvider) Get(key string) (string, error) {
	val, ok := os.LookupEnv(key)
	if !ok {
		return "", fmt.Errorf("env secret not found: %s", key)
	}
	return val, nil
}

// Name returns the provider name for logging.
func (p *EnvProvider) Name() string { return "env" }
