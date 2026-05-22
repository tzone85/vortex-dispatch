package ghost

import (
	"fmt"
	"os"
)

// ResolveAPIKey returns the API key from an explicit value (highest priority),
// then from the env var named by envVar, then from the default GHOST_API_KEY env var.
// Returns an error with a helpful message when no key is found.
func ResolveAPIKey(envVar, explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if envVar == "" {
		envVar = "GHOST_API_KEY"
	}
	if v := os.Getenv(envVar); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("ghost: no API key — set %s env var or run `ghost login --headless`", envVar)
}
